package onvifdiscovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/icholy/digest"
)

const securityNS = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
const utilityNS = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"

func xmlText(value string) string {
	var buffer bytes.Buffer
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

func tokenHeader(credentials Credentials, now time.Time) (string, error) {
	if credentials.Username == "" {
		return "", nil
	}
	var nonce [20]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", &Error{Code: "internal_error", Message: "无法生成鉴权随机数"}
	}
	created := now.UTC().Format(time.RFC3339Nano)
	hash := sha1.New()
	hash.Write(nonce[:])
	hash.Write([]byte(created))
	hash.Write([]byte(credentials.Password))
	return fmt.Sprintf(`<wsse:Security s:mustUnderstand="true" xmlns:wsse="%s" xmlns:wsu="%s"><wsse:UsernameToken><wsse:Username>%s</wsse:Username><wsse:Password Type="%s#PasswordDigest">%s</wsse:Password><wsse:Nonce EncodingType="%s#Base64Binary">%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security>`, securityNS, utilityNS, xmlText(credentials.Username), "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0", base64.StdEncoding.EncodeToString(hash.Sum(nil)), "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0", base64.StdEncoding.EncodeToString(nonce[:]), created), nil
}

type soapReply struct {
	XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Body    struct {
		Values []struct {
			XMLName xml.Name
			Inner   string `xml:",innerxml"`
		} `xml:",any"`
	} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}

func soapRequest(ctx context.Context, destination target, namespace, operation, body string, credentials Credentials, offset time.Duration) (*http.Request, error) {
	header, err := tokenHeader(credentials, time.Now().Add(offset))
	if err != nil {
		return nil, err
	}
	message := fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:a="http://www.w3.org/2005/08/addressing" xmlns:o="%s" xmlns:tt="%s"><s:Header><a:Action s:mustUnderstand="true">%s/%s</a:Action><a:MessageID>urn:uuid:%s</a:MessageID><a:To s:mustUnderstand="true">%s</a:To>%s</s:Header><s:Body><o:%s>%s</o:%s></s:Body></s:Envelope>`, soapNS, namespace, schemaNS, namespace, operation, uuid.NewString(), xmlText(destination.url.String()), header, operation, body, operation)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, destination.url.String(), strings.NewReader(message))
	if err != nil {
		return nil, invalid("ONVIF 服务地址无效")
	}
	request.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+namespace+"/"+operation+`"`)
	request.Header.Set("User-Agent", "StreamAnalyzer-ONVIF/1.0")
	return request, nil
}

func call(ctx context.Context, destination target, namespace, operation, body string, credentials Credentials, offset time.Duration, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	request, err := soapRequest(ctx, destination, namespace, operation, body, credentials, offset)
	if err != nil {
		return err
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, ResponseHeaderTimeout: 3 * time.Second, TLSHandshakeTimeout: 3 * time.Second, MaxResponseHeaderBytes: 32 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 3 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(destination.bind.IP)}}).DialContext(ctx, "tcp4", net.JoinHostPort(destination.peer.String(), port))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return networkError(operation, err)
	}
	// A camera may require HTTP Digest in addition to WS-Security. Retry only
	// this exact pinned endpoint once; never cache a challenge or use Basic.
	if response.StatusCode == http.StatusUnauthorized && credentials.Username != "" {
		challenge, challengeErr := digest.FindChallenge(response.Header)
		if challengeErr == nil && len(challenge.Nonce) > 0 && len(challenge.Nonce) <= 1024 && len(challenge.Realm) <= 1024 {
			response.Body.Close()
			retry, requestErr := soapRequest(ctx, destination, namespace, operation, body, credentials, offset)
			if requestErr != nil {
				return requestErr
			}
			authorization, digestErr := digest.Digest(challenge, digest.Options{Method: retry.Method, URI: retry.URL.RequestURI(), GetBody: retry.GetBody, Count: 1, Username: credentials.Username, Password: credentials.Password})
			if digestErr != nil {
				return &Error{Code: "auth_failed", Stage: operation, Message: "设备的 HTTP Digest 鉴权方式不受支持"}
			}
			retry.Header.Set("Authorization", authorization.String())
			response, err = client.Do(retry)
			if err != nil {
				return networkError(operation, err)
			}
		}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return networkError(operation, err)
	}
	if len(data) > 1<<20 {
		return &Error{Code: "response_limit", Stage: operation, Message: "设备 SOAP 响应超过 1 MiB 上限"}
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return &Error{Code: "redirect_stopped", Stage: operation, Message: "设备返回重定向，已停止自动跟随；请核对后手动填写目标地址", HTTPStatus: response.StatusCode}
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return &Error{Code: "auth_failed", Stage: operation, Message: "ONVIF 鉴权失败，请核对账号、密码与设备时间", HTTPStatus: response.StatusCode}
	}
	var envelope soapReply
	if err := decodeXML(data, &envelope); err != nil {
		return &Error{Code: "soap_parse_error", Stage: operation, Message: "设备 SOAP 响应格式无效", HTTPStatus: response.StatusCode}
	}
	if len(envelope.Body.Values) != 1 {
		return &Error{Code: "soap_parse_error", Stage: operation, Message: "设备 SOAP 响应须包含一条结果", HTTPStatus: response.StatusCode}
	}
	value := envelope.Body.Values[0]
	if value.XMLName.Local == "Fault" && value.XMLName.Space == soapNS {
		if response.StatusCode == 401 || response.StatusCode == 403 || strings.Contains(value.Inner, "NotAuthorized") {
			return &Error{Code: "auth_failed", Stage: operation, Message: "ONVIF 鉴权失败，请核对账号、密码与设备时间", HTTPStatus: response.StatusCode}
		}
		return &Error{Code: "soap_fault", Stage: operation, Message: "设备返回 ONVIF SOAP Fault，请核对服务能力与配置", HTTPStatus: response.StatusCode}
	}
	if response.StatusCode != http.StatusOK {
		return &Error{Code: "http_status", Stage: operation, Message: "ONVIF 服务返回异常 HTTP 状态", HTTPStatus: response.StatusCode}
	}
	if value.XMLName.Space != namespace || value.XMLName.Local != operation+"Response" {
		return &Error{Code: "soap_parse_error", Stage: operation, Message: "设备返回了不匹配的 ONVIF 操作响应"}
	}
	if output != nil {
		wrapped := []byte(`<result xmlns:tt="` + schemaNS + `" xmlns:trt="` + mediaNS + `" xmlns:tds="` + deviceNS + `">` + value.Inner + `</result>`)
		if decodeXML(wrapped, output) != nil {
			return &Error{Code: "soap_parse_error", Stage: operation, Message: "无法读取设备的 ONVIF 数据"}
		}
	}
	return nil
}

func networkError(stage string, err error) error {
	code, message := "connection_error", "无法连接所选 ONVIF 服务，请核对地址、网卡与设备状态"
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var timeout net.Error
	switch {
	case errors.Is(err, context.Canceled):
		code, message = "canceled", "ONVIF 查询已取消"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
		code, message = "timeout", "ONVIF 查询超时，请检查设备或网络"
	case errors.As(err, &certificate) || errors.As(err, &authority) || errors.As(err, &hostname):
		code, message = "tls_error", "ONVIF HTTPS 证书校验失败，请核对证书信任和主机名"
	}
	return &Error{Code: code, Stage: stage, Message: message}
}

type timeResponse struct {
	DateTime struct {
		UTC struct {
			Time struct{ Hour, Minute, Second int }
			Date struct{ Year, Month, Day int }
		} `xml:"UTCDateTime"`
	} `xml:"SystemDateAndTime"`
}

func deviceTime(ctx context.Context, destination target, credentials Credentials) (time.Duration, string, *int64) {
	start := time.Now()
	var result timeResponse
	err := call(ctx, destination, deviceNS, "GetSystemDateAndTime", "", Credentials{}, 0, &result)
	var authError *Error
	if errors.As(err, &authError) && authError.Code == "auth_failed" && credentials.Username != "" {
		err = call(ctx, destination, deviceNS, "GetSystemDateAndTime", "", credentials, 0, &result)
	}
	if err != nil {
		return 0, "unavailable", nil
	}
	value := result.DateTime.UTC
	if value.Date.Year < 2000 || value.Date.Year > 2200 || value.Date.Month < 1 || value.Date.Month > 12 || value.Date.Day < 1 || value.Date.Day > 31 || value.Time.Hour < 0 || value.Time.Hour > 23 || value.Time.Minute < 0 || value.Time.Minute > 59 || value.Time.Second < 0 || value.Time.Second > 59 {
		return 0, "unavailable", nil
	}
	clock := time.Date(value.Date.Year, time.Month(value.Date.Month), value.Date.Day, value.Time.Hour, value.Time.Minute, value.Time.Second, 0, time.UTC)
	if clock.Day() != value.Date.Day {
		return 0, "unavailable", nil
	}
	offset := clock.Sub(start.Add(time.Since(start) / 2))
	seconds := int64(offset / time.Second)
	if offset < -24*time.Hour || offset > 24*time.Hour {
		return 0, "out_of_range", &seconds
	}
	return offset, "device_utc", &seconds
}

func (m *Manager) Profiles(ctx context.Context, req ProfilesRequest) (ProfilesResult, error) {
	if err := validateCredentials(req.Credentials); err != nil {
		return ProfilesResult{}, err
	}
	ctx, done, err := m.operation(ctx)
	if err != nil {
		return ProfilesResult{}, err
	}
	defer done()
	destination, err := m.selectedTarget(ctx, req)
	if err != nil {
		return ProfilesResult{}, err
	}
	offset, clockStatus, clockOffset := deviceTime(ctx, destination, req.Credentials)
	var capabilities struct {
		Capabilities struct{ Media struct{ XAddr string } }
	}
	if err := call(ctx, destination, deviceNS, "GetCapabilities", "<o:Category>Media</o:Category>", req.Credentials, offset, &capabilities); err != nil {
		return ProfilesResult{}, err
	}
	media, err := mediaTarget(ctx, capabilities.Capabilities.Media.XAddr, destination)
	if err != nil {
		return ProfilesResult{}, err
	}
	var response struct {
		Profiles []struct {
			Token string `xml:"token,attr"`
			Name  string
			Video struct {
				Encoding    string
				Resolution  struct{ Width, Height int }
				RateControl struct{ FrameRateLimit int }
			} `xml:"VideoEncoderConfiguration"`
			Audio struct{ Encoding string } `xml:"AudioEncoderConfiguration"`
		}
	}
	if err := call(ctx, media, mediaNS, "GetProfiles", "", req.Credentials, offset, &response); err != nil {
		return ProfilesResult{}, err
	}
	result := ProfilesResult{SelectionID: identifier(), DeviceURL: destination.url.String(), MediaURL: media.url.String(), PeerIP: destination.peer.String(), Profiles: []Profile{}, ClockStatus: clockStatus, ClockOffsetSeconds: clockOffset}
	tokens := make(map[string]bool)
	for _, value := range response.Profiles {
		if len(result.Profiles) == maxProfiles {
			result.DroppedProfiles++
			continue
		}
		if value.Token == "" || len(value.Token) > 256 || tokens[value.Token] || strings.ContainsAny(value.Token, "\x00\r\n") {
			continue
		}
		tokens[value.Token] = true
		name := value.Name
		if len(name) > 256 {
			name = ""
		}
		profile := Profile{Token: value.Token, Name: name, VideoCodec: safeCodec(value.Video.Encoding), AudioCodec: safeCodec(value.Audio.Encoding)}
		if value.Video.Resolution.Width > 0 && value.Video.Resolution.Width <= 16384 {
			profile.Width = value.Video.Resolution.Width
		}
		if value.Video.Resolution.Height > 0 && value.Video.Resolution.Height <= 16384 {
			profile.Height = value.Video.Resolution.Height
		}
		if value.Video.RateControl.FrameRateLimit > 0 && value.Video.RateControl.FrameRateLimit <= 1000 {
			profile.FrameRate = value.Video.RateControl.FrameRateLimit
		}
		result.Profiles = append(result.Profiles, profile)
	}
	if len(tokens) == 0 {
		return ProfilesResult{}, &Error{Code: "no_profiles", Stage: "GetProfiles", Message: "设备未返回可选择的媒体 Profile"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ProfilesResult{}, &Error{Code: "closed", Message: "ONVIF 发现服务已关闭"}
	}
	if m.selections == nil {
		m.selections = make(map[string]selection)
	}
	m.pruneLocked()
	if len(m.selections) >= maxSelections {
		var oldest string
		var expiry time.Time
		for id, value := range m.selections {
			if oldest == "" || value.expires.Before(expiry) {
				oldest, expiry = id, value.expires
			}
		}
		delete(m.selections, oldest)
	}
	m.selections[result.SelectionID] = selection{device: destination, media: media, profiles: tokens, offset: offset, expires: time.Now().Add(stateTTL)}
	return result, nil
}

func safeCodec(value string) string {
	switch strings.ToUpper(value) {
	case "H264", "H265", "HEVC", "JPEG", "MPEG4", "AAC", "G711", "G726", "OPUS":
		return strings.ToUpper(value)
	case "":
		return ""
	}
	return "unknown"
}

func (m *Manager) Stream(ctx context.Context, req StreamRequest) (StreamResult, error) {
	if err := validateCredentials(req.Credentials); err != nil {
		return StreamResult{}, err
	}
	if req.Transport != "tcp" && req.Transport != "udp" {
		return StreamResult{}, invalid("请选择 TCP 或 UDP 传输")
	}
	ctx, done, err := m.operation(ctx)
	if err != nil {
		return StreamResult{}, err
	}
	defer done()
	m.mu.Lock()
	m.pruneLocked()
	value, found := m.selections[req.SelectionID]
	m.mu.Unlock()
	if !found || !value.profiles[req.ProfileToken] {
		return StreamResult{}, &Error{Code: "not_found", Message: "Profile 选择已过期，请重新读取设备 Profile"}
	}
	protocol := "RTSP"
	if req.Transport == "udp" {
		protocol = "UDP"
	}
	body := `<o:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>` + protocol + `</tt:Protocol></tt:Transport></o:StreamSetup><o:ProfileToken>` + xmlText(req.ProfileToken) + `</o:ProfileToken>`
	var response struct {
		MediaURI struct {
			URI string `xml:"Uri"`
		} `xml:"MediaUri"`
	}
	if err := call(ctx, value.media, mediaNS, "GetStreamUri", body, req.Credentials, value.offset, &response); err != nil {
		return StreamResult{}, err
	}
	raw := strings.TrimSpace(response.MediaURI.URI)
	if len(raw) > 4096 || strings.ContainsAny(raw, "\r\n\x00") {
		return StreamResult{}, &Error{Code: "stream_url_invalid", Message: "设备返回的播放地址无效"}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "rtsp" && u.Scheme != "rtsps") {
		return StreamResult{}, &Error{Code: "stream_url_invalid", Message: "设备未返回有效的 RTSP 或 RTSPS 播放地址"}
	}
	if req.Transport == "udp" && u.Scheme == "rtsps" {
		return StreamResult{}, invalid("RTSPS 使用 TLS/TCP，请改选 TCP 传输")
	}
	result := StreamResult{Protocol: u.Scheme, Transport: req.Transport, RemovedUserInfo: u.User != nil, DifferentHost: !strings.EqualFold(u.Hostname(), value.device.url.Hostname())}
	u.User = nil
	u.Fragment = ""
	result.URL = u.String()
	return result, nil
}
