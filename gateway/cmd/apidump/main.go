// apidump 是 OpenAPI 契约的静态提取器：用 go/ast 解析
// gateway/cmd/server/router.go（setupRoutes + registerXxxRoutes 系列）
// 以及被其调用的内部包 RegisterRoutes（internal/onchain、internal/social、
// internal/dataprovider），提取 method + path + handler，输出结构化 JSON
// 并渲染为 OpenAPI 3.0 yaml。
//
// 用法：
//
//	go run ./cmd/apidump                                    # JSON 到 stdout
//	go run ./cmd/apidump -json routes.json                  # JSON 到文件
//	go run ./cmd/apidump -openapi ../docs/api/openapi.yaml  # 重新生成契约
//
// 提取结果与运行期路由的一致性由两道防线保证：
//  1. gateway/cmd/server 的契约测试断言 gin 运行期路由集合 == openapi.yaml 路径集合；
//  2. CI（openapi-ci.yml）重新生成 openapi.yaml 并 git diff，漂移即失败。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	gatewayDir := flag.String("gateway", ".", "gateway 模块根目录（含 go.mod）")
	openapiOut := flag.String("openapi", "", "把 OpenAPI 3.0 yaml 写到该路径")
	jsonOut := flag.String("json", "", "把提取结果 JSON 写到该路径（\"-\" = stdout）")
	flag.Parse()

	// 两个输出都没给时默认 JSON 到 stdout（纯提取器模式）。
	if *openapiOut == "" && *jsonOut == "" {
		*jsonOut = "-"
	}

	absGw, err := filepath.Abs(*gatewayDir)
	if err != nil {
		fatal("解析 gateway 目录失败: %v", err)
	}

	routes, warnings, err := extractRoutes(absGw)
	if err != nil {
		fatal("提取失败: %v", err)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "apidump: WARN:", w)
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].OpenAPIPath() != routes[j].OpenAPIPath() {
			return routes[i].OpenAPIPath() < routes[j].OpenAPIPath()
		}
		return routes[i].Method < routes[j].Method
	})
	assignOperationIDs(routes)

	if *jsonOut != "" {
		payload, err := json.MarshalIndent(map[string]any{
			"route_count": len(routes),
			"routes":      routes,
		}, "", "  ")
		if err != nil {
			fatal("JSON 序列化失败: %v", err)
		}
		payload = append(payload, '\n')
		if *jsonOut == "-" {
			if _, err := os.Stdout.Write(payload); err != nil {
				fatal("写 stdout 失败: %v", err)
			}
		} else if err := os.WriteFile(*jsonOut, payload, 0o644); err != nil {
			fatal("写 %s 失败: %v", *jsonOut, err)
		}
	}

	if *openapiOut != "" {
		doc, err := renderOpenAPI(routes)
		if err != nil {
			fatal("渲染 OpenAPI 失败: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(*openapiOut), 0o755); err != nil {
			fatal("创建输出目录失败: %v", err)
		}
		if err := os.WriteFile(*openapiOut, []byte(doc), 0o644); err != nil {
			fatal("写 %s 失败: %v", *openapiOut, err)
		}
		fmt.Fprintf(os.Stderr, "apidump: wrote %s\n", *openapiOut)
	}

	fmt.Fprintf(os.Stderr, "apidump: extracted %d routes\n", len(routes))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "apidump: "+format+"\n", args...)
	os.Exit(1)
}
