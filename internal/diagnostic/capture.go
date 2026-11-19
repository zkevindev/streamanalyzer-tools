package diagnostic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/yutopp/go-rtmp"
)

func parseTarget(rawURL string) (*url.URL, error) {
	return parseProtocolURL(rawURL, ProtocolHTTPFLV)
}

func parseSampleTarget(rawURL, protocol string) (*url.URL, error) {
	target, err := parseProtocolURL(rawURL, protocol)
	if err != nil {
		return nil, err
	}
	if protocol == ProtocolRTMP {
		if _, err := rtmp.ParsePlayURL(target.String()); err != nil {
			return nil, fmt.Errorf("%w: RTMP 播放地址需要包含应用名和流名", ErrInvalid)
		}
	}
	return target, nil
}

func parseProtocolURL(rawURL, protocol string) (*url.URL, error) {
	if len(rawURL) > 8192 {
		return nil, fmt.Errorf("%w: URL 太长", ErrInvalid)
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" || (target.User != nil && protocol != ProtocolRTSP && protocol != ProtocolRTSPS) {
		return nil, fmt.Errorf("%w: 播放地址格式无效", ErrInvalid)
	}
	switch protocol {
	case ProtocolHTTPFLV, ProtocolHLS, ProtocolHTTPTS:
		if target.Scheme != "http" && target.Scheme != "https" {
			return nil, fmt.Errorf("%w: 请输入 HTTP 或 HTTPS 播放地址", ErrInvalid)
		}
	case ProtocolRTMP:
		if target.Scheme != "rtmp" && target.Scheme != "rtmps" {
			return nil, fmt.Errorf("%w: 请输入 RTMP 或 RTMPS 播放地址", ErrInvalid)
		}
	case ProtocolRTSP, ProtocolRTSPS:
		if target.Scheme != protocol {
			return nil, fmt.Errorf("%w: 请输入 %s 播放地址", ErrInvalid, protocol)
		}
	case ProtocolUDPTS, ProtocolRTPTS:
		scheme := "udp"
		if protocol == ProtocolRTPTS {
			scheme = "rtp"
		}
		address, err := netip.ParseAddrPort(target.Host)
		query, queryErr := url.ParseQuery(target.RawQuery)
		if target.Scheme != scheme || err != nil || !address.Addr().Is4() || address.Port() == 0 ||
			target.Path != "" || target.Fragment != "" || queryErr != nil || len(query) > 1 {
			return nil, fmt.Errorf("%w: 请输入 IPv4 接收地址和端口，例如 %s://0.0.0.0:5000", ErrInvalid, scheme)
		}
		for key, values := range query {
			if key != "interface" || len(values) != 1 || values[0] == "" || !address.Addr().IsMulticast() {
				return nil, fmt.Errorf("%w: interface 参数仅用于指定组播接收网卡", ErrInvalid)
			}
		}
	default:
		return nil, fmt.Errorf("%w: 不支持的采样协议", ErrInvalid)
	}
	target.Fragment = ""
	return target, nil
}

func origin(target *url.URL) string {
	return (&url.URL{Scheme: target.Scheme, Host: target.Host}).String()
}

func captureClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  15 * time.Second,
			DisableCompression:     true,
			MaxResponseHeaderBytes: 1 << 20,
			DisableKeepAlives:      true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return errors.New("too many redirects")
			}
			if _, err := parseTarget(req.URL.String()); err != nil {
				return err
			}
			// A signed playback URL must not leak through a redirect's Referer.
			req.Header.Del("Referer")
			return nil
		},
	}
}

func sample(ctx context.Context, client *http.Client, dir string, target *url.URL, seconds int) (Report, error) {
	stream, err := newArtifact(dir, streamName)
	if err != nil {
		return Report{}, err
	}
	timeline, err := newArtifact(dir, timelineName)
	if err != nil {
		return Report{}, errors.Join(err, stream.close())
	}

	start := time.Now()
	observation := Observation{Origin: origin(target), StartedAt: start.UTC(), RequestedSeconds: seconds}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()

	events, writeErr := capture(ctx, client, target, start, &observation, stream, timeline)
	if player := capturePlayer(ctx); player != nil {
		player.EndMedia()
	}
	observation.ElapsedMS = time.Since(start).Milliseconds()
	if err := errors.Join(writeErr, stream.close(), timeline.close()); err != nil {
		return Report{}, err
	}

	manifest := Manifest{Version: multiProtocolSchemaVersion, Protocol: ProtocolHTTPFLV, Observation: observation, Stream: stream.artifact(), Timeline: timeline.artifact()}
	return analyze(manifest, events), nil
}

func capture(ctx context.Context, client *http.Client, target *url.URL, start time.Time, observation *Observation, stream, timeline *artifactWriter) ([]Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		observation.FailureStage, observation.FailureCode = httpFailure("request_send", err)
		observation.EndReason = "request_error"
		return nil, nil
	}
	req.Header.Set("User-Agent", "StreamAnalyzer-Diagnostic/1.0")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := doObservedRequest(client, req, observation, start)
	if err != nil {
		observation.EndReason = captureEnd(ctx, "request_error")
		return nil, nil
	}
	defer resp.Body.Close()

	observation.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		observation.EndReason = "http_status"
		observation.FailureStage, observation.FailureCode = "response_headers", "http_status"
		return nil, nil
	}
	stage := len(observation.Stages)
	observation.Stages = append(observation.Stages, CaptureStage{Name: "receive", StartMS: time.Since(start).Milliseconds()})
	defer func() {
		observation.Stages[stage].EndMS = time.Since(start).Milliseconds()
		if observation.EndReason == "body_error" {
			observation.FailureStage, observation.FailureCode = "receive", "body_error"
		} else if observation.EndReason == "duration_limit" && observation.FirstPayloadMS == nil {
			observation.FailureStage, observation.FailureCode = "receive", "timeout"
		}
		recordHTTPBody(observation, observation.Stages[stage])
	}()

	body := io.LimitReader(resp.Body, maxStreamBytes)
	var output io.Writer = stream
	if player := capturePlayer(ctx); player != nil {
		output = playerMediaWriter{output, player}
	}
	tapped := io.TeeReader(bufio.NewReaderSize(body, 64<<10), payloadWriter{output, observation, start})
	parser, err := openFLV(tapped)
	if err != nil {
		observation.EndReason = bodyEnd(ctx, stream.bytes, err)
		return nil, stream.err
	}

	var events []Event
	encoder := json.NewEncoder(timeline)
	for len(events) < maxTags {
		if ctx.Err() != nil {
			observation.EndReason = captureEnd(ctx, "")
			return events, nil
		}
		event, err := parser.next()
		if err != nil {
			observation.EndReason = bodyEnd(ctx, stream.bytes, err)
			return events, stream.err
		}
		event.ArrivalMS = time.Since(start).Milliseconds()
		if err := encoder.Encode(event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	observation.EndReason = "tag_limit"
	return events, nil
}

func bodyEnd(ctx context.Context, bytes int64, err error) string {
	switch {
	case ctx.Err() != nil:
		return captureEnd(ctx, "")
	case bytes >= maxStreamBytes:
		return "size_limit"
	case errors.Is(err, errFLV):
		return "invalid_flv"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "truncated_tag"
	case errors.Is(err, io.EOF):
		return "eof"
	default:
		return "body_error"
	}
}

func captureEnd(ctx context.Context, fallback string) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "duration_limit"
	case errors.Is(ctx.Err(), context.Canceled):
		return "canceled"
	default:
		return fallback
	}
}
