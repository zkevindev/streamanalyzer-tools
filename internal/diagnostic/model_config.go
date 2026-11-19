package diagnostic

import (
	"context"
	"fmt"
	"strings"
)

const connectionProbeSystem = "This is an API connectivity test. Reply with a short json object."
const connectionProbeInput = `Return this object: {"ok":true}`

// ModelConfig is the configuration shared by the CLI and desktop settings.
// APIKey is excluded from JSON so configuration views cannot expose it.
type ModelConfig struct {
	ImageInput string `json:"image_input,omitempty"`
	Provider   string `json:"provider"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	APIKey     string `json:"-"`
}

// Normalize supplies the base URL for native providers while retaining a
// zero value for an unconfigured model.
func (c ModelConfig) Normalize() ModelConfig {
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	if endpoint := strings.TrimRight(c.BaseURL, "/"); endpoint != "" {
		c.BaseURL = endpoint
	}
	c.Model = strings.TrimSpace(c.Model)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.ImageInput = strings.ToLower(strings.TrimSpace(c.ImageInput))
	if c == (ModelConfig{}) {
		return c
	}
	if c.ImageInput == "" {
		c.ImageInput = ImageInputAuto
	}
	if c.Provider == "" {
		c.Provider = ProviderOpenAICompatible
	}
	if c.BaseURL == "" {
		switch c.Provider {
		case ProviderDeepSeek:
			c.BaseURL = "https://api.deepseek.com"
		case ProviderAnthropic:
			c.BaseURL = "https://api.anthropic.com/v1"
		case ProviderGemini:
			c.BaseURL = geminiBaseURL
		}
	}
	return c
}

func (c ModelConfig) NewExplainer() (Explainer, error) {
	c = c.Normalize()
	if c == (ModelConfig{}) {
		return nil, nil
	}
	if c.ImageInput != ImageInputAuto && c.ImageInput != ImageInputText {
		return nil, fmt.Errorf("%w: 截图输入须为 auto 或 text", ErrInvalid)
	}
	var model Explainer
	var err error
	switch c.Provider {
	case ProviderOpenAICompatible:
		model, err = NewOpenAI(c.BaseURL, c.Model, c.APIKey)
	case ProviderDeepSeek:
		model, err = NewDeepSeek(c.BaseURL, c.Model, c.APIKey)
	case ProviderAnthropic:
		model, err = NewAnthropic(c.BaseURL, c.Model, c.APIKey)
	case ProviderGemini:
		model, err = NewGemini(c.BaseURL, c.Model, c.APIKey)
	default:
		return nil, fmt.Errorf("%w: 模型厂商须为 openai-compatible、deepseek、anthropic 或 gemini", ErrInvalid)
	}
	if err != nil {
		return nil, err
	}
	if configurable, ok := model.(interface{ setImageInput(string) }); ok {
		configurable.setImageInput(c.ImageInput)
	}
	return model, nil
}

// TestConnection checks that the configured model can generate a response.
// It sends a fixed prompt and does not use diagnostic report data.
func (c ModelConfig) TestConnection(ctx context.Context) error {
	model, err := c.NewExplainer()
	if err != nil {
		return err
	}
	if model == nil {
		return ErrUnconfigured
	}
	prober, ok := model.(interface{ testConnection(context.Context) error })
	if !ok {
		return fmt.Errorf("%w: 模型厂商不支持连接测试", ErrProvider)
	}
	return prober.testConnection(ctx)
}
