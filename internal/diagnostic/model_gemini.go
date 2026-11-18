package diagnostic

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

var geminiModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Gemini uses the native Gemini Developer API.
type Gemini struct {
	modelClient
}

func NewGemini(baseURL, model, apiKey string) (*Gemini, error) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if !geminiModelID.MatchString(model) {
		return nil, fmt.Errorf("%w: Gemini 模型名无效", ErrInvalid)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: Gemini 需要 API Key", ErrInvalid)
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = geminiBaseURL
	}
	client, err := newModelClient(ProviderGemini, baseURL, model, apiKey)
	if err != nil {
		return nil, err
	}
	return &Gemini{modelClient: client}, nil
}

type geminiPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

func (g *Gemini) Explain(ctx context.Context, report Report) (Explanation, error) {
	data, err := modelInput(report)
	if err != nil {
		return Explanation{}, err
	}
	candidate, err := g.generate(ctx, explanationPrompt, string(data), 0)
	if err != nil {
		return Explanation{}, err
	}
	if candidate.FinishReason != "" && candidate.FinishReason != "STOP" {
		return Explanation{}, fmt.Errorf("%w: Gemini 解读未完整生成", ErrProvider)
	}
	var answer strings.Builder
	for _, part := range candidate.Content.Parts {
		if !part.Thought {
			answer.WriteString(part.Text)
		}
	}
	content := answer.String()
	if strings.TrimSpace(content) == "" {
		return Explanation{}, fmt.Errorf("%w: Gemini 未生成可用的解读", ErrProvider)
	}
	return g.parse(content, report)
}

func (g *Gemini) testConnection(ctx context.Context) error {
	candidate, err := g.generate(ctx, connectionProbeSystem, connectionProbeInput, 128)
	if err != nil {
		return err
	}
	if len(candidate.Content.Parts) == 0 && candidate.FinishReason != "MAX_TOKENS" {
		return fmt.Errorf("%w: Gemini 未返回生成内容", ErrProvider)
	}
	return nil
}

func (g *Gemini) generate(ctx context.Context, system, input string, maxTokens int) (geminiCandidate, error) {
	request := struct {
		SystemInstruction geminiContent   `json:"systemInstruction"`
		Contents          []geminiContent `json:"contents"`
		GenerationConfig  struct {
			ResponseMimeType string `json:"responseMimeType"`
			MaxOutputTokens  int    `json:"maxOutputTokens,omitempty"`
		} `json:"generationConfig"`
	}{
		SystemInstruction: geminiContent{Parts: []geminiPart{{Text: system}}},
		Contents:          []geminiContent{{Role: "user", Parts: []geminiPart{{Text: input}}}},
	}
	request.GenerationConfig.ResponseMimeType = "application/json"
	request.GenerationConfig.MaxOutputTokens = maxTokens
	var response struct {
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Candidates []geminiCandidate `json:"candidates"`
	}
	path := "/models/" + g.identity.Model + ":generateContent"
	if err := g.post(ctx, path, map[string]string{"x-goog-api-key": g.apiKey}, request, &response); err != nil {
		return geminiCandidate{}, err
	}
	if len(response.Candidates) == 0 || (response.PromptFeedback.BlockReason != "" && response.PromptFeedback.BlockReason != "BLOCK_REASON_UNSPECIFIED") {
		return geminiCandidate{}, fmt.Errorf("%w: Gemini 未生成可用的响应", ErrProvider)
	}
	return response.Candidates[0], nil
}
