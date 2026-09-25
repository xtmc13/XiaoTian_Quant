// Package apispec 提供 OpenAPI 契约的共享工具：Gin 路径 ↔ OpenAPI 路径转换、
// 生成物 docs/api/openapi.yaml 的定位与读取。
//
// 使用方：
//   - cmd/apidump（AST 提取器，生成契约）
//   - cmd/server 的契约测试（断言运行期路由集合 == 契约路径集合）
//   - handler 的 /api/docs（Swagger UI 读取规范文件）
package apispec

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// SpecFileRelPath 是契约文件相对仓库根的路径（仓库内唯一权威位置）。
const SpecFileRelPath = "docs/api/openapi.yaml"

// JoinGinPath 复刻 gin 的 joinPaths 语义（RouterGroup.Group / 路由注册时的
// 绝对路径拼接规则），静态提取器用它保证拼出的路径与运行期完全一致：
// relativePath 为空时原样返回；rel 以 "/" 结尾而 join 结果没有时补回尾斜杠。
func JoinGinPath(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if relativePath[len(relativePath)-1] == '/' && finalPath[len(finalPath)-1] != '/' {
		return finalPath + "/"
	}
	return finalPath
}

// GinToOpenAPIPath 把 gin 路径参数语法转换为 OpenAPI path templating：
// ":id" → "{id}"，"*any"（通配段）→ "{any}"。转换是可逆映射的约定，
// 契约测试对运行期路由应用同一函数后与规范比较。
func GinToOpenAPIPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if len(s) > 1 && (s[0] == ':' || s[0] == '*') {
			segs[i] = "{" + s[1:] + "}"
		}
	}
	return strings.Join(segs, "/")
}

// PathParamNames 返回 OpenAPI 路径中出现的全部模板参数名（按出现顺序）。
func PathParamNames(openapiPath string) []string {
	var names []string
	for _, seg := range strings.Split(openapiPath, "/") {
		if len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			names = append(names, seg[1:len(seg)-1])
		}
	}
	return names
}

// FindSpecFile 按优先级定位 openapi.yaml：OPENAPI_SPEC_PATH 环境变量 >
// 逐级向上探测（cwd 可能是仓库根、gateway/ 或 gateway/cmd/server 等）。
// 全部未命中时返回错误。
func FindSpecFile() (string, error) {
	if p := os.Getenv("OPENAPI_SPEC_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("OPENAPI_SPEC_PATH=%s 不存在", p)
	}
	for i := 0; i < 6; i++ {
		candidate := strings.Repeat("../", i) + SpecFileRelPath
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("找不到 %s（可用 OPENAPI_SPEC_PATH 显式指定）", SpecFileRelPath)
}

// LoadSpecOperations 读取 OpenAPI 规范，返回 path → (method → operationId)。
// 契约测试用它与 gin 运行期路由集合做双向 diff。
func LoadSpecOperations(specPath string) (map[string]map[string]string, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `yaml:"operationId"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", specPath, err)
	}
	out := make(map[string]map[string]string, len(doc.Paths))
	for p, item := range doc.Paths {
		methods := make(map[string]string, len(item))
		for m, op := range item {
			methods[strings.ToUpper(m)] = op.OperationID
		}
		out[p] = methods
	}
	return out, nil
}
