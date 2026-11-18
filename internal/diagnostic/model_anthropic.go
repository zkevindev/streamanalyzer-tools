package diagnostic

import (
	"context"
	"fmt"
	"strings"
)

const anthropicVersion = "2026-10-21"

type Anthropic struct {
	modelClient
}

func NewAnthropic(baseURL, model, apiKey string) (*Anthropic, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: Anthropic 需要 API Key", ErrInvalid)
	}
	client, err := newModelClient(ProviderAnthropic, baseURL, model, apiKey)
	if err != nil {
		return nil, err
	}
	return &Anthropic{modelClient: client}, nil
}

func (p *Anthropic) Explain(ctx context.Context, report Report) (Explanation, error) {
	data, err := modelInput(report)
	if err != nil {
		return Explanation{}, err
	}
	answer, err := p.complete(ctx, explanationPrompt, string(data), 2048)
	if err != nil {
		return Explanation{}, err
	}
	if answer == "" {
		return Explanation{}, fmt.Errorf("%w: Anthropic 未返回可用文本", ErrProvider)
	}
	return p.parse(answer, report)
}

func (p *Anthropic) testConnection(ctx context.Context) error {
	_, err := p.complete(ctx, connectionProbeSystem, connectionProbeInput, 128)
	return err
}

func (p *Anthropic) complete(ctx context.Context, system, text string, maxTokens int) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	input := struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		System    string    `json:"system"`
		Messages  []message `json:"messages"`
	}{
		Model: p.identity.Model, MaxTokens: maxTokens, System: system,
		Messages: []message{{Role: "user", Content: text}},
	}
	var output struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	headers := map[string]string{"x-api-key": p.apiKey, "anthropic-version": anthropicVersion}
	if err := p.post(ctx, "/messages", headers, input, &output); err != nil {
		return "", err
	}
	if len(output.Content) == 0 && (output.Content == nil || output.StopReason != "max_tokens") {
		return "", fmt.Errorf("%w: Anthropic 未返回生成内容", ErrProvider)
	}
	var content strings.Builder
	for _, block := range output.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	return content.String(), nil
}
