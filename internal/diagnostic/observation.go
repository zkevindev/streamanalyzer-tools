package diagnostic

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

const (
	maxHTTPRequests      = 8
	maxHTTPRequestStages = 16
	maxHTTPSetupStages   = 10 // retain room for request and response phases
)

// Observe each RoundTrip, so a redirect response gets its own status and
// timings. The shallow client copy keeps the original redirect, cookie, and
// timeout behavior and continues to use the original transport.
func doObservedRequest(client *http.Client, req *http.Request, observation *Observation, start time.Time) (*http.Response, error) {
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &observedHTTPTransport{base: base, sampleStart: start}
	observed := *client
	observed.Transport = transport
	resp, err := observed.Do(req)
	observation.Requests = transport.requests
	if len(transport.requests) == 0 {
		if err != nil {
			observation.FailureStage, observation.FailureCode = httpFailure("request_send", err)
		}
		return resp, err
	}
	last := &transport.requests[len(transport.requests)-1]
	if err != nil && last.FailureCode == "" {
		// Client.Do can return an error after RoundTrip succeeded, for example
		// when the client's redirect policy rejects the next request.
		last.FailureStage, last.FailureCode = httpFailure("response_headers", err)
		switch last.HTTPStatus {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
			http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			last.FailureCode = "redirect_error"
		}
	}
	observation.Stages = append(observation.Stages, last.Stages...)
	if last.TTFBMS != nil {
		ms := last.StartMS + *last.TTFBMS
		observation.TTFBMS = &ms
	}
	observation.FailureStage, observation.FailureCode = last.FailureStage, last.FailureCode
	return resp, err
}

type observedHTTPTransport struct {
	base        http.RoundTripper
	sampleStart time.Time
	requests    []HTTPRequestTiming
}

func (t *observedHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(t.requests) == maxHTTPRequests {
		return t.base.RoundTrip(req)
	}
	attempt := httpAttempt{
		sampleStart: t.sampleStart,
		request: HTTPRequestTiming{
			Number: len(t.requests) + 1, StartMS: time.Since(t.sampleStart).Milliseconds(),
		},
		active: make(map[string]int),
	}
	ctx := httptrace.WithClientTrace(req.Context(), attempt.trace())
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	t.requests = append(t.requests, attempt.finish(resp, err))
	return resp, err
}

type httpAttempt struct {
	mu          sync.Mutex
	sampleStart time.Time
	request     HTTPRequestTiming
	active      map[string]int
	lastStage   string
	finished    bool
}

func (a *httpAttempt) trace() *httptrace.ClientTrace {
	begin := func(name, key string) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.finished && len(a.request.Stages) < maxHTTPSetupStages {
			a.begin(name, key, a.now())
		}
	}
	end := func(key string) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.finished {
			a.end(key, a.now())
		}
	}
	return &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { begin("dns", "dns") },
		DNSDone:              func(httptrace.DNSDoneInfo) { end("dns") },
		ConnectStart:         func(network, address string) { begin("connect", network+"\x00"+address) },
		ConnectDone:          func(network, address string, _ error) { end(network + "\x00" + address) },
		TLSHandshakeStart:    func() { begin("tls", "tls") },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { end("tls") },
		GotConn:              a.gotConnection,
		WroteRequest:         a.wroteRequest,
		GotFirstResponseByte: a.receivedFirstByte,
	}
}

func (a *httpAttempt) now() int64 {
	return time.Since(a.sampleStart).Milliseconds()
}

// Stage helpers are called with a.mu held. Endpoint keys only pair TCP trace
// callbacks, and are never persisted.
func (a *httpAttempt) begin(name, key string, at int64) {
	if len(a.request.Stages) == maxHTTPRequestStages {
		return
	}
	a.end(key, at)
	a.active[key] = len(a.request.Stages)
	a.request.Stages = append(a.request.Stages, CaptureStage{Name: name, StartMS: at, EndMS: -1})
	a.lastStage = name
}

func (a *httpAttempt) end(key string, at int64) bool {
	index, ok := a.active[key]
	if ok {
		a.request.Stages[index].EndMS = at
		delete(a.active, key)
	}
	return ok
}

func (a *httpAttempt) gotConnection(httptrace.GotConnInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		now := a.now()
		a.end("response_first_byte", now)
		a.begin("request_send", "request_send", now)
	}
}

func (a *httpAttempt) wroteRequest(info httptrace.WroteRequestInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.finished {
		now := a.now()
		a.end("request_send", now)
		if info.Err == nil && a.request.TTFBMS == nil {
			a.begin("response_first_byte", "response_first_byte", now)
		}
	}
}

func (a *httpAttempt) receivedFirstByte() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.finished || a.request.TTFBMS != nil {
		return
	}
	now := a.now()
	ttfb := now - a.request.StartMS
	a.request.TTFBMS = &ttfb
	if !a.end("response_first_byte", now) {
		// A server may respond before the write goroutine reports completion.
		a.begin("response_first_byte", "response_first_byte", now)
		a.end("response_first_byte", now)
	}
	a.begin("response_headers", "response_headers", now)
}

func (a *httpAttempt) finish(resp *http.Response, err error) HTTPRequestTiming {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finished = true
	now := a.now()
	a.request.EndMS, a.request.TotalMS = now, now-a.request.StartMS
	if resp != nil {
		a.request.HTTPStatus = resp.StatusCode
	}
	if err != nil {
		stage := a.lastStage
		if _, ok := a.active["response_headers"]; ok {
			stage = "response_headers"
		} else if _, ok := a.active["response_first_byte"]; ok {
			stage = "response_first_byte"
		} else if _, ok := a.active["request_send"]; ok {
			stage = "request_send"
		}
		a.request.FailureStage, a.request.FailureCode = httpFailure(stage, err)
	}
	for index := range a.request.Stages {
		if a.request.Stages[index].EndMS < 0 {
			a.request.Stages[index].EndMS = now
		}
	}
	return a.request
}

func httpFailure(stage string, err error) (string, string) {
	code := modelNetworkError(err, 0).Code
	switch code {
	case "dns_error":
		stage = "dns"
	case "tls_error":
		stage = "tls"
	case "connection_refused":
		stage = "connect"
	}
	if stage == "" {
		stage = "request_send"
	}
	return stage, code
}

func recordHTTPBody(observation *Observation, stage CaptureStage) {
	if len(observation.Requests) == 0 {
		return
	}
	last := &observation.Requests[len(observation.Requests)-1]
	last.BodyEndMS = &stage.EndMS
	last.Stages = append(last.Stages, stage)
	if observation.FailureStage == "receive" {
		last.FailureStage, last.FailureCode = observation.FailureStage, observation.FailureCode
	}
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
	case "dns", "connect", "tls", "request_send", "response_first_byte", "response_headers",
		"listen", "receive", "handshake", "app_connect", "create_stream", "play":
		return true
	default:
		return false
	}
}

func validCaptureFailure(code string) bool {
	switch code {
	case "", "timeout", "canceled", "dns_error", "tls_error", "connection_refused", "network_error",
		"http_status", "redirect_error", "body_error", "udp_bind_error", "udp_read_error", "rtmp_error":
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
	if len(observation.Stages) > 128 || !validHTTPRequests(observation.Requests, 0, observation.ElapsedMS) ||
		!validCaptureFailure(observation.FailureCode) ||
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

func validHTTPRequests(requests []HTTPRequestTiming, startMS, endMS int64) bool {
	if len(requests) > maxHTTPRequests {
		return false
	}
	for i, request := range requests {
		validFailureStage := request.FailureStage == "" || validHTTPRequestStage(request.FailureStage) ||
			(request.FailureStage == "receive" && request.BodyEndMS != nil)
		if request.Number != i+1 || request.Kind != "" || request.StartMS < startMS ||
			request.EndMS < request.StartMS || request.EndMS > endMS || request.TotalMS != request.EndMS-request.StartMS ||
			(request.HTTPStatus != 0 && (request.HTTPStatus < 100 || request.HTTPStatus > 599)) ||
			(request.TTFBMS != nil && (*request.TTFBMS < 0 || *request.TTFBMS > request.TotalMS)) ||
			!validCaptureFailure(request.FailureCode) || !validFailureStage {
			return false
		}
		stages := request.Stages
		if request.BodyEndMS != nil {
			bodyEnd := *request.BodyEndMS
			if i != len(requests)-1 || len(stages) == 0 || bodyEnd < request.EndMS || bodyEnd > endMS {
				return false
			}
			receive := stages[len(stages)-1]
			if receive.Name != "receive" || receive.StartMS < request.EndMS || receive.EndMS < receive.StartMS || receive.EndMS != bodyEnd {
				return false
			}
			stages = stages[:len(stages)-1]
		}
		if len(stages) > maxHTTPRequestStages {
			return false
		}
		lastStageStart := request.StartMS
		for _, stage := range stages {
			if !validHTTPRequestStage(stage.Name) || stage.StartMS < lastStageStart || stage.EndMS < stage.StartMS || stage.EndMS > request.EndMS {
				return false
			}
			lastStageStart = stage.StartMS
		}
		startMS = request.EndMS
	}
	return true
}

func validHTTPRequestStage(name string) bool {
	switch name {
	case "dns", "connect", "tls", "request_send", "response_first_byte", "response_headers":
		return true
	default:
		return false
	}
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
	case "redirect_error":
		return "重定向处理失败或超过跳转上限，请检查跳转目标和鉴权"
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
