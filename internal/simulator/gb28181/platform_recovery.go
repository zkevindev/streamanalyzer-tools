package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

const (
	mediaIdleTimeout = 3500 * time.Millisecond
	recoveryMedia    = "media_rtp_timeout"
	recoveryTCP      = "sip_tcp_disconnect"
	recoveryUDP      = "udp_keepalive_timeout"
)

type PlatformRecovery struct {
	RegistrationChanges  uint64 `json:"registration_changes,omitempty"`
	registrationBaseline uint64
	LastRTPAt            time.Time        `json:"last_rtp_at,omitzero"`
	KeepaliveAt          time.Time        `json:"keepalive_at,omitzero"`
	Mode                 string           `json:"mode"`
	State                string           `json:"state"`
	Before               *PlatformPreview `json:"before"`
	PreviousSession      *SessionReport   `json:"previous_session,omitempty"`
	InterruptedAt        time.Time        `json:"interrupted_at,omitzero"`
	RegisteredAt         time.Time        `json:"registered_at,omitzero"`
	CatalogAt            time.Time        `json:"catalog_at,omitzero"`
	FirstKeyframeAt      time.Time        `json:"first_keyframe_at,omitzero"`
	RecoveryMS           *int64           `json:"recovery_ms,omitempty"`
	IgnoredKeepalives    int              `json:"ignored_keepalives"`
	Error                string           `json:"error,omitempty"`
}

type recoveryMessage struct {
	XMLName  xml.Name
	Command  string `xml:"CmdType"`
	SN       uint32
	DeviceID string
	Items    []struct {
		ID     string `xml:"DeviceID"`
		Status string `xml:"Status"`
	} `xml:"DeviceList>Item"`
}

type recoveryCatalog struct {
	sn      uint32
	channel string
	done    chan struct{}
}

func (p *platform) runRecovery(cfg PreviewConfig, first *PlatformPreview, media previewMedia, peer netip.AddrPort, result *PlatformRecovery) {
	if cfg.Recovery == recoveryMedia {
		cfg.watchMedia = true
		p.runPreview(cfg, first, media, peer, nil)
		if first.State != "media_timeout" {
			p.finishRecovery(result, errors.New("未在已收到关键帧的会话中观测到 RTP 超时"))
			return
		}
		p.mu.Lock()
		result.InterruptedAt, result.State = result.LastRTPAt, "interrupted"
		p.mu.Unlock()
		cfg.watchMedia = false
		ctx, cancel := context.WithTimeout(p.ctx, 8*time.Second)
		defer cancel()
		p.recoverPreview(ctx, cfg, peer, result)
		return
	}
	started := false
	p.runPreview(cfg, first, media, peer, func() bool {
		p.mu.Lock()
		if p.peer != peer || !time.Now().Before(p.expires) {
			p.mu.Unlock()
			return false
		}
		started = true
		result.InterruptedAt = time.Now()
		result.State = "interrupted"
		p.mu.Unlock()
		p.mark(first, "fault_injected", cfg.Recovery)
		if cfg.Recovery == recoveryTCP && !p.interrupt(peer) {
			started = false
			return false
		}
		p.finish(first, "interrupted", "")
		return true
	})
	media.close()
	if !started {
		p.finishRecovery(result, errors.New("未能在收到关键帧后触发 SIP 中断"))
		return
	}

	// A UDP device checks heartbeats every five seconds in the built-in
	// scenario and can spend four seconds waiting for each unanswered request.
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	peer, err := p.awaitReregistration(ctx, result)
	if err != nil {
		p.finishRecovery(result, err)
		return
	}
	p.recoverPreview(ctx, cfg, peer, result)
}

func (p *platform) recoverPreview(ctx context.Context, cfg PreviewConfig, peer netip.AddrPort, result *PlatformRecovery) {
	if err := p.recoveryCatalog(ctx, peer, cfg.ChannelID); err != nil {
		p.finishRecovery(result, err)
		return
	}
	nextMedia, err := openPreviewMedia(p.local.Addr(), cfg.MediaTransport)
	if err != nil {
		p.finishRecovery(result, err)
		return
	}
	defer nextMedia.close()
	next := newPlatformPreview(cfg, nextMedia)
	p.mu.Lock()
	p.preview, result.State, result.CatalogAt = next, "previewing", time.Now()
	p.mu.Unlock()
	p.runPreview(cfg, next, nextMedia, peer, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		result.FirstKeyframeAt = time.Now()
		elapsed := result.FirstKeyframeAt.Sub(result.InterruptedAt).Milliseconds()
		result.RecoveryMS = &elapsed
		return false
	})
	if next.State != "passed" {
		p.finishRecovery(result, fmt.Errorf("恢复点播失败（%s）", next.State))
		return
	}
	p.finishRecovery(result, nil)
}

func (p *platform) finishRecovery(result *PlatformRecovery, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if result.Mode == recoveryMedia {
		result.RegistrationChanges = p.registrations - result.registrationBaseline
	}
	if err != nil {
		result.State, result.Error = "failed", err.Error()
		if p.ctx.Err() != nil {
			result.State, result.Error = "stopped", ""
		}
		return
	}
	result.State = "passed"
}

func (p *platform) awaitReregistration(ctx context.Context, result *PlatformRecovery) (netip.AddrPort, error) {
	check := time.NewTicker(50 * time.Millisecond)
	defer check.Stop()
	for {
		p.mu.Lock()
		peer, ready := p.peer, !result.RegisteredAt.IsZero() && time.Now().Before(p.expires)
		p.mu.Unlock()
		if ready {
			return peer, nil
		}
		select {
		case <-ctx.Done():
			return netip.AddrPort{}, errors.New("观测窗口内设备未重新注册")
		case <-check.C:
		}
	}
}

func (p *platform) recoveryCatalog(parent context.Context, peer netip.AddrPort, channel string) error {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	query := &recoveryCatalog{sn: uint32(time.Now().UnixMilli() % 99999999), channel: channel, done: make(chan struct{}, 1)}
	p.mu.Lock()
	p.catalog = query
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.catalog = nil
		p.mu.Unlock()
	}()
	remote := sip.Uri{Scheme: "sip", User: p.cfg.DeviceID, Host: peer.Addr().String(), Port: int(peer.Port())}
	req := sip.NewRequest(sip.MESSAGE, remote)
	req.SetTransport(strings.ToUpper(p.cfg.SIPTransport))
	req.SetDestination(peer.String())
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: p.cfg.PlatformID, Host: p.cfg.PlatformID[:10]}}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(sip.NewHeader("Content-Type", manscdp))
	req.SetBody(fmt.Appendf(nil, "%s<Query><CmdType>Catalog</CmdType><SN>%d</SN><DeviceID>%s</DeviceID></Query>", xml.Header, query.sn, p.cfg.DeviceID))
	answer, err := p.client.Do(ctx, req)
	if err != nil || answer == nil || !answer.IsSuccess() {
		return errors.New("恢复前的 Catalog 请求未获成功应答")
	}
	select {
	case <-ctx.Done():
		return errors.New("恢复前未收到匹配的在线通道目录")
	case <-query.done:
		return nil
	}
}

// recoveryPeerMessage runs under the platform lock. An unanswered transaction
// can retransmit several times, so count each heartbeat SN once.
func (p *platform) recoveryPeerMessage(req *sip.Request) (ignore bool, catalog *recoveryCatalog, heartbeat *PlatformRecovery) {
	if req.Method != sip.MESSAGE || p.recovery == nil || !p.busy {
		return false, nil, nil
	}
	var msg recoveryMessage
	if xml.Unmarshal(req.Body(), &msg) != nil || msg.DeviceID != p.cfg.DeviceID {
		return false, nil, nil
	}
	r := p.recovery
	if r.Mode == recoveryMedia && msg.XMLName.Local == "Notify" && msg.Command == "Keepalive" {
		return false, nil, r
	}
	if r.Mode == recoveryUDP && r.State == "interrupted" && r.RegisteredAt.IsZero() &&
		msg.XMLName.Local == "Notify" && msg.Command == "Keepalive" {
		if _, already := p.ignoredSN[msg.SN]; !already {
			p.ignoredSN[msg.SN] = struct{}{}
			r.IgnoredKeepalives++
		}
		return true, nil, nil
	}
	if query := p.catalog; query != nil && msg.XMLName.Local == "Response" && msg.Command == "Catalog" && msg.SN == query.sn {
		for _, item := range msg.Items {
			if item.ID == query.channel && strings.EqualFold(item.Status, "ON") {
				return false, query, nil
			}
		}
	}
	return false, nil, nil
}

func (r *PlatformRecovery) snapshot() *PlatformRecovery {
	result := *r
	result.Before = clonePlatformPreview(r.Before)
	return &result
}

func clonePlatformPreview(p *PlatformPreview) *PlatformPreview {
	result := *p
	result.Timeline = slices.Clone(result.Timeline)
	if p.Recording != nil {
		recording := *p.Recording
		recording.Records = slices.Clone(recording.Records)
		result.Recording = &recording
	}
	return &result
}

func earlierTime(first, second time.Time) time.Time {
	if first.Before(second) {
		return first
	}
	return second
}
