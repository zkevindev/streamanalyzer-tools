package diagnostic

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http/httptrace"
	"slices"
	"sync"
	"time"
)

// recordHTTPAttempt stores timings, while keeping endpoint names and network
// error text out of the report. Trace callbacks can run on transport goroutines.
func recordHTTPAttempt(ctx context.Context, observation *Observation, start time.Time) (context.Context, func(error)) {
	var mu sync.Mutex
	var stages []CaptureStage
	active := make(map[string]int)
	var firstByte *int64
	done := false
	lastStage := "response_headers"
	begin := func(name, key string) {
		mu.Lock()
		defer mu.Unlock()
		if done || len(stages) >= 64 {
			return
		}
		active[key] = len(stages)
		stages = append(stages, CaptureStage{Name: name, StartMS: time.Since(start).Milliseconds(), EndMS: -1})
		lastStage = name
	}
	end := func(key string) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		if index, ok := active[key]; ok {
			stages[index].EndMS = time.Since(start).Milliseconds()
			delete(active, key)
		}
	}
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { begin("dns", "dns") },
		DNSDone:           func(httptrace.DNSDoneInfo) { end("dns") },
		ConnectStart:      func(network, address string) { begin("connect", network+address) },
		ConnectDone:       func(network, address string, _ error) { end(network + address) },
		TLSHandshakeStart: func() { begin("tls", "tls") },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { end("tls") },
		GotConn:           func(httptrace.GotConnInfo) { begin("response_headers", "response_headers") },
		GotFirstResponseByte: func() {
			end("response_headers")
			mu.Lock()
			defer mu.Unlock()
			if !done {
				ms := time.Since(start).Milliseconds()
				firstByte = &ms
			}
		},
	}
	finish := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		done = true
		now := time.Since(start).Milliseconds()
		for index := range stages {
			if stages[index].EndMS < 0 {
				stages[index].EndMS = now
			}
		}
		slices.SortStableFunc(stages, func(a, b CaptureStage) int {
			if a.StartMS < b.StartMS {
				return -1
			}
			if a.StartMS > b.StartMS {
				return 1
			}
			return 0
		})
		observation.Stages = append(observation.Stages, stages...)
		observation.TTFBMS = firstByte
		if err != nil {
			observation.FailureStage = lastStage
			observation.FailureCode = modelNetworkError(err, 0).Code
			if _, waitingForHeaders := active["response_headers"]; waitingForHeaders {
				observation.FailureStage = "response_headers"
			}
			switch observation.FailureCode {
			case "dns_error":
				observation.FailureStage = "dns"
			case "tls_error":
				observation.FailureStage = "tls"
			case "connection_refused":
				observation.FailureStage = "connect"
			}
		}
	}
	return httptrace.WithClientTrace(ctx, trace), finish
}

func markFirstPayload(observation *Observation, start time.Time) {
	if observation.FirstPayloadMS == nil {
		ms := time.Since(start).Milliseconds()
		observation.FirstPayloadMS = &ms
	}
}

type payloadWriter struct {
	writer      io.Writer
	observation *Observation
	start       time.Time
}

func (w payloadWriter) Write(data []byte) (int, error) {
	if len(data) > 0 {
		markFirstPayload(w.observation, w.start)
	}
	return w.writer.Write(data)
}

func validCaptureStage(name string) bool {
	switch name {
	case "dns", "connect", "tls", "response_headers", "listen", "receive", "handshake", "play":
		return true
	default:
		return false
	}
}

func validCaptureFailure(code string) bool {
	switch code {
	case "", "timeout", "canceled", "dns_error", "tls_error", "connection_refused", "network_error",
		"http_status", "body_error", "udp_bind_error", "udp_read_error", "rtmp_error":
		return true
	default:
		return false
	}
}

func validateObservationDetails(observation Observation) error {
	invalid := func() error { return fmt.Errorf("%w: 连接阶段或首包时间记录无效", ErrInvalid) }
	if observation.FirstPayloadMS != nil && (*observation.FirstPayloadMS < 0 || *observation.FirstPayloadMS > observation.ElapsedMS) {
		return invalid()
	}
	if len(observation.Stages) > 128 || !validCaptureFailure(observation.FailureCode) ||
		(observation.FailureStage != "" && !validCaptureStage(observation.FailureStage)) {
		return invalid()
	}
	for _, stage := range observation.Stages {
		if !validCaptureStage(stage.Name) || stage.StartMS < 0 || stage.EndMS < stage.StartMS || stage.EndMS > observation.ElapsedMS {
			return invalid()
		}
	}
	return nil
}

func captureFailureText(observation Observation) string {
	switch observation.FailureCode {
	case "udp_bind_error":
		return "UDP 接收端口绑定或组播入组失败，请检查本机地址、端口占用和接收网卡"
	case "udp_read_error":
		return "UDP 数据接收失败，请检查接收网卡及系统套接字状态"
	case "body_error":
		return "读取媒体数据时连接中断，请检查服务端断开和网络连接"
	case "rtmp_error":
		return "RTMP 连接或播放协商失败，请检查应用名、流名和服务端状态"
	case "dns_error":
		return "播放地址 DNS 解析失败，请检查域名和本机 DNS 配置"
	case "tls_error":
		return "播放连接 TLS 握手或证书校验失败，请检查服务证书和系统时间"
	case "timeout":
		if observation.FailureStage == "receive" {
			return "采样窗口内未收到媒体数据，请检查发送端、接收地址和防火墙配置"
		}
		return "连接阶段请求超时，请检查阶段耗时、服务可用性和网络链路"
	case "connection_refused":
		return "连接被拒绝，请检查播放服务端口和监听状态"
	case "network_error":
		return "无法建立播放连接，请检查目标服务状态和网络链路"
	case "canceled":
		return ""
	case "http_status", "":
		return ""
	default:
		return ""
	}
}
