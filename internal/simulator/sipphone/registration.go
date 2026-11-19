package sipphone

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const requestedExpiry = 3600

type registrationFlow struct {
	ctx        context.Context
	registered chan struct{}
}

func (a *agent) serve() error {
	lookup, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	addresses, err := net.DefaultResolver.LookupNetIP(lookup, "ip4", a.cfg.Host)
	cancel()
	if err != nil {
		return fmt.Errorf("解析 SIP 服务器失败：%w", err)
	}
	for _, ip := range addresses {
		if isUnicast(ip.Unmap()) {
			a.registrar = netip.AddrPortFrom(ip.Unmap(), uint16(a.cfg.Port))
			break
		}
	}
	if !a.registrar.IsValid() {
		return errors.New("SIP 服务器需要单播 IPv4 地址")
	}
	var connected bool
	delay := 250 * time.Millisecond
	for {
		var established bool
		established, err = a.serveRegistration()
		if a.ctx.Err() != nil || a.cfg.Transport == "udp" || (!connected && !established) {
			return err
		}
		if established {
			delay = 250 * time.Millisecond
		}
		connected = true
		a.state("reconnecting", err)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			a.mu.Lock()
			a.status.Reconnect.Attempts++
			a.mu.Unlock()
		case <-a.ctx.Done():
			timer.Stop()
			return nil
		}
		delay = min(delay*2, 3*time.Second)
	}
}

func (a *agent) serveRegistration() (established bool, err error) {
	flowCtx, stopFlow := context.WithCancel(a.ctx)
	defer stopFlow()
	flow := &registrationFlow{ctx: flowCtx, registered: make(chan struct{})}
	a.flow = flow
	local, _ := netip.ParseAddr(a.cfg.LocalIP)
	wire, err := a.openSignal(local)
	if err != nil {
		return false, err
	}
	defer wire.close()
	options := []sipgo.UserAgentOption{sipgo.WithUserAgent("Stream Analyzer SIP"), sipgo.WithUserAgentHostname(a.cfg.Domain)}
	if a.cfg.Transport != "udp" {
		options = append(options, a.streamOptions()...)
	}
	ua, err := sipgo.NewUA(options...)
	if err != nil {
		return false, err
	}
	defer ua.Close()
	server, err := sipgo.NewServer(ua)
	if err != nil {
		return false, err
	}
	a.client, err = sipgo.NewClient(ua, sipgo.WithClientAddr(a.local.String()), sipgo.WithClientConnectionAddr(a.local.String()), sipgo.WithClientNAT())
	if err != nil {
		return false, err
	}
	contact := sip.Uri{Scheme: "sip", User: a.cfg.User, Host: a.local.Addr().String(), Port: int(a.local.Port())}
	if a.cfg.Transport != "udp" {
		contact.UriParams.Add("transport", a.cfg.Transport)
	}
	a.dialogs = sipgo.DialogUA{Client: a.client, RewriteContact: a.cfg.Transport != "udp", ContactHDR: sip.ContactHeader{Address: contact}}
	a.registerID = sip.CallIDHeader(sip.GenerateTagN(24))
	a.registerTag = sip.GenerateTagN(16)
	a.registerSeq = 0
	a.routes(server, flow)

	served := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = wire.serve(server)
		close(served)
	}()
	defer func() {
		// Handlers and Dial add workers under the same lock and reject after
		// cancellation, so none can begin work on this flow after the barrier.
		a.mu.Lock()
		if established && a.ctx.Err() == nil && a.cfg.Transport != "udp" {
			a.markDisconnectedLocked()
		}
		stopFlow()
		a.mu.Unlock()
		_ = wire.close()
		ua.Close()
		<-served
		a.workers.Wait()
	}()
	select {
	case <-wire.ready:
	case <-a.ctx.Done():
		return false, a.ctx.Err()
	case <-served:
		return false, fmt.Errorf("SIP 信令流初始化失败：%w", serveErr)
	}

	a.mu.Lock()
	a.status.LocalAddress, a.status.Registrar = a.local.String(), a.registrar.String()
	a.mu.Unlock()
	a.signalPhase("register", "")
	expiry, err := a.register(flow.ctx, requestedExpiry)
	if err != nil {
		return false, err
	}
	a.signalPhase("registered", "")
	established = true
	a.registrationSucceeded()
	close(flow.registered)
	delay := time.Duration(expiry) * time.Second * 4 / 5
	for {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			a.signalPhase("register", "")
			refreshed, err := a.register(flow.ctx, expiry)
			if err != nil {
				a.state("retrying", err)
				delay = 5 * time.Second
			} else {
				a.signalPhase("registered", "")
				a.state("registered", nil)
				expiry = refreshed
				delay = time.Duration(expiry) * time.Second * 4 / 5
			}
		case <-a.ctx.Done():
			timer.Stop()
			a.unregister()
			return true, nil
		case <-served:
			timer.Stop()
			a.signalPhase("connection", "closed")
			return true, fmt.Errorf("SIP 信令流已结束：%w", serveErr)
		}
	}
}

func (a *agent) markDisconnectedLocked() {
	a.status.State = "reconnecting"
	if a.status.Reconnect == nil {
		a.status.Reconnect = &ReconnectStatus{}
	}
	stats := a.status.Reconnect
	stats.DisconnectedAt, stats.RegisteredAt = time.Now(), time.Time{}
	stats.OldCallID, stats.OldMediaReleased = "", time.Time{}
	if a.call != nil && a.call.status.Active {
		stats.OldCallID = a.call.status.ID
	}
}

func (a *agent) registrationSucceeded() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil {
		return
	}
	a.status.State, a.status.Error = "registered", ""
	if stats := a.status.Reconnect; stats != nil {
		stats.Recovered++
		stats.RegisteredAt = time.Now()
	}
}

func (a *agent) unregister() {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	if _, err := a.register(ctx, 0); err != nil {
		a.event("system", "未收到 SIP 注销确认")
	}
	a.mu.Lock()
	c := a.call
	a.mu.Unlock()
	if c != nil {
		select {
		case <-c.done:
		case <-ctx.Done():
		}
	}
}

func (a *agent) register(ctx context.Context, expiry int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for range 2 {
		req := sip.NewRequest(sip.REGISTER, sip.Uri{Scheme: "sip", Host: a.cfg.Domain})
		req.SetTransport(strings.ToUpper(a.cfg.Transport))
		req.SetDestination(a.registrar.String())
		identity := sip.Uri{Scheme: "sip", User: a.cfg.User, Host: a.cfg.Domain}
		from := &sip.FromHeader{Address: identity}
		from.Params.Add("tag", a.registerTag)
		a.registerSeq++
		req.AppendHeader(from)
		req.AppendHeader(&sip.ToHeader{Address: identity})
		req.AppendHeader(sip.HeaderClone(&a.registerID))
		req.AppendHeader(&sip.CSeqHeader{SeqNo: a.registerSeq, MethodName: sip.REGISTER})
		req.AppendHeader(sip.HeaderClone(&a.dialogs.ContactHDR))
		req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expiry)))
		res, err := a.client.Do(ctx, req)
		stage := "register"
		if err == nil && res != nil && (res.StatusCode == 401 || res.StatusCode == 407) {
			stage = "digest"
			if expiry > 0 {
				a.signalPhase("digest", "")
			}
			res, err = a.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{Username: a.cfg.Username, Password: a.cfg.Password})
		}
		a.registerSeq = req.CSeq().SeqNo
		if err != nil {
			if expiry > 0 {
				a.signalPhase(stage, "transaction")
			}
			return 0, fmt.Errorf("SIP REGISTER 事务失败：%w", err)
		}
		if res == nil {
			return 0, errors.New("SIP REGISTER 未收到最终响应")
		}
		if res.StatusCode == 423 && expiry > 0 {
			if value, ok := sipIntHeader(res, "Min-Expires"); ok && value > expiry && value <= 86400 {
				expiry = value
				continue
			}
		}
		if res.StatusCode != 200 {
			if expiry > 0 {
				a.signalPhase(stage, "sip_"+strconv.Itoa(res.StatusCode))
			}
			return 0, fmt.Errorf("SIP 注册请求被拒绝（%d）", res.StatusCode)
		}
		if expiry == 0 {
			return 0, nil
		}
		if value, ok := sipIntHeader(res, "Expires"); ok {
			expiry = value
		}
		for _, header := range res.GetHeaders("Contact") {
			contact, ok := header.(*sip.ContactHeader)
			local := a.dialogs.ContactHDR.Address
			if !ok || contact.Address.User != local.User || contact.Address.Host != local.Host || contact.Address.Port != local.Port {
				continue
			}
			if value, ok := contact.Params.Get("expires"); ok {
				if ttl, err := strconv.Atoi(value); err == nil && ttl >= 0 {
					expiry = ttl
				}
			}
			break
		}
		if expiry <= 0 {
			return 0, errors.New("SIP 服务器未保留 Contact 绑定")
		}
		return min(expiry, 86400), nil
	}
	return 0, errors.New("SIP 服务器要求更长的注册有效期")
}

func sipIntHeader(res *sip.Response, name string) (int, bool) {
	if h := res.GetHeader(name); h != nil {
		n, err := strconv.Atoi(strings.TrimSpace(h.Value()))
		return n, err == nil && n >= 0
	}
	return 0, false
}

type signalSocket struct {
	*net.UDPConn
	agent *agent
	ready chan struct{}
	once  sync.Once
}

func (s *signalSocket) ReadFrom(buf []byte) (int, net.Addr, error) {
	s.once.Do(func() { close(s.ready) })
	n, addr, err := s.UDPConn.ReadFrom(buf)
	if err == nil {
		s.agent.observeSignal("in", buf[:n], addr.String())
	}
	return n, addr, err
}

func (s *signalSocket) WriteTo(data []byte, addr net.Addr) (int, error) {
	n, err := s.UDPConn.WriteTo(data, addr)
	if err == nil {
		s.agent.observeSignal("out", data, addr.String())
	}
	return n, err
}

func (a *agent) observeSignal(direction string, packet []byte, addr string) {
	// Only retain SIP method and status. Credentials, headers and SDP stay off the workbench.
	line, rest, _ := strings.Cut(string(packet), "\r\n")
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}
	message := fields[0]
	if message == "SIP/2.0" {
		code, err := strconv.Atoi(fields[1])
		if err != nil || code < 100 || code > 699 {
			return
		}
		message = strconv.Itoa(code)
		for hdr := range strings.SplitSeq(rest, "\r\n") {
			if hdr == "" {
				break
			}
			name, value, _ := strings.Cut(hdr, ":")
			if strings.EqualFold(name, "CSeq") {
				parts := strings.Fields(value)
				if len(parts) == 2 && sipMethod(parts[1]) {
					message += " " + parts[1]
				}
				break
			}
		}
	} else if !sipMethod(message) || len(fields) != 3 || fields[2] != "SIP/2.0" {
		return
	}
	a.event(direction, message+" · "+addr)
}

func sipMethod(method string) bool {
	return len(method) > 0 && len(method) <= 16 && strings.Trim(method, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
}
