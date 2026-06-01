package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// ── CC Switch: Anthropic ↔ OpenAI Format Converter ──

// CCSwitch is an HTTP proxy that converts between Anthropic Messages API
// and OpenAI Chat Completions API formats. This allows tools like Claude Code
// or Cursor to connect to any OpenAI-compatible backend (DeepSeek, Qwen, etc.).
type CCSwitch struct {
	TargetURL  string
	TargetKey  string
	TargetModel string
}

// NewCCSwitch creates a new format converter proxy.
func NewCCSwitch(targetURL, apiKey, model string) *CCSwitch {
	return &CCSwitch{
		TargetURL:  strings.TrimRight(targetURL, "/"),
		TargetKey:  apiKey,
		TargetModel: model,
	}
}

// HandleAnthropicRequest processes an Anthropic-formatted request and converts
// it to an OpenAI-compatible request, then converts the response back.
func (c *CCSwitch) HandleAnthropicRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer r.Body.Close()

	var anthropicReq map[string]any
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	messages, _ := anthropicReq["messages"].([]any)
	systemMsg, _ := anthropicReq["system"].(string)
	model, _ := anthropicReq["model"].(string)
	maxTokens, _ := anthropicReq["max_tokens"].(float64)
	stream, _ := anthropicReq["stream"].(bool)

	if model == "" {
		model = c.TargetModel
	}

	// Convert to OpenAI format
	openAIReq := map[string]any{
		"model":    model,
		"messages": convertAnthropicToOpenAIMessages(messages, systemMsg),
	}
	if maxTokens > 0 {
		openAIReq["max_tokens"] = int(maxTokens)
	}
	// Anthropic defaults to streaming; OpenAI uses stream per request
	if stream {
		openAIReq["stream"] = true
	}

	reqBody, _ := json.Marshal(openAIReq)
	req, err := http.NewRequest("POST", c.TargetURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.TargetKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if stream {
		// Convert streaming SSE response
		c.writeStreamResponse(w, respBody)
		return
	}

	// Convert non-streaming response back to Anthropic format
	var openAIResp map[string]any
	json.Unmarshal(respBody, &openAIResp)

	anthropicResp := convertOpenAIToAnthropicResponse(openAIResp, model)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthropicResp)
}

// HandleOpenAIRequest passes through or converts an OpenAI request.
func (c *CCSwitch) HandleOpenAIRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer r.Body.Close()

	var req map[string]any
	json.Unmarshal(body, &req)

	// Override model if target is set
	if c.TargetModel != "" {
		req["model"] = c.TargetModel
	}

	reqBody, _ := json.Marshal(req)
	proxyReq, err := http.NewRequest("POST", c.TargetURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Authorization", "Bearer "+c.TargetKey)

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// HandleModels returns model list in OpenAI format.
func (c *CCSwitch) HandleModels(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id":       c.TargetModel,
				"object":   "model",
				"owned_by": "ccswitch",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (c *CCSwitch) writeStreamResponse(w http.ResponseWriter, openAIBody []byte) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Parse SSE from OpenAI and convert each chunk to Anthropic SSE format
	lines := strings.Split(string(openAIBody), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			break
		}

		var chunk map[string]any
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}

		anthropicChunk := convertOpenAIChunkToAnthropic(chunk)
		chunkJSON, _ := json.Marshal(anthropicChunk)
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", string(chunkJSON))
	}
	w.(http.Flusher).Flush()
}

// ── Message Conversion ──

func convertAnthropicToOpenAIMessages(messages []any, systemMsg string) []map[string]any {
	var openAIMsgs []map[string]any
	if systemMsg != "" {
		openAIMsgs = append(openAIMsgs, map[string]any{
			"role":    "system",
			"content": systemMsg,
		})
	}
	for _, msg := range messages {
		m, _ := msg.(map[string]any)
		role, _ := m["role"].(string)
		if role == "assistant" {
			role = "assistant"
		} else {
			role = "user"
		}
		content := m["content"]
		// Anthropic content can be array or string
		if contentArr, ok := content.([]any); ok {
			var textParts []string
			for _, block := range contentArr {
				if blockMap, ok := block.(map[string]any); ok {
					if text, ok := blockMap["text"].(string); ok {
						textParts = append(textParts, text)
					}
				}
			}
			openAIMsgs = append(openAIMsgs, map[string]any{
				"role":    role,
				"content": strings.Join(textParts, "\n"),
			})
		} else {
			openAIMsgs = append(openAIMsgs, map[string]any{
				"role":    role,
				"content": fmt.Sprint(content),
			})
		}
	}
	return openAIMsgs
}

// ── Tool Conversion ──

// ConvertAnthropicToolsToOpenAI converts Anthropic tool definitions to OpenAI format.
func ConvertAnthropicToolsToOpenAI(anthropicTools []map[string]any) []map[string]any {
	var openAITools []map[string]any
	for _, tool := range anthropicTools {
		name, _ := tool["name"].(string)
		desc, _ := tool["description"].(string)
		schema, _ := tool["input_schema"].(map[string]any)

		openAITools = append(openAITools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters":  schema,
			},
		})
	}
	return openAITools
}

// ConvertOpenAIToolsToAnthropic converts OpenAI function definitions to Anthropic format.
func ConvertOpenAIToolsToAnthropic(openAITools []map[string]any) []map[string]any {
	var anthropicTools []map[string]any
	for _, tool := range openAITools {
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]any)

		anthropicTools = append(anthropicTools, map[string]any{
			"name":         name,
			"description":  desc,
			"input_schema": params,
		})
	}
	return anthropicTools
}

// ConvertAnthropicToolUseToOpenAI converts Anthropic tool_use blocks to OpenAI function_call.
func ConvertAnthropicToolUseToOpenAI(toolUse map[string]any) map[string]any {
	name, _ := toolUse["name"].(string)
	input, _ := json.Marshal(toolUse["input"])
	return map[string]any{
		"role": "assistant",
		"function_call": map[string]any{
			"name":      name,
			"arguments": string(input),
		},
	}
}

// ConvertOpenAIFunctionCallToAnthropic converts OpenAI function_call to Anthropic tool_use.
func ConvertOpenAIFunctionCallToAnthropic(msg map[string]any) map[string]any {
	fc, _ := msg["function_call"].(map[string]any)
	name, _ := fc["name"].(string)
	var input map[string]any
	json.Unmarshal([]byte(fmt.Sprint(fc["arguments"])), &input)

	return map[string]any{
		"role":  "assistant",
		"content": []map[string]any{{
			"type":  "tool_use",
			"name":  name,
			"input": input,
		}},
	}
}

func convertOpenAIToAnthropicResponse(openAIResp map[string]any, model string) map[string]any {
	choices, _ := openAIResp["choices"].([]any)
	resp := map[string]any{
		"id":    fmt.Sprint(openAIResp["id"]),
		"model": model,
		"type":  "message",
		"role":  "assistant",
		"stop_reason": "end_turn",
	}

	var content []map[string]any
	for _, choice := range choices {
		c, _ := choice.(map[string]any)
		msg, _ := c["message"].(map[string]any)
		text, _ := msg["content"].(string)
		if text != "" {
			content = append(content, map[string]any{
				"type": "text",
				"text": text,
			})
		}
		// Convert tool calls back
		if toolCalls, ok := msg["tool_calls"].([]any); ok {
			for _, tc := range toolCalls {
				tcMap, _ := tc.(map[string]any)
				fn, _ := tcMap["function"].(map[string]any)
				name, _ := fn["name"].(string)
				var input map[string]any
				json.Unmarshal([]byte(fmt.Sprint(fn["arguments"])), &input)
				content = append(content, map[string]any{
					"type":  "tool_use",
					"name":  name,
					"input": input,
				})
			}
		}
	}

	if len(content) == 0 {
		content = []map[string]any{{"type": "text", "text": ""}}
	}
	resp["content"] = content

	// Usage
	if usage, ok := openAIResp["usage"].(map[string]any); ok {
		inputTokens, _ := toInt(usage["prompt_tokens"])
		outputTokens, _ := toInt(usage["completion_tokens"])
		resp["usage"] = map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		}
	}

	return resp
}

func convertOpenAIChunkToAnthropic(chunk map[string]any) map[string]any {
	choices, _ := chunk["choices"].([]any)
	result := map[string]any{"type": "content_block_delta"}

	if len(choices) > 0 {
		c, _ := choices[0].(map[string]any)
		delta, _ := c["delta"].(map[string]any)
		content, _ := delta["content"].(string)
		result["delta"] = map[string]any{
			"type": "text_delta",
			"text": content,
		}
	}

	return result
}

// ── Helpers ──

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	default:
		return 0, false
	}
}

// ── Server ──

// ServeCCSwitch creates an HTTP handler for the CC Switch proxy.
// It auto-detects Anthropic vs OpenAI format based on the request body.
func ServeCCSwitch(targetURL, apiKey, model string) http.Handler {
	cc := NewCCSwitch(targetURL, apiKey, model)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", cc.HandleAnthropicRequest)
	mux.HandleFunc("/v1/chat/completions", cc.HandleOpenAIRequest)
	mux.HandleFunc("/v1/models", cc.HandleModels)

	// Catch-all for Claude Code compatibility
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/messages") {
			cc.HandleAnthropicRequest(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/chat/completions") {
			cc.HandleOpenAIRequest(w, r)
			return
		}
		if strings.Contains(r.URL.Path, "/models") {
			cc.HandleModels(w, r)
			return
		}
		log.Printf("[CCSwitch] Unknown path: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", 404)
	})

	return mux
}
