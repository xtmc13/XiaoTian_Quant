package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ListModels 从厂商 API 实时拉取该账号可用的模型列表（供设置页"拉取模型"按钮）。
// 三协议：Anthropic GET /v1/models；Gemini GET /v1beta/models；其余按 OpenAI 兼容
// GET {base}/models 处理（base 带版本段直接拼 /models，否则补 /v1/models）。
// 返回纯模型 id 列表（升序），已对 embeddings/TTS 等非对话模型做过滤。
func ListModels(name, baseURL, apiKey string) ([]string, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API Key 为空")
	}
	switch name {
	case "claude":
		return listAnthropicModels(baseURL, apiKey)
	case "gemini":
		return listGeminiModels(baseURL, apiKey)
	default:
		return listOpenAICompatibleModels(baseURL, apiKey)
	}
}

// modelsURL 与 chatCompletionsURL 同一推导规则：末段是版本号直接拼 /models，否则补 /v1。
func modelsURL(baseURL string) string {
	b := strings.TrimRight(baseURL, "/")
	seg := b[strings.LastIndex(b, "/")+1:]
	if len(seg) >= 2 && seg[0] == 'v' && seg[1] >= '0' && seg[1] <= '9' {
		return b + "/models"
	}
	return b + "/v1/models"
}

func httpGetJSON(url, apiKey string, headers map[string]string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return body, nil
}

func listOpenAICompatibleModels(baseURL, apiKey string) ([]string, error) {
	body, err := httpGetJSON(modelsURL(baseURL), apiKey, nil, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %v", err)
	}
	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" && isChatModelID(m.ID) {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

func listAnthropicModels(baseURL, apiKey string) ([]string, error) {
	b := strings.TrimRight(baseURL, "/")
	url := b + "/v1/models"
	body, err := httpGetJSON(url, "", map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
	}, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID         string `json:"id"`
			Deprecated bool   `json:"deprecated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %v", err)
	}
	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" && !m.Deprecated {
			models = append(models, m.ID)
		}
	}
	// 新的在前：Anthropic 列表按时间正序返回。
	for i, j := 0, len(models)-1; i < j; i, j = i+1, j-1 {
		models[i], models[j] = models[j], models[i]
	}
	return models, nil
}

func listGeminiModels(baseURL, apiKey string) ([]string, error) {
	b := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/v1beta/models?key=%s", b, apiKey)
	body, err := httpGetJSON(url, "", nil, 15*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Models []struct {
			Name string `json:"name"` // models/gemini-2.5-pro
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %v", err)
	}
	models := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if id != "" && strings.Contains(id, "gemini") && isChatModelID(id) {
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models, nil
}

// isChatModelID 过滤明显非对话类模型（embedding/TTS/图像/审核/检索等）。
func isChatModelID(id string) bool {
	id = strings.ToLower(id)
	deny := []string{"embed", "tts", "whisper", "dall", "moderation", "babbage", "davinci",
		"audio", "image", "realtime", "transcribe", "search", "guard", "safety"}
	for _, d := range deny {
		if strings.Contains(id, d) {
			return false
		}
	}
	return true
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
