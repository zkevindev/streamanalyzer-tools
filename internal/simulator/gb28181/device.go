package gb28181

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type device struct {
	cfg      Config
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	status   Status
	platform netip.AddrPort
	callID   string
	fromTag  string
	seq      uint32 // REGISTER is serialized in run.
	xmlSN    atomic.Uint32
	media    *mediaSession
	file     *localMedia
	workers  sync.WaitGroup // New workers are admitted under mu while the device is registered.
}

const defaultExpires = 3600

func (d *device) run() {
	defer close(d.done)
	defer d.cancel()
	if d.file != nil {
		defer d.file.remove()
	}
	if err := d.serve(); err != nil && d.ctx.Err() == nil {
		d.log("system", err.Error(), "")
		d.state("failed", err.Error())
	}
	if d.ctx.Err() != nil {
		d.state("stopped", "")
	}
}

func (d *device) serve() error {
	ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", d.cfg.PlatformHost)
	cancel()
	if err != nil {
		return fmt.Errorf("解析平台 IPv4 地址失败：%w", err)
	}
	for _, ip := range ips {
		if unicastIP(ip) {
			d.platform = netip.AddrPortFrom(netip.AddrFrom4([4]byte(ip.To4())), uint16(d.cfg.PlatformPort))
			break
		}
	}
	if !d.platform.IsValid() {
		return fmt.Errorf("平台地址必须解析为单播 IPv4 地址")
	}
	d.callID, d.fromTag = sip.GenerateTagN(24), sip.GenerateTagN(16)

	wasRegistered := false
	retryDelay := time.Second
	for d.ctx.Err() == nil {
		registered, err := d.serveSIPFlow()
		if d.ctx.Err() != nil {
			return nil
		}
		if !wasRegistered && !registered {
			return err
		}
		if d.cfg.SIPTransport == "udp" {
			return err
		}
		if registered {
			retryDelay = time.Second
		}
		wasRegistered = true
		d.mu.Lock()
		if d.ctx.Err() == nil {
			d.status.State, d.status.Error, d.status.LocalAddress = "retrying", err.Error(), ""
		}
		d.mu.Unlock()
		d.log("system", err.Error()+"，正在重连", "")
		select {
		case <-d.ctx.Done():
			return nil
		case <-time.After(retryDelay):
		}
		retryDelay = min(2*retryDelay, 5*time.Second)
	}
	return nil
}

func (d *device) request(method sip.RequestMethod, body []byte) *sip.Request {
	platform := sip.Uri{Scheme: "sip", User: d.cfg.PlatformID, Host: d.cfg.Domain}
	target := platform
	target.Host, target.Port = d.cfg.PlatformHost, d.cfg.PlatformPort
	req := sip.NewRequest(method, target)
	req.SetTransport(strings.ToUpper(d.cfg.SIPTransport))
	req.SetDestination(d.platform.String())
	req.SetBody(body)
	device := sip.Uri{Scheme: "sip", User: d.cfg.DeviceID, Host: d.cfg.Domain}
	from := &sip.FromHeader{Address: device}
	from.Params.Add("tag", d.fromTag)
	req.AppendHeader(from)
	if method == sip.REGISTER {
		platform = device
	}
	req.AppendHeader(&sip.ToHeader{Address: platform})
	req.AppendHeader(sip.NewHeader("User-Agent", "Stream Analyzer GB28181"))
	return req
}

func (d *device) contact(flow *sipFlow) *sip.ContactHeader {
	address := sip.Uri{Scheme: "sip", User: d.cfg.DeviceID,
		Host: flow.local.Addr().String(), Port: int(flow.local.Port())}
	if d.cfg.SIPTransport == "tcp" {
		address.UriParams.Add("transport", "tcp")
	}
	return &sip.ContactHeader{Address: address}
}

func (d *device) register(parent context.Context, flow *sipFlow, expires int) (int, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	action := "注册"
	if expires == 0 {
		action = "注销"
	}
	req := d.request(sip.REGISTER, nil)
	callID := sip.CallIDHeader(d.callID)
	d.seq++
	cseq := &sip.CSeqHeader{SeqNo: d.seq, MethodName: sip.REGISTER}
	req.AppendHeader(&callID)
	req.AppendHeader(cseq)
	req.AppendHeader(d.contact(flow))
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))

	response, err := flow.client.Do(ctx, req)
	if err == nil && response != nil && (response.StatusCode == 401 || response.StatusCode == 407) {
		response, err = flow.client.DoDigestAuth(ctx, req, response,
			sipgo.DigestAuth{Username: d.cfg.Username, Password: d.cfg.Password})
		d.seq = cseq.SeqNo
	}
	if err != nil {
		return 0, fmt.Errorf("SIP %s失败：%w", action, err)
	}
	if response == nil {
		return 0, fmt.Errorf("SIP %s事务结束前未收到响应", action)
	}
	if response.StatusCode != sip.StatusOK {
		return 0, fmt.Errorf("平台拒绝%s（SIP %d）", action, response.StatusCode)
	}
	if expires > 0 {
		// The registrar may shorten the requested lifetime.
		var lifetime string
		if header := response.GetHeader("Expires"); header != nil {
			lifetime = header.Value()
		}
		if contact := response.Contact(); contact != nil {
			if value, ok := contact.Params.Get("expires"); ok {
				lifetime = value
			}
		}
		if value, err := strconv.ParseInt(lifetime, 10, 32); err == nil && value > 0 {
			expires = int(value)
		}
	}
	return expires, nil
}

func renewalDelay(expires int) time.Duration {
	return time.Duration(expires) * time.Second * 4 / 5
}

func (d *device) keepRegistered(flow *sipFlow, expires int) error {
	heartbeat := time.NewTicker(time.Duration(d.cfg.Heartbeat) * time.Second)
	defer heartbeat.Stop()
	refresh := time.NewTimer(renewalDelay(expires))
	defer refresh.Stop()
	failures := 0
	for {
		select {
		case <-flow.ctx.Done():
			return flow.ctx.Err()
		case <-heartbeat.C:
			if err := d.sendXML(flow, d.keepalive()); err == nil {
				failures = 0
				continue
			} else {
				d.log("system", "心跳失败："+err.Error(), "")
			}
			failures++
			if failures < 3 {
				continue
			}
			if d.cfg.SIPTransport == "tcp" {
				return fmt.Errorf("连续三次 SIP TCP 心跳失败")
			}
			d.state("retrying", "连续三次心跳失败，正在重新注册")
			d.closeMedia()
		case <-refresh.C:
		}
		for flow.ctx.Err() == nil {
			lifetime, err := d.register(flow.ctx, flow, defaultExpires)
			if err == nil {
				d.state("registered", "")
				refresh.Reset(renewalDelay(lifetime))
				heartbeat.Reset(time.Duration(d.cfg.Heartbeat) * time.Second)
				failures = 0
				break
			}
			d.state("retrying", err.Error())
			d.closeMedia()
			d.log("system", err.Error(), "")
			select {
			case <-flow.ctx.Done():
				return flow.ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}
}
