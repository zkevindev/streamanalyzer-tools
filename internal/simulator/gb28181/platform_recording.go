package gb28181

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"golang.org/x/net/html/charset"
)

type PlaybackRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type PlatformRecording struct {
	PlaybackRange
	Total   *int            `json:"total,omitempty"`
	Records []PlaybackRange `json:"records,omitempty"`
}

type PlaybackControl struct {
	Method string   `json:"method"`
	Offset *float64 `json:"offset,omitempty"`
}

type platformPlayback struct {
	control     sync.Mutex
	controlCSeq uint32
	report      *PlatformPreview
	sn          uint32
	records     map[string]struct{}
	queryDone   chan struct{}
	ended       chan struct{}
	dialog      *sipgo.DialogClientSession
	wakeRead    func()
}

func (p *platform) queryRecording(cfg PreviewConfig, peer netip.AddrPort, playback *platformPlayback) bool {
	result := playback.report
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{Scheme: "sip", User: cfg.ChannelID, Host: peer.Addr().String(), Port: int(peer.Port())})
	req.SetTransport(strings.ToUpper(p.cfg.SIPTransport))
	req.SetDestination(peer.String())
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: p.cfg.PlatformID, Host: p.cfg.PlatformID[:10]}}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(sip.NewHeader("Content-Type", manscdp))
	req.SetBody(fmt.Appendf(nil, "%s<Query><CmdType>RecordInfo</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><StartTime>%s</StartTime><EndTime>%s</EndTime><Secrecy>0</Secrecy><Type>all</Type></Query>",
		xml.Header, playback.sn, cfg.ChannelID, cfg.Recording.Start.Local().Format(recordTimeLayout), cfg.Recording.End.Local().Format(recordTimeLayout)))
	p.mark(result, "record_info", "")
	ctx, cancel := context.WithTimeout(p.ctx, 4*time.Second)
	defer cancel()
	answer, err := p.client.Do(ctx, req)
	if err != nil || answer == nil || !answer.IsSuccess() {
		p.finish(result, "record_info_failed", "RecordInfo 请求未获成功应答")
		return false
	}
	select {
	case <-playback.queryDone:
	case <-ctx.Done():
		p.finish(result, "record_info_failed", "未在窗口内收到完整的录像目录")
		return false
	}
	p.mu.Lock()
	count := len(result.Recording.Records)
	slices.SortFunc(result.Recording.Records, func(a, b PlaybackRange) int { return a.Start.Compare(b.Start) })
	p.mu.Unlock()
	p.mark(result, "record_info_result", strconv.Itoa(count))
	if count == 0 {
		p.finish(result, "no_recordings", "")
	}
	return count > 0
}

// Called only for messages from the currently registered device, under p.mu.
func (p *platform) recordingPeerMessage(req *sip.Request) {
	playback := p.playback
	if playback == nil || !p.busy || req.Method != sip.MESSAGE || len(req.Body()) > 128<<10 {
		return
	}
	var msg struct {
		XMLName    xml.Name
		Command    string `xml:"CmdType"`
		SN         uint32
		DeviceID   string
		NotifyType int
		Total      *int `xml:"SumNum"`
		Items      []struct {
			StartTime, EndTime, FilePath string
		} `xml:"RecordList>Item"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(req.Body()))
	decoder.CharsetReader = charset.NewReaderLabel
	if decoder.Decode(&msg) != nil {
		return
	}
	if msg.XMLName.Local == "Notify" && msg.Command == "MediaStatus" && msg.NotifyType == 121 &&
		(msg.DeviceID == playback.report.ChannelID || msg.DeviceID == p.cfg.DeviceID) && playback.dialog != nil {
		select {
		case <-playback.ended:
		default:
			report := playback.report
			report.Timeline = append(report.Timeline, SessionMark{Stage: "recording_ended", Detail: "MediaStatus 121", OffsetMS: float64(time.Since(report.Start)) / float64(time.Millisecond)})
			close(playback.ended)
			if playback.wakeRead != nil {
				playback.wakeRead()
			}
		}
		return
	}
	if msg.XMLName.Local != "Response" || msg.Command != "RecordInfo" || msg.SN != playback.sn ||
		(msg.DeviceID != playback.report.ChannelID && msg.DeviceID != p.cfg.DeviceID) || msg.Total == nil || *msg.Total < 0 || *msg.Total > 512 {
		return
	}
	select {
	case <-playback.queryDone:
		return
	default:
	}
	recording := playback.report.Recording
	if recording.Total != nil && *recording.Total != *msg.Total {
		return
	}
	recording.Total = msg.Total
	for _, item := range msg.Items {
		begin, beginErr := parseRecordTime(item.StartTime)
		end, endErr := parseRecordTime(item.EndTime)
		if beginErr != nil || endErr != nil || begin.IsZero() || !begin.Before(end) || len(recording.Records) >= *msg.Total {
			continue
		}
		key := begin.Format(time.RFC3339Nano) + "/" + end.Format(time.RFC3339Nano) + "/" + item.FilePath
		if _, exists := playback.records[key]; !exists {
			playback.records[key] = struct{}{}
			recording.Records = append(recording.Records, PlaybackRange{Start: begin, End: end})
		}
	}
	if len(recording.Records) == *msg.Total {
		close(playback.queryDone)
	}
}

func (m *PlatformManager) ControlPlayback(action PlaybackControl) error {
	action.Method = strings.ToUpper(strings.TrimSpace(action.Method))
	var command string
	switch action.Method {
	case "PAUSE":
		command = "PauseTime: now"
	case "PLAY":
		command = "Range: npt=now-"
	default:
		return fmt.Errorf("%w，控制命令需为 PAUSE 或 PLAY", ErrPlatformConfig)
	}
	m.mu.Lock()
	p := m.run
	m.mu.Unlock()
	if p == nil {
		return ErrPlatformPeer
	}
	p.mu.Lock()
	playback := p.playback
	p.mu.Unlock()
	if playback == nil {
		return ErrPlatformPeer
	}
	playback.control.Lock()
	defer playback.control.Unlock()
	p.mu.Lock()
	dialog := playback.dialog
	ready := p.playback == playback && playback.report.State == "receiving" && p.ctx.Err() == nil
	p.mu.Unlock()
	if !ready || dialog == nil {
		return ErrPlatformPeer
	}
	if action.Offset != nil {
		offset := *action.Offset
		window := playback.report.Recording.End.Sub(playback.report.Recording.Start).Seconds()
		if action.Method != "PLAY" || !(offset >= 0 && offset < window) {
			return fmt.Errorf("%w，定位秒数应位于回放窗口内", ErrPlatformConfig)
		}
		command = fmt.Sprintf("Range: npt=%.3f-", offset)
	}
	req := sip.NewRequest(sip.INFO, dialog.InviteResponse.Contact().Address)
	req.AppendHeader(sip.NewHeader("Content-Type", "Application/MANSRTSP"))
	playback.controlCSeq++
	req.SetBody(fmt.Appendf(nil, "%s MANSRTSP/1.0\r\nCSeq: %d\r\n%s\r\n\r\n", action.Method, playback.controlCSeq, command))
	ctx, cancel := context.WithTimeout(p.ctx, 1500*time.Millisecond)
	defer cancel()
	res, err := dialog.Do(ctx, req)
	detail := action.Method + " · " + command
	if err != nil || res == nil {
		p.mark(playback.report, "recording_info_failed", detail)
		return errors.New("回放控制未收到应答")
	}
	p.mark(playback.report, "recording_info", fmt.Sprintf("%s · SIP %d", detail, res.StatusCode))
	if !res.IsSuccess() {
		return fmt.Errorf("设备拒绝回放控制（SIP %d）", res.StatusCode)
	}
	return nil
}
