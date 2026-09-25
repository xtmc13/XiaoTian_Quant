package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/apispec"
)

// Route 是一条被静态提取出的路由注册记录。
type Route struct {
	Method       string `json:"method"`                // GET/POST/...
	Path         string `json:"path"`                  // gin 语法路径（:param、*wildcard）
	Handler      string `json:"handler"`               // handler 表达式的源码形态（如 handler.Login、social:h.getETHMetrics）
	OperationID  string `json:"operation_id"`          // 渲染阶段去重后填入
	Tag          string `json:"tag"`                   // 由 registerXxxRoutes 函数名派生
	RegisterFunc string `json:"register_func"`         // 注册它的函数（tag 来源）
	Source       string `json:"source"`                // 相对 gateway 模块根的 file:line
	Auth         string `json:"auth"`                  // none|bearer|admin|webhook|stripe-signature
	Conditional  string `json:"conditional,omitempty"` // 条件挂载的 if 表达式（如 metrics.Enabled()）
}

// OpenAPIPath 返回该路由的 OpenAPI path templating 形态。
func (r *Route) OpenAPIPath() string { return apispec.GinToOpenAPIPath(r.Path) }

// groupInfo 记录一个 gin.RouterGroup 变量的前缀与继承到的鉴权中间件。
type groupInfo struct {
	prefix string
	auth   bool // Use(middleware.AuthRequired())
	admin  bool // Use(middleware.AdminRequired())
}

type externalCall struct {
	pkgName     string
	dir         string // 包目录（绝对路径）
	prefix      string
	auth, admin bool
	tag         string
	src         string
}

type extractor struct {
	fset      *token.FileSet
	gwDir     string // gateway 模块根（绝对路径）
	routes    []*Route
	externals []externalCall
	warnings  []string
	visited   map[string]bool // 防止本地 register 函数递归重复展开
}

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// extractRoutes 从 gateway 模块根静态提取全部注册路由。
func extractRoutes(gwDir string) ([]*Route, []string, error) {
	e := &extractor{
		fset:    token.NewFileSet(),
		gwDir:   gwDir,
		visited: map[string]bool{},
	}

	routerPath := filepath.Join(gwDir, "cmd", "server", "router.go")
	f, err := parser.ParseFile(e.fset, routerPath, nil, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 %s 失败: %w", routerPath, err)
	}
	imports := importAliasMap(f, gwDir)
	funcs := map[string]*ast.FuncDecl{}
	var setup *ast.FuncDecl
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		funcs[fn.Name.Name] = fn
		if fn.Name.Name == "setupRoutes" {
			setup = fn
		}
	}
	if setup == nil {
		return nil, nil, fmt.Errorf("%s 中找不到 setupRoutes", routerPath)
	}
	if len(setup.Type.Params.List) == 0 || len(setup.Type.Params.List[0].Names) == 0 {
		return nil, nil, fmt.Errorf("setupRoutes 签名异常：无参数")
	}

	// 入口环境：第一个参数（*gin.Engine）前缀为空。
	engineParam := setup.Type.Params.List[0].Names[0].Name
	env := map[string]*groupInfo{engineParam: {prefix: ""}}
	e.walkBlock(setup.Body, env, imports, funcs, "core", "")

	// 展开跨包 RegisterRoutes（onchain/social/dataprovider……，按 import 自动发现）。
	for _, ext := range e.externals {
		if err := e.processExternal(ext); err != nil {
			e.warnings = append(e.warnings, err.Error())
		}
	}

	return e.routes, e.warnings, nil
}

// importAliasMap 返回 import 别名 → 包目录（绝对路径），仅含本模块内的包。
func importAliasMap(f *ast.File, gwDir string) map[string]string {
	out := map[string]string{}
	const modPrefix = "github.com/xiaotian-quant/gateway/"
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(p, modPrefix) {
			continue
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			name = p[strings.LastIndex(p, "/")+1:]
		}
		out[name] = filepath.Join(gwDir, filepath.FromSlash(strings.TrimPrefix(p, modPrefix)))
	}
	return out
}

// walkBlock 按语句顺序遍历一个 BlockStmt，跟踪 group 变量环境并收集路由。
//
// 处理的 AST 形态（边界情况）：
//   - `g := parent.Group("/lit")`        —— 新 group 变量（继承父级鉴权状态）
//   - `g.METHOD("/lit", mws..., hdl)`    —— 路由注册；最后一个参数为 handler，
//     中间参数里出现 middleware.AuthRequired()/AdminRequired() 时按路由级鉴权记
//   - `g.Use(middleware.AuthRequired())` —— group 级鉴权（就地更新 groupInfo）
//   - `registerXxx(api)`                 —— 本地注册函数，递归展开（参数名映射前缀）
//   - `pkg.RegisterRoutes(g, ...)`       —— 跨包注册，记录下来稍后展开
//   - if / for / range / 裸 block        —— 递归遍历；if 条件文本记为条件挂载说明
//
// 其余形态（非注册类方法调用、非 Group 字面量赋值等）一律忽略。
func (e *extractor) walkBlock(body *ast.BlockStmt, env map[string]*groupInfo, imports map[string]string, funcs map[string]*ast.FuncDecl, tag, conditional string) {
	for _, stmt := range body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			e.walkAssign(s, env)
		case *ast.ExprStmt:
			e.walkExprStmt(s, env, imports, funcs, tag, conditional)
		case *ast.IfStmt:
			cond := renderExpr(e.fset, s.Cond)
			e.walkBlock(s.Body, env, imports, funcs, tag, mergeConditional(conditional, cond))
			switch els := s.Else.(type) {
			case *ast.BlockStmt:
				e.walkBlock(els, env, imports, funcs, tag, conditional)
			case *ast.IfStmt:
				// else-if 链：包一层 block 复用同一套遍历
				e.walkBlock(&ast.BlockStmt{List: []ast.Stmt{els}}, env, imports, funcs, tag, conditional)
			}
		case *ast.BlockStmt:
			e.walkBlock(s, env, imports, funcs, tag, conditional)
		case *ast.ForStmt:
			e.walkBlock(s.Body, env, imports, funcs, tag, conditional)
		case *ast.RangeStmt:
			e.walkBlock(s.Body, env, imports, funcs, tag, conditional)
		}
	}
}

func (e *extractor) walkAssign(s *ast.AssignStmt, env map[string]*groupInfo) {
	if s.Tok != token.DEFINE || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}
	lhs, ok := s.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	call, ok := s.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Group" {
		return
	}
	base, ok := sel.X.(*ast.Ident)
	if !ok || env[base.Name] == nil {
		return
	}
	if len(call.Args) != 1 {
		return
	}
	lit, ok := stringLiteral(call.Args[0])
	if !ok {
		e.warnings = append(e.warnings, fmt.Sprintf("%s: Group() 参数非字符串字面量，跳过: %s", e.srcPos(s), renderExpr(e.fset, call.Args[0])))
		return
	}
	parent := env[base.Name]
	env[lhs.Name] = &groupInfo{
		prefix: apispec.JoinGinPath(parent.prefix, lit),
		auth:   parent.auth,
		admin:  parent.admin,
	}
}

func (e *extractor) walkExprStmt(s *ast.ExprStmt, env map[string]*groupInfo, imports map[string]string, funcs map[string]*ast.FuncDecl, tag, conditional string) {
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		return
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		x, ok := fun.X.(*ast.Ident)
		if !ok {
			return
		}
		if g := env[x.Name]; g != nil {
			switch {
			case httpMethods[fun.Sel.Name]:
				e.emitRoute(s, call, g, fun.Sel.Name, tag, conditional)
			case fun.Sel.Name == "Any":
				for _, m := range []string{"GET", "POST", "PUT", "PATCH", "HEAD", "OPTIONS", "DELETE"} {
					e.emitRoute(s, call, g, m, tag, conditional)
				}
				e.warnings = append(e.warnings, fmt.Sprintf("%s: Any() 已展开为 7 个标准方法", e.srcPos(s)))
			case fun.Sel.Name == "Use":
				for _, arg := range call.Args {
					scanMiddlewareArg(arg, &g.auth, &g.admin)
				}
			}
			return
		}
		// 跨包 RegisterRoutes：pkg.RegisterRoutes(groupVar, ...)
		if dir, isPkg := imports[x.Name]; isPkg && fun.Sel.Name == "RegisterRoutes" && len(call.Args) >= 1 {
			gv, ok := call.Args[0].(*ast.Ident)
			if !ok || env[gv.Name] == nil {
				e.warnings = append(e.warnings, fmt.Sprintf("%s: %s.RegisterRoutes 的首参不是已知 group 变量，跳过", e.srcPos(s), x.Name))
				return
			}
			g := env[gv.Name]
			e.externals = append(e.externals, externalCall{
				pkgName: x.Name, dir: dir, prefix: g.prefix,
				auth: g.auth, admin: g.admin, tag: tag,
				src: e.srcPos(s),
			})
		}
	case *ast.Ident:
		// 本地注册函数：registerXxxRoutes(api)
		fn := funcs[fun.Name]
		if fn == nil || len(fn.Type.Params.List) == 0 || len(fn.Type.Params.List[0].Names) == 0 || len(call.Args) == 0 {
			return
		}
		argVar, ok := call.Args[0].(*ast.Ident)
		if !ok || env[argVar.Name] == nil {
			return
		}
		key := fun.Name + "@" + env[argVar.Name].prefix
		if e.visited[key] {
			return
		}
		e.visited[key] = true
		paramName := fn.Type.Params.List[0].Names[0].Name
		inner := map[string]*groupInfo{paramName: env[argVar.Name]}
		e.walkBlock(fn.Body, inner, imports, funcs, deriveTag(fun.Name), conditional)
	}
}

// emitRoute 从 `g.METHOD("/path", mws..., handler)` 形态的调用生成一条 Route。
func (e *extractor) emitRoute(stmt ast.Stmt, call *ast.CallExpr, g *groupInfo, method, tag, conditional string) {
	if len(call.Args) < 2 {
		e.warnings = append(e.warnings, fmt.Sprintf("%s: %s() 参数不足（缺 handler），跳过", e.srcPos(stmt), method))
		return
	}
	lit, ok := stringLiteral(call.Args[0])
	if !ok {
		e.warnings = append(e.warnings, fmt.Sprintf("%s: %s() 路径非字符串字面量，跳过: %s", e.srcPos(stmt), method, renderExpr(e.fset, call.Args[0])))
		return
	}
	auth, admin := g.auth, g.admin
	for _, arg := range call.Args[1:] {
		scanMiddlewareArg(arg, &auth, &admin)
	}
	route := &Route{
		Method:       method,
		Path:         apispec.JoinGinPath(g.prefix, lit),
		Handler:      renderExpr(e.fset, call.Args[len(call.Args)-1]),
		Tag:          tag,
		RegisterFunc: tag,
		Source:       e.srcPos(stmt),
		Auth:         authKind(auth, admin),
		Conditional:  conditional,
	}
	route.Auth = applyWebhookOverride(route.Path, route.Handler, route.Auth)
	e.routes = append(e.routes, route)
}

// scanMiddlewareArg 识别 middleware.AuthRequired()/AdminRequired() 调用并更新鉴权状态。
// （router.go 与内部包均以 "middleware" 作为 internal/middleware 的默认别名。）
func scanMiddlewareArg(arg ast.Expr, auth, admin *bool) {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != "middleware" {
		return
	}
	switch sel.Sel.Name {
	case "AuthRequired":
		*auth = true
	case "AdminRequired":
		*admin = true
		*auth = true // AdminRequired 内部含 JWT 认证
	}
}

// processExternal 展开一次跨包 RegisterRoutes 调用：在该包目录内找到
// RegisterRoutes 函数，以第一个参数为 group 变量提取路由。
func (e *extractor) processExternal(ext externalCall) error {
	files, err := filepath.Glob(filepath.Join(ext.dir, "*.go"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("外部注册包 %s 目录为空: %s", ext.pkgName, ext.dir)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(e.fset, file, nil, 0)
		if err != nil {
			return fmt.Errorf("解析 %s 失败: %w", file, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "RegisterRoutes" ||
				len(fn.Type.Params.List) == 0 || len(fn.Type.Params.List[0].Names) == 0 {
				continue
			}
			paramName := fn.Type.Params.List[0].Names[0].Name
			env := map[string]*groupInfo{paramName: {prefix: ext.prefix, auth: ext.auth, admin: ext.admin}}
			e.walkExternalBlock(fn.Body, env, ext.tag, f.Name.Name)
			return nil
		}
	}
	return fmt.Errorf("外部注册包 %s 中找不到 RegisterRoutes 函数（目录 %s）", ext.pkgName, ext.dir)
}

// walkExternalBlock 遍历跨包 RegisterRoutes 函数体，只提取路由注册语句
// （同样的 group 变量/Use/HTTP 方法规则；嵌套 block/if/for 递归）。
func (e *extractor) walkExternalBlock(body *ast.BlockStmt, env map[string]*groupInfo, tag, pkgName string) {
	for _, stmt := range body.List {
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			call, ok := s.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || env[x.Name] == nil {
				continue
			}
			switch {
			case httpMethods[sel.Sel.Name]:
				e.emitExternalRoute(s, call, env[x.Name], sel.Sel.Name, tag, pkgName)
			case sel.Sel.Name == "Use":
				g := env[x.Name]
				for _, arg := range call.Args {
					scanMiddlewareArg(arg, &g.auth, &g.admin)
				}
			}
		case *ast.IfStmt:
			e.walkExternalBlock(s.Body, env, tag, pkgName)
		case *ast.BlockStmt:
			e.walkExternalBlock(s, env, tag, pkgName)
		case *ast.ForStmt:
			e.walkExternalBlock(s.Body, env, tag, pkgName)
		case *ast.RangeStmt:
			e.walkExternalBlock(s.Body, env, tag, pkgName)
		}
	}
}

func (e *extractor) emitExternalRoute(stmt ast.Stmt, call *ast.CallExpr, g *groupInfo, method, tag, pkgName string) {
	if len(call.Args) < 2 {
		return
	}
	lit, ok := stringLiteral(call.Args[0])
	if !ok {
		e.warnings = append(e.warnings, fmt.Sprintf("%s: %s() 路径非字符串字面量，跳过", e.srcPos(stmt), method))
		return
	}
	auth, admin := g.auth, g.admin
	for _, arg := range call.Args[1:] {
		scanMiddlewareArg(arg, &auth, &admin)
	}
	handler := renderExpr(e.fset, call.Args[len(call.Args)-1])
	// 包内局部 handler（h.foo / hm.bar）：加包前缀避免跨包撞名（social.h.listProviders 等）。
	if sel, ok := call.Args[len(call.Args)-1].(*ast.SelectorExpr); ok {
		if x, ok := sel.X.(*ast.Ident); ok && x.Name != "" && x.Name[0] >= 'a' && x.Name[0] <= 'z' {
			handler = pkgName + ":" + handler
		}
	}
	route := &Route{
		Method:       method,
		Path:         apispec.JoinGinPath(g.prefix, lit),
		Handler:      handler,
		Tag:          tag,
		RegisterFunc: tag,
		Source:       e.srcPos(stmt),
		Auth:         authKind(auth, admin),
	}
	route.Auth = applyWebhookOverride(route.Path, route.Handler, route.Auth)
	e.routes = append(e.routes, route)
}

// authKind 把 auth/admin 布尔压成契约里的单一鉴权类别。
func authKind(auth, admin bool) string {
	switch {
	case admin:
		return "admin"
	case auth:
		return "bearer"
	default:
		return "none"
	}
}

// applyWebhookOverride 对 webhook 路径改标签名类鉴权（它们不带 AuthRequired，
// 验签在 handler 内完成）。已是登录态的路由不覆盖（防御未来混挂形态）。
func applyWebhookOverride(path, handler, auth string) string {
	isWebhook := strings.Contains(path, "/webhook/") || strings.HasSuffix(path, "/webhook")
	if auth != "none" || !isWebhook {
		return auth
	}
	if strings.Contains(strings.ToLower(handler), "stripe") {
		return "stripe-signature"
	}
	return "webhook"
}

// deriveTag 由注册函数名派生 OpenAPI tag：
// registerXxxRoutes → xxx（前导大写缩写小写化：Auth→auth、AIGate→aiGate、DCABot→dcaBot）。
func deriveTag(fnName string) string {
	s := strings.TrimPrefix(fnName, "register")
	s = strings.TrimSuffix(s, "Routes")
	if s == "" {
		return fnName
	}
	n := 0
	for n < len(s) && s[n] >= 'A' && s[n] <= 'Z' {
		n++
	}
	if n > 1 && n < len(s) && s[n] >= 'a' && s[n] <= 'z' {
		n-- // 大写连跑的最后一个字母属于下一个单词（AIGate → aiGate）
	}
	return strings.ToLower(s[:n]) + s[n:]
}

func mergeConditional(parent, cond string) string {
	if parent == "" {
		return cond
	}
	return parent + " && " + cond
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

func renderExpr(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return fmt.Sprintf("%T", e)
	}
	return b.String()
}

// srcPos 输出相对 gateway 模块根的 file:line，便于在规范里回溯源码。
func (e *extractor) srcPos(n ast.Node) string {
	pos := e.fset.Position(n.Pos())
	if rel, err := filepath.Rel(e.gwDir, pos.Filename); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line)
	}
	return fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
}
