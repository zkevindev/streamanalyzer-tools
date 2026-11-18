package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ProviderOpenAICompatible = "openai-compatible"
	ProviderDeepSeek         = "deepseek"
	ProviderAnthropic        = "anthropic"
	ProviderGemini           = "gemini"
)

var ErrModelChanged = errors.New("model configuration changed during explanation")

type ModelIdentity struct {
	Provider string
	Model    string
}

func (identity ModelIdentity) matches(result Explanation) bool {
	provider := result.Provider
	if provider == "" {
		provider = ProviderOpenAICompatible
	}
	return provider == identity.Provider && result.Model == identity.Model
}

type Explainer interface {
	Identity() ModelIdentity
	Explain(context.Context, Report) (Explanation, error)
}

type Hypothesis struct {
	Claim       string   `json:"claim"`
	EvidenceIDs []string `json:"evidence_ids"`
	NextCheck   string   `json:"next_check"`
}

type Explanation struct {
	Provider   string       `json:"provider,omitempty"`
	Model      string       `json:"model"`
	Summary    string       `json:"summary"`
	Hypotheses []Hypothesis `json:"hypotheses"`
}

const explanationPrompt = "你是流媒体排障助手。请用中文，仅返回 JSON，格式为 " +
	`{"summary":"观测摘要","hypotheses":[{"claim":"待验证假设","evidence_ids":["F001"],"next_check":"可执行的验证步骤"}]}` +
	"。结论限于采样点。每个假设必须引用输入 findings 中真实存在的 ID。" +
	"播放器首屏是从提交采样到视频呈现的观测，loadeddata_estimate 表示估算。" +
	"卡顿排除启动等待、暂停、跳转和后台时段。source 为 independent_pull 时播放器与采样使用独立连接，不能把两条连接的时间相关性当作根因。" +
	"stall_investigations 为选定卡顿的补查时序证据。截图和片段像素未提供给你，不得声称已看见画面、黑屏或解码结果。请引用真实证据 ID，明确观测和待验证假设。" +
	"没有足够证据时，给出简洁摘要并返回空 hypotheses。"

type modelClient struct {
	client   *http.Client
	baseURL  string
	identity ModelIdentity
	apiKey   string
}

func newModelClient(provider, baseURL, model, apiKey string) (modelClient, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return modelClient{}, fmt.Errorf("%w: 模型服务需配置模型名", ErrInvalid)
	}
	endpoint, err := parseTarget(strings.TrimSpace(baseURL))
	if err != nil || endpoint.RawQuery != "" || endpoint.ForceQuery {
		return modelClient{}, fmt.Errorf("%w: 模型服务 Base URL 无效", ErrInvalid)
	}
	return modelClient{
		baseURL:  strings.TrimRight(endpoint.String(), "/"),
		identity: ModelIdentity{Provider: provider, Model: model},
		apiKey:   strings.TrimSpace(apiKey),
		client: &http.Client{
			Timeout: 45 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c modelClient) Identity() ModelIdentity {
	return c.identity
}

func (c modelClient) post(ctx context.Context, path string, headers map[string]string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("%w: 无法编码请求", ErrProvider)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: 无法构造请求", ErrProvider)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return modelNetworkError(err, 0)
	}
	defer resp.Body.Close()
	limit := 256 << 10
	if resp.StatusCode != http.StatusOK {
		limit = 32 << 10
	}
	responseBytes, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if len(responseBytes) > limit {
		return &ModelConnectionError{Code: modelResponseTooLarge, StatusCode: resp.StatusCode}
	}
	if err != nil {
		return modelNetworkError(err, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return modelHTTPError(resp.StatusCode, responseBytes)
	}
	if code := modelDeclaredError(responseBytes); code != "" {
		return &ModelConnectionError{Code: code, StatusCode: resp.StatusCode}
	}
	if err := json.Unmarshal(responseBytes, output); err != nil {
		return &ModelConnectionError{Code: modelInvalidResponse, StatusCode: resp.StatusCode}
	}
	return nil
}

func (c modelClient) parse(content string, report Report) (Explanation, error) {
	content = strings.TrimSpace(content)
	if body, fenced := strings.CutPrefix(content, "```"); fenced {
		content = strings.TrimSpace(strings.TrimSuffix(body, "```"))
		if len(content) >= 4 && strings.EqualFold(content[:4], "json") {
			content = strings.TrimSpace(content[4:])
		}
	}
	var result Explanation
	if err := json.Unmarshal([]byte(content), &result); err != nil || !validExplanation(result, report.Findings) {
		return Explanation{}, fmt.Errorf("%w: 模型输出格式或证据引用无效", ErrProvider)
	}
	result.Provider = c.identity.Provider
	result.Model = c.identity.Model
	return result, nil
}

type modelFinding struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	StartMS     int64  `json:"start_ms"`
	EndMS       int64  `json:"end_ms"`
	Occurrences int    `json:"occurrences,omitempty"`
	NextCheck   string `json:"next_check,omitempty"`
}

type modelPlayer struct {
	State             string        `json:"state"`
	Source            string        `json:"source"`
	Engine            string        `json:"engine"`
	StartOffsetMS     int64         `json:"start_offset_ms"`
	FirstScreenMS     *int64        `json:"first_screen_ms,omitempty"`
	FirstScreenMethod string        `json:"first_screen_method,omitempty"`
	FirstPlayingMS    *int64        `json:"first_playing_ms,omitempty"`
	StallCount        int           `json:"stall_count"`
	TotalStallMS      int64         `json:"total_stall_ms"`
	MaxStallMS        int64         `json:"max_stall_ms"`
	ActiveMS          int64         `json:"active_ms"`
	StallRatio        float64       `json:"stall_ratio"`
	MinBufferMS       *int64        `json:"min_buffer_ms,omitempty"`
	MaxBufferMS       int64         `json:"max_buffer_ms"`
	StallThresholdMS  int64         `json:"stall_threshold_ms"`
	Stalls            []PlayerStall `json:"stalls"`
}

// Keep connection metadata and header text local even as the report grows.
type modelHTTPRequest struct {
	Number       int            `json:"number"`
	Kind         string         `json:"kind,omitempty"`
	StartMS      int64          `json:"start_ms"`
	EndMS        int64          `json:"end_ms"`
	TotalMS      int64          `json:"total_ms"`
	BodyEndMS    *int64         `json:"body_end_ms,omitempty"`
	HTTPStatus   int            `json:"http_status,omitempty"`
	TTFBMS       *int64         `json:"ttfb_ms,omitempty"`
	Stages       []CaptureStage `json:"stages,omitempty"`
	FailureStage string         `json:"failure_stage,omitempty"`
	FailureCode  string         `json:"failure_code,omitempty"`
}

type modelStallInvestigation struct {
	ID                  string               `json:"id"`
	Stall               PlayerStall          `json:"stall"`
	Source              string               `json:"source"`
	WindowStartMS       int64                `json:"window_start_ms"`
	WindowEndMS         int64                `json:"window_end_ms"`
	Gaps                []ReceptionGap       `json:"gaps"`
	BufferSamples       []PlayerBufferSample `json:"buffer_samples"`
	Keyframes           []Event              `json:"keyframes"`
	Requests            []modelHTTPRequest   `json:"requests,omitempty"`
	Notes               []string             `json:"notes"`
	MediaStatus         string               `json:"media_status"`
	MediaAlignment      string               `json:"media_alignment,omitempty"`
	ClipCount           int                  `json:"clip_count"`
	ScreenshotCount     int                  `json:"screenshot_count"`
	ImagePixelsProvided bool                 `json:"image_pixels_provided"`
}

func modelRequestTimings(requests []HTTPRequestTiming) []modelHTTPRequest {
	var result []modelHTTPRequest
	for _, request := range requests {
		result = append(result, modelHTTPRequest{
			Number: request.Number, Kind: request.Kind, StartMS: request.StartMS, EndMS: request.EndMS, TotalMS: request.TotalMS,
			BodyEndMS: request.BodyEndMS, HTTPStatus: request.HTTPStatus, TTFBMS: request.TTFBMS, Stages: request.Stages,
			FailureStage: request.FailureStage, FailureCode: request.FailureCode,
		})
	}
	return result
}

// Only tool-generated findings and numeric measurements leave the process.
func modelInput(report Report) ([]byte, error) {
	obs := report.Manifest.Observation
	input := struct {
		Protocol            string                    `json:"protocol"`
		ElapsedMS           int64                     `json:"elapsed_ms"`
		HTTPStatus          int                       `json:"http_status"`
		TTFBMS              *int64                    `json:"ttfb_ms,omitempty"`
		EndReason           string                    `json:"end_reason"`
		Metrics             Metrics                   `json:"metrics"`
		HLS                 *HLSMetrics               `json:"hls,omitempty"`
		MPEGTS              *MPEGTSMetrics            `json:"mpegts,omitempty"`
		FirstPayloadMS      *int64                    `json:"first_payload_ms,omitempty"`
		Stages              []CaptureStage            `json:"stages,omitempty"`
		Requests            []modelHTTPRequest        `json:"requests,omitempty"`
		RequestsTruncated   int                       `json:"requests_truncated,omitempty"`
		FailureStage        string                    `json:"failure_stage,omitempty"`
		FailureCode         string                    `json:"failure_code,omitempty"`
		Findings            []modelFinding            `json:"findings"`
		Player              *modelPlayer              `json:"player,omitempty"`
		StallInvestigations []modelStallInvestigation `json:"stall_investigations,omitempty"`
	}{
		Protocol: report.Manifest.Protocol, ElapsedMS: obs.ElapsedMS, HTTPStatus: obs.HTTPStatus, TTFBMS: obs.TTFBMS,
		EndReason: obs.EndReason, Metrics: report.Metrics, HLS: report.HLS, MPEGTS: report.MPEGTS,
		FirstPayloadMS: obs.FirstPayloadMS, Stages: obs.Stages, FailureStage: obs.FailureStage, FailureCode: obs.FailureCode,
		Findings: []modelFinding{},
	}
	input.Requests = modelRequestTimings(report.Requests)
	input.RequestsTruncated = report.RequestsTruncated
	if player := report.Player; player != nil {
		input.Player = &modelPlayer{
			State: player.State, Source: player.Source, Engine: player.Engine, StartOffsetMS: player.StartOffsetMS,
			FirstScreenMS: player.FirstScreenMS, FirstScreenMethod: player.FirstScreenMethod, FirstPlayingMS: player.FirstPlayingMS,
			StallCount: player.StallCount, TotalStallMS: player.TotalStallMS, MaxStallMS: player.MaxStallMS, ActiveMS: player.ActiveMS,
			StallRatio: player.StallRatio, MinBufferMS: player.MinBufferMS, MaxBufferMS: player.MaxBufferMS, StallThresholdMS: player.StallThresholdMS,
			Stalls: player.Stalls[:min(20, len(player.Stalls))],
		}
	}
	if len(input.Requests) == 0 {
		input.Requests = modelRequestTimings(obs.Requests)
	}
	for _, item := range report.StallInvestigations {
		entry := modelStallInvestigation{ID: item.ID, Stall: item.Stall, Source: item.Correlation.Source,
			WindowStartMS: item.Correlation.WindowStartMS, WindowEndMS: item.Correlation.WindowEndMS,
			Gaps: item.Correlation.Gaps, BufferSamples: item.Correlation.BufferSamples, Keyframes: item.Correlation.Keyframes,
			Requests: modelRequestTimings(item.Correlation.Requests), Notes: item.Correlation.Notes,
			MediaStatus: item.Media.Status, MediaAlignment: item.Media.Alignment}
		for _, asset := range item.Media.Assets {
			if asset.Kind == "clip" {
				entry.ClipCount++
			} else if asset.Kind == "screenshot" {
				entry.ScreenshotCount++
			}
		}
		input.StallInvestigations = append(input.StallInvestigations, entry)
	}
	for _, finding := range report.Findings {
		input.Findings = append(input.Findings, modelFinding{
			ID: finding.ID, Code: finding.Code, Severity: finding.Severity, Message: finding.Message,
			StartMS: finding.StartMS, EndMS: finding.EndMS,
			Occurrences: finding.Occurrences, NextCheck: finding.NextCheck,
		})
	}
	return json.Marshal(input)
}

func validExplanation(result Explanation, findings []Finding) bool {
	if strings.TrimSpace(result.Summary) == "" || len(result.Summary) > 4096 || len(result.Hypotheses) > 8 {
		return false
	}
	known := make(map[string]bool, len(findings))
	for _, finding := range findings {
		known[finding.ID] = true
	}
	for _, item := range result.Hypotheses {
		if strings.TrimSpace(item.Claim) == "" || len(item.Claim) > 4096 ||
			strings.TrimSpace(item.NextCheck) == "" || len(item.NextCheck) > 4096 ||
			len(item.EvidenceIDs) == 0 || len(item.EvidenceIDs) > len(known) {
			return false
		}
		for _, id := range item.EvidenceIDs {
			if !known[id] {
				return false
			}
		}
	}
	return true
}
