package diagnostic

import (
	"context"
	"fmt"
	"strings"
)

type ChatCompletions struct {
	modelClient
}

func NewOpenAI(baseURL, model, apiKey string) (*ChatCompletions, error) {
	return newChatCompletions(ProviderOpenAICompatible, baseURL, model, apiKey)
}

func NewDeepSeek(baseURL, model, apiKey string) (*ChatCompletions, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.deepseek.com"
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: DeepSeek 需要 API Key", ErrInvalid)
	}
	return newChatCompletions(ProviderDeepSeek, baseURL, model, apiKey)
}

func newChatCompletions(provider, baseURL, model, apiKey string) (*ChatCompletions, error) {
	client, err := newModelClient(provider, baseURL, model, apiKey)
	if err != nil {
		return nil, err
	}
	return &ChatCompletions{modelClient: client}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOption struct {
	Type string `json:"type"`
}

func (p *ChatCompletions) Explain(ctx context.Context, report Report) (Explanation, error) {
	data, err := modelInput(report)
	if err != nil {
		return Explanation{}, err
	}
	answer, err := p.complete(ctx, explanationPrompt, string(data), 4096)
	if err != nil {
		return Explanation{}, err
	}
	return p.parse(answer, report)
}

func (p *ChatCompletions) testConnection(ctx context.Context) error {
	_, err := p.complete(ctx, connectionProbeSystem, connectionProbeInput, 64)
	return err
}

func (p *ChatCompletions) complete(ctx context.Context, system, input string, deepSeekMaxTokens int) (string, error) {
	request := struct {
		Model          string        `json:"model"`
		Messages       []chatMessage `json:"messages"`
		ResponseFormat *chatOption   `json:"response_format,omitempty"`
		Thinking       *chatOption   `json:"thinking,omitempty"`
		MaxTokens      int           `json:"max_tokens,omitempty"`
	}{
		Model: p.identity.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: input},
		},
	}
	if p.identity.Provider == ProviderDeepSeek {
		request.ResponseFormat = &chatOption{Type: "json_object"}
		request.Thinking = &chatOption{Type: "disabled"}
		request.MaxTokens = deepSeekMaxTokens
	}
	headers := make(map[string]string)
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	var response struct {
		Choices []struct {
			Message *chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := p.post(ctx, "/chat/completions", headers, request, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 || response.Choices[0].Message == nil {
		return "", fmt.Errorf("%w: 缺少可用的模型响应", ErrProvider)
	}
	return response.Choices[0].Message.Content, nil
}
