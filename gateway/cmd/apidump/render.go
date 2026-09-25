package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/apispec"
	"gopkg.in/yaml.v3"
)

// ── operationId 生成 ────────────────────────────────────────────────────────

var opIDSanitizer = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// assignOperationIDs 为每条路由生成全局唯一的 operationId（调用前须先排序，
// 保证输出确定性）。规则：
//   - 包级 handler（handler.Login、ws.Stats）→ "handler.Login"
//   - 内部包局部 handler（social:h.listProviders）→ "social.listProviders"
//   - 内联/包装 handler（func 字面量、gin.WrapF(...)）→ inlineGetMetrics 形态
//   - 同名冲突追加 _2/_3 后缀
func assignOperationIDs(routes []*Route) {
	used := map[string]int{}
	for _, r := range routes {
		base := operationIDBase(r)
		id := base
		if n := used[base]; n > 0 {
			id = fmt.Sprintf("%s_%d", base, n+1)
		}
		used[base]++
		r.OperationID = id
	}
}

func operationIDBase(r *Route) string {
	h := r.Handler
	switch {
	case strings.HasPrefix(h, "func("), strings.HasPrefix(h, "func ("):
		return "inline" + methodTitle(r.Method) + camelPath(r.Path)
	}
	if i := strings.Index(h, ":"); i > 0 {
		// 内部包局部 handler：pkg:h.method → pkg.method
		rest := h[i+1:]
		if j := strings.Index(rest, "."); j > 0 {
			return h[:i] + rest[j:]
		}
		return h[:i] + "_" + opIDSanitizer.ReplaceAllString(rest, "_")
	}
	if strings.ContainsAny(h, "(") {
		// gin.WrapF(...) 等包装形态
		return "wrapped" + methodTitle(r.Method) + camelPath(r.Path)
	}
	return opIDSanitizer.ReplaceAllString(h, "_")
}

func methodTitle(m string) string {
	return strings.ToUpper(m[:1]) + strings.ToLower(m[1:])
}

// camelPath 把 /debug/pprof/*any 形态转为 DebugPprofAny（inline handler 命名用）。
func camelPath(p string) string {
	parts := strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == ':' || r == '*' || r == '-' || r == '_' || r == '.'
	})
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// ── OpenAPI 渲染 ────────────────────────────────────────────────────────────

type openAPIDoc struct {
	OpenAPI    string                        `yaml:"openapi"`
	Info       specInfo                      `yaml:"info"`
	Servers    []specServer                  `yaml:"servers"`
	Tags       []specTag                     `yaml:"tags"`
	Paths      map[string]map[string]*specOp `yaml:"paths"`
	Components specComponents                `yaml:"components"`
}

type specInfo struct {
	Title       string `yaml:"title"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

type specServer struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description,omitempty"`
}

type specTag struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type specOp struct {
	OperationID  string                `yaml:"operationId"`
	Summary      string                `yaml:"summary"`
	Description  string                `yaml:"description,omitempty"`
	Tags         []string              `yaml:"tags"`
	Security     []map[string][]string `yaml:"security,omitempty"`
	Parameters   []specParam           `yaml:"parameters,omitempty"`
	Responses    map[string]specResp   `yaml:"responses"`
	XHandler     string                `yaml:"x-handler"`
	XSource      string                `yaml:"x-source"`
	XAdmin       bool                  `yaml:"x-admin-required,omitempty"`
	XConditional string                `yaml:"x-conditional-mount,omitempty"`
}

type specParam struct {
	Name        string            `yaml:"name"`
	In          string            `yaml:"in"`
	Required    bool              `yaml:"required"`
	Description string            `yaml:"description,omitempty"`
	Schema      map[string]string `yaml:"schema"`
}

type specResp struct {
	Description string `yaml:"description"`
}

type specComponents struct {
	SecuritySchemes map[string]specSecurityScheme `yaml:"securitySchemes"`
}

type specSecurityScheme struct {
	Type         string `yaml:"type"`
	Scheme       string `yaml:"scheme,omitempty"`
	BearerFormat string `yaml:"bearerFormat,omitempty"`
	In           string `yaml:"in,omitempty"`
	Name         string `yaml:"name,omitempty"`
	Description  string `yaml:"description,omitempty"`
}

const specHeader = `# =============================================================================
# AUTO-GENERATED FILE — DO NOT EDIT / 自动生成，请勿手改
#
# 由 AST 提取器从 gateway/cmd/server/router.go（setupRoutes + registerXxxRoutes
# 系列）及 internal/{onchain,social,dataprovider} 的 RegisterRoutes 生成。
# 新增/修改路由后必须重新生成本文件，否则 CI（openapi-ci）会因漂移失败：
#
#     cd gateway && go run ./cmd/apidump -openapi ../docs/api/openapi.yaml
#
# 一致性守护：
#   - gateway/cmd/server 契约测试断言 gin 运行期路由集合 == 本文件路径集合
#   - .github/workflows/openapi-ci.yml 重新生成并 git diff
# =============================================================================

`

func renderOpenAPI(routes []*Route) (string, error) {
	doc := openAPIDoc{
		OpenAPI: "3.0.3",
		Info: specInfo{
			Title:   "XiaoTian Quant Gateway API",
			Version: "1.0.0",
			Description: "由 gateway/cmd/apidump 自动生成的全路由清单契约（v1）：" +
				"每条路由含 method+path+handler+鉴权标注，request/response schema 后续逐步补充。" +
				"请勿手改；重新生成：cd gateway && go run ./cmd/apidump -openapi ../docs/api/openapi.yaml",
		},
		Servers: []specServer{
			{URL: "/", Description: "网关根：REST 挂 /api，WebSocket 挂 /ws、/ws/v2"},
		},
		Paths: map[string]map[string]*specOp{},
		Components: specComponents{SecuritySchemes: map[string]specSecurityScheme{
			"bearerAuth": {
				Type: "http", Scheme: "bearer", BearerFormat: "JWT",
				Description: "登录态 JWT（Authorization: Bearer <token>），由 middleware.AuthRequired 校验",
			},
			"webhookSignature": {
				Type: "apiKey", In: "header", Name: "X-Webhook-Signature",
				Description: "TradingView/通用 webhook 的 HMAC-SHA256 签名（WEBHOOK_SECRET），验签在 handler 内完成",
			},
			"stripeSignature": {
				Type: "apiKey", In: "header", Name: "Stripe-Signature",
				Description: "Stripe 服务端回调验签头（v1 scheme）",
			},
		}},
	}

	tagSet := map[string]bool{}
	for _, r := range routes {
		op := buildOperation(r)
		methodKey := strings.ToLower(r.Method)
		item := doc.Paths[r.OpenAPIPath()]
		if item == nil {
			item = map[string]*specOp{}
			doc.Paths[r.OpenAPIPath()] = item
		}
		if _, dup := item[methodKey]; dup {
			return "", fmt.Errorf("路由重复: %s %s（%s 与 %s）", r.Method, r.OpenAPIPath(), item[methodKey].XSource, r.Source)
		}
		item[methodKey] = op
		tagSet[r.Tag] = true
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	for _, t := range tags {
		doc.Tags = append(doc.Tags, specTag{
			Name:        t,
			Description: "register " + t + " 路由分组（gateway/cmd/server/router.go 或内部包 RegisterRoutes）",
		})
	}

	var b strings.Builder
	b.WriteString(specHeader)
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func buildOperation(r *Route) *specOp {
	op := &specOp{
		OperationID: r.OperationID,
		Summary:     displayHandler(r.Handler),
		Tags:        []string{r.Tag},
		XHandler:    r.Handler,
		XSource:     r.Source,
	}

	var desc []string
	if r.Conditional != "" {
		desc = append(desc, fmt.Sprintf("条件挂载：仅当 `%s` 成立时注册。", r.Conditional))
		op.XConditional = r.Conditional
	}
	if r.Path == "/ws" || r.Path == "/ws/v2" {
		desc = append(desc, "WebSocket 升级端点（非普通 JSON REST）。")
	}
	if r.Path == "/metrics" {
		desc = append(desc, "Prometheus 指标；访问控制见 metricsAccessGuard（PROMETHEUS_TOKEN 或 localhost-only）。")
	}
	switch r.Auth {
	case "admin":
		op.Security = []map[string][]string{{"bearerAuth": {}}}
		op.XAdmin = true
	case "bearer":
		op.Security = []map[string][]string{{"bearerAuth": {}}}
	case "webhook":
		op.Security = []map[string][]string{{"webhookSignature": {}}}
		desc = append(desc, "HMAC-SHA256 验签（WEBHOOK_SECRET），无需登录。")
	case "stripe-signature":
		op.Security = []map[string][]string{{"stripeSignature": {}}}
		desc = append(desc, "Stripe 服务端回调，Stripe-Signature 验签，无需登录。")
	}

	for _, name := range apispec.PathParamNames(r.OpenAPIPath()) {
		p := specParam{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   map[string]string{"type": "string"},
		}
		if name == "any" {
			p.Description = "gin 通配段（匹配剩余路径）"
		}
		op.Parameters = append(op.Parameters, p)
	}

	op.Responses = map[string]specResp{
		"200": {Description: "成功响应（v1 契约未细化 schema，见 x-handler 对应实现）"},
	}
	switch r.Auth {
	case "admin":
		op.Responses["401"] = specResp{Description: "未认证或 token 失效"}
		op.Responses["403"] = specResp{Description: "已认证但非管理员"}
	case "bearer":
		op.Responses["401"] = specResp{Description: "未认证或 token 失效"}
	case "webhook", "stripe-signature":
		op.Responses["401"] = specResp{Description: "签名验签失败"}
	}
	op.Description = strings.Join(desc, " ")
	return op
}

// displayHandler 把 handler 表达式收敛为一行人类可读形态。
func displayHandler(h string) string {
	if strings.HasPrefix(h, "func(") || strings.HasPrefix(h, "func (") {
		return "inline func"
	}
	if i := strings.Index(h, ":"); i > 0 {
		return h[i+1:] + " (" + h[:i] + ")"
	}
	return h
}
