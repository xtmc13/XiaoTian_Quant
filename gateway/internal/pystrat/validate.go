package pystrat

import (
	"fmt"
	"regexp"
	"strings"
)

// ── 静态校验 ──
// 不启动沙箱即可提前报错：import 白名单、危险内建调用、STRATEGY_MANIFEST
// 存在性、回调签名。实现为行扫描 + 正则（沙箱 worker 内部还有 Python AST
// 二次校验，这里是第一层；两层都过才算 validate 通过）。

// AllowedImportRoots 是策略允许 import 的模块白名单（根模块名）。
// 注意：任务书原文 "functypes" 是 functools 的笔误——Python 标准库无
// functypes，按 functools 落地。
var AllowedImportRoots = map[string]bool{
	"math": true, "json": true, "datetime": true, "collections": true,
	"heapq": true, "itertools": true, "functools": true, "statistics": true,
}

// unsafeBuiltinCalls 与 sandbox/executor.py 的 UNSAFE_BUILTINS 对齐。
var unsafeBuiltinCalls = []string{
	"eval", "exec", "compile", "open", "__import__", "input",
	"globals", "vars", "breakpoint", "exit", "quit", "help",
}

// ValidationIssue 是一条静态校验发现（Line 从 1 开始，0 表示整份代码级）。
type ValidationIssue struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (v ValidationIssue) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("第 %d 行: %s", v.Line, v.Message)
	}
	return v.Message
}

var (
	reImportStmt   = regexp.MustCompile(`^\s*import\s+(.+)$`)
	reFromImport   = regexp.MustCompile(`^\s*from\s+([A-Za-z_][A-Za-z0-9_.]*)\s+import\s+`)
	reDefInit      = regexp.MustCompile(`^\s*def\s+initialize\s*\(\s*context\s*\)\s*:`)
	reDefOnBar     = regexp.MustCompile(`^\s*def\s+on_bar\s*\(\s*context\s*,\s*bar\s*\)\s*:`)
	reDefOnOrder   = regexp.MustCompile(`^\s*def\s+on_order\s*\(\s*context\s*,\s*order\s*\)\s*:`)
	reManifest     = regexp.MustCompile(`^\s*STRATEGY_MANIFEST\s*=`)
	reDunderCall   = regexp.MustCompile(`\b(getattr|setattr|delattr)\s*\(`)
	reDunderString = regexp.MustCompile(`['"]\s*__[A-Za-z0-9_]+__\s*['"]`)
	reDynImport    = regexp.MustCompile(`__import__\s*\(\s*['"]([A-Za-z0-9_.]+)['"]`)
)

// ValidateStatic 对策略源码做静态校验；issues 为空表示通过。
// 校验项：
//  1. import 白名单（含缩进在函数体内的 import、别名 import、from-import）
//  2. 危险内建调用（eval/exec/open/__import__ 等）
//  3. STRATEGY_MANIFEST 必须存在
//  4. initialize(context) / on_bar(context, bar) 必须存在且签名正确；
//     on_order(context, order) 如出现也必须签名正确
func ValidateStatic(code string) []ValidationIssue {
	var issues []ValidationIssue
	lines := strings.Split(code, "\n")

	seenManifest := false
	seenInit := false
	seenOnBar := false

	for i, raw := range lines {
		lineNo := i + 1
		line := raw

		if reManifest.MatchString(line) {
			seenManifest = true
		}
		if reDefInit.MatchString(line) {
			seenInit = true
		}
		if reDefOnBar.MatchString(line) {
			seenOnBar = true
		}
		if m := reDefOnBarWrong.FindStringSubmatch(line); m != nil && !reDefOnBar.MatchString(line) {
			issues = append(issues, ValidationIssue{Line: lineNo, Code: "SIGNATURE",
				Message: fmt.Sprintf("on_bar 签名错误：应为 on_bar(context, bar)，当前 %s", strings.TrimSpace(line))})
		}
		if m := reDefInitWrong.FindStringSubmatch(line); m != nil && !reDefInit.MatchString(line) {
			issues = append(issues, ValidationIssue{Line: lineNo, Code: "SIGNATURE",
				Message: fmt.Sprintf("initialize 签名错误：应为 initialize(context)，当前 %s", strings.TrimSpace(line))})
		}
		if reDefOnOrderWrong.MatchString(line) && !reDefOnOrder.MatchString(line) {
			issues = append(issues, ValidationIssue{Line: lineNo, Code: "SIGNATURE",
				Message: fmt.Sprintf("on_order 签名错误：应为 on_order(context, order)，当前 %s", strings.TrimSpace(line))})
		}

		// import 白名单
		if m := reImportStmt.FindStringSubmatch(line); m != nil {
			// import a, b as c, d.e —— 逐个取根模块
			for _, part := range strings.Split(m[1], ",") {
				mod := strings.TrimSpace(part)
				if as := strings.LastIndex(mod, " as "); as >= 0 {
					mod = strings.TrimSpace(mod[:as])
				}
				root := strings.Split(strings.TrimSpace(mod), ".")[0]
				if root == "" {
					continue
				}
				if !AllowedImportRoots[root] {
					issues = append(issues, ValidationIssue{Line: lineNo, Code: "IMPORT",
						Message: fmt.Sprintf("禁止 import 模块 %q：仅允许 %s", root, allowedList())})
				}
			}
		}
		if m := reFromImport.FindStringSubmatch(line); m != nil {
			root := strings.Split(m[1], ".")[0]
			if !AllowedImportRoots[root] {
				issues = append(issues, ValidationIssue{Line: lineNo, Code: "IMPORT",
					Message: fmt.Sprintf("禁止 from %s import ...：仅允许 %s", m[1], allowedList())})
			}
		}

		// 危险内建调用
		for _, name := range unsafeBuiltinCalls {
			if hasCall(line, name) {
				issues = append(issues, ValidationIssue{Line: lineNo, Code: "BUILTIN",
					Message: fmt.Sprintf("禁止调用 %s()：策略代码不得使用危险内建函数", name)})
			}
		}
		if reDynImport.FindStringSubmatch(line) != nil {
			issues = append(issues, ValidationIssue{Line: lineNo, Code: "BUILTIN",
				Message: "禁止动态 __import__()：策略代码不得动态导入模块"})
		}
		if reDunderCall.MatchString(line) && reDunderString.MatchString(line) {
			issues = append(issues, ValidationIssue{Line: lineNo, Code: "BUILTIN",
				Message: "禁止通过 getattr/setattr 访问双下划线属性（沙箱逃逸通道）"})
		}
	}

	if !seenManifest {
		issues = append(issues, ValidationIssue{Line: 0, Code: "MANIFEST",
			Message: "缺少 STRATEGY_MANIFEST = {...}：策略必须声明模块级 manifest 契约"})
	}
	if !seenInit {
		issues = append(issues, ValidationIssue{Line: 0, Code: "CALLBACK",
			Message: "缺少 initialize(context) 回调"})
	}
	if !seenOnBar {
		issues = append(issues, ValidationIssue{Line: 0, Code: "CALLBACK",
			Message: "缺少 on_bar(context, bar) 回调"})
	}
	return issues
}

// "签名错误" 的宽松识别：出现 def on_bar( 但精确签名不匹配。
var (
	reDefOnBarWrong   = regexp.MustCompile(`^\s*def\s+on_bar\s*\(`)
	reDefInitWrong    = regexp.MustCompile(`^\s*def\s+initialize\s*\(`)
	reDefOnOrderWrong = regexp.MustCompile(`^\s*def\s+on_order\s*\(`)
)

func hasCall(line, name string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
	// __import__ 已在动态检测里覆盖，避免双报
	if name == "__import__" {
		return false
	}
	return re.MatchString(line)
}

func allowedList() string {
	names := make([]string, 0, len(AllowedImportRoots))
	for k := range AllowedImportRoots {
		names = append(names, k)
	}
	// 稳定输出便于测试断言
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return strings.Join(names, "/")
}

// FormatIssues 把 issues 渲染成多行中文报告。
func FormatIssues(issues []ValidationIssue) string {
	var b strings.Builder
	for i, iss := range issues {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(iss.String())
	}
	return b.String()
}
