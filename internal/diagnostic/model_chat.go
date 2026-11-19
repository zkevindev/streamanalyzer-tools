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

func (p *ChatCompletions) SupportsImages() bool {
	return p.identity.Provider != ProviderDeepSeek
}

func (p *ChatCompletions) ExplainWithImages(ctx context.Context, report Report, images []ModelImage) (Explanation, error) {
	if len(images) == 0 {
		return p.Explain(ctx, report)
	}
	if err := validateModelImages(images); err != nil {
		return Explanation{}, err
	}
	if !p.SupportsImages() {
		return Explanation{}, ErrModelImagesUnsupported
	}
	data, err := modelInputWithImages(report, images)
	if err != nil {
		return Explanation{}, err
	}
	type imageURL struct {
		URL string `json:"url"`
	}
	type block struct {
		Type     string    `json:"type"`
		Text     string    `json:"text,omitempty"`
		ImageURL *imageURL `json:"image_url,omitempty"`
	}
	type message struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	content := []block{{Type: "text", Text: string(data)}}
	for _, image := range images {
		content = append(content, block{Type: "text", Text: modelImageLabel(image)}, block{Type: "image_url", ImageURL: &imageURL{URL: "data:image/jpeg;base64," + modelImageBase64(image)}})
	}
	request := struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}{Model: p.identity.Model, Messages: []message{{Role: "system", Content: explanationPrompt}, {Role: "user", Content: content}}}
	headers := make(map[string]string)
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	var response struct {
		Choices []struct {
			Message *chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := p.postImages(ctx, "/chat/completions", headers, request, &response); err != nil {
		return Explanation{}, err
	}
	if len(response.Choices) == 0 || response.Choices[0].Message == nil {
		return Explanation{}, fmt.Errorf("%w: 缺少可用的模型响应", ErrProvider)
	}
	return p.parse(response.Choices[0].Message.Content, report)
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
