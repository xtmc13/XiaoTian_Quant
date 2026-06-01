package indicator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/ai"
)

// SupportedLanguages maps locale codes to human-readable labels.
var SupportedLanguages = map[string]string{
	"zh-CN": "Simplified Chinese",
	"zh-TW": "Traditional Chinese",
	"en-US": "English",
	"ja-JP": "Japanese",
	"ko-KR": "Korean",
	"de-DE": "German",
	"fr-FR": "French",
	"vi-VN": "Vietnamese",
	"th-TH": "Thai",
	"ar-SA": "Arabic",
}

// TranslateIndicator uses LLM to translate name + description into all supported languages.
func TranslateIndicator(name, description, sourceLang string) (nameI18n, descI18n map[string]string, resolvedLang string) {
	if sourceLang == "" {
		sourceLang = DetectSourceLang(name + " " + description)
	}

	provider := getActiveAIProvider()
	if provider == nil {
		return nil, nil, sourceLang
	}

	systemPrompt := buildTranslationPrompt()
	userPrompt := fmt.Sprintf(
		"Source language: %s\n\nIndicator name:\n%s\n\nIndicator description:\n%s\n",
		sourceLang, name, description,
	)

	resp, err := provider.ChatCompletion(ai.CompletionRequest{
		Messages: []ai.ChatMessage{
			{Role: ai.RoleSystem, Content: systemPrompt},
			{Role: ai.RoleUser, Content: userPrompt},
		},
		Temperature: 0.2,
	})
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		return nil, nil, sourceLang
	}

	content := cleanMarkdownFences(resp.Choices[0].Message.Content)
	var result struct {
		Name        map[string]string `json:"name"`
		Description map[string]string `json:"description"`
	}
	if json.Unmarshal([]byte(content), &result) != nil {
		return nil, nil, sourceLang
	}

	nameI18n = make(map[string]string)
	descI18n = make(map[string]string)
	for code := range SupportedLanguages {
		if code == sourceLang {
			if name != "" {
				nameI18n[code] = name
			}
			if description != "" {
				descI18n[code] = description
			}
			continue
		}
		if v := result.Name[code]; v != "" {
			nameI18n[code] = v
		}
		if v := result.Description[code]; v != "" {
			descI18n[code] = v
		}
	}
	return nameI18n, descI18n, sourceLang
}

func buildTranslationPrompt() string {
	var langs []string
	for code, label := range SupportedLanguages {
		langs = append(langs, fmt.Sprintf("  - %s: %s", code, label))
	}
	return fmt.Sprintf(`You are a professional translator for quantitative trading indicators. Translate the given indicator name and description into all of the following languages.

Rules:
1. Preserve all technical jargon (RSI, MACD, EMA, Bollinger Bands, ATR, ADX, etc.) verbatim — never translate formula names.
2. Keep the name SHORT (ideally <= 4 words / <=14 CJK characters).
3. Keep the description tight: 1-2 sentences, plain prose, no markdown, no emojis.
4. If a target language matches the source language, copy the original unchanged.
5. Output STRICT JSON only — no markdown fences, no commentary.

Target languages:
%s

Output schema:
{
  "name":        { "<code>": "...", ... },
  "description": { "<code>": "...", ... }
}`, strings.Join(langs, "\n"))
}

// DetectSourceLang guesses the language of text based on character ranges.
func DetectSourceLang(text string) string {
	if text == "" {
		return "en-US"
	}
	has := func(lo, hi rune) bool {
		for _, r := range text {
			if r >= lo && r <= hi {
				return true
			}
		}
		return false
	}
	if has('぀', 'ヿ') {
		return "ja-JP"
	}
	if has('가', '힯') {
		return "ko-KR"
	}
	if has('؀', 'ۿ') {
		return "ar-SA"
	}
	if has('一', '鿿') {
		return "zh-CN"
	}
	return "en-US"
}
