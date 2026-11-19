package gb28181

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"golang.org/x/net/html/charset"
)

var (
	ErrChannelNotFound    = errors.New("找不到模拟通道")
	ErrChannelUnavailable = errors.New("设备未注册，无法切换通道状态")
	ErrCatalogBusy        = errors.New("目录通知等待发送，请稍后再试")
	catalogEventPattern   = regexp.MustCompile(`(?i)^catalog(?:;id=[0-9]{1,20})?$`)
)

const maxCatalogSubscriptions = 16

type catalogSubscription struct {
	callID, event, scope string
	from                 sip.FromHeader
	to                   sip.ToHeader
	recipient            sip.Uri
	until                time.Time
	cseq, remoteSeq      uint32
}

type catalogJob struct {
	ready    <-chan struct{} // The SIP 200 must be sent before its initial NOTIFY.
	requests []*sip.Request
}

func (d *device) onSubscribe(flow *sipFlow, req *sip.Request, tx sip.ServerTransaction) {
	header := req.GetHeader("Event")
	if header == nil {
		d.respond(req, tx, 489, "Bad Event")
		return
	}
	event := strings.ReplaceAll(strings.TrimSpace(header.Value()), " ", "")
	if !catalogEventPattern.MatchString(event) {
		d.respond(req, tx, 489, "Bad Event")
		return
	}
	event = "Catalog" + event[len("Catalog"):]
	if req.CallID() == nil || req.From() == nil || req.To() == nil || req.CSeq() == nil || req.Contact() == nil {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	remoteTag, _ := req.From().Params.Get("tag")
	localTag, _ := req.To().Params.Get("tag")
	if remoteTag == "" {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	expiry := 600
	if value := req.GetHeader("Expires"); value != nil {
		var err error
		expiry, err = strconv.Atoi(value.Value())
		if err != nil || expiry < 0 {
			d.respond(req, tx, 400, "Bad Request")
			return
		}
		expiry = min(expiry, 86400)
	}
	scope := ""
	if body := req.Body(); len(body) > 0 {
		var query simulatorQuery
		decoder := xml.NewDecoder(bytes.NewReader(body))
		decoder.CharsetReader = charset.NewReaderLabel
		if err := decoder.Decode(&query); err != nil || query.XMLName.Local != "Query" || query.Command != "Catalog" {
			d.respond(req, tx, 400, "Bad Request")
			return
		}
		scope = query.DeviceID
		if d.channelFor(scope) == "" {
			d.respond(req, tx, 404, "Not Found")
			return
		}
	} else if localTag == "" {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	if d.channelFor(req.Recipient.User) == "" {
		d.respond(req, tx, 404, "Not Found")
		return
	}
	flow.awaitRegistration()

	response := sip.NewResponseFromRequest(req, 200, "OK", nil)
	response.AppendHeader(d.contact(flow))
	response.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expiry)))
	ready := make(chan struct{})
	code, reason := d.applySubscription(flow, req, response, localTag, remoteTag, scope, event, expiry, ready)
	if code != 200 {
		d.respond(req, tx, code, reason)
		return
	}
	err := tx.Respond(response)
	close(ready)
	if err != nil {
		d.log("system", "目录订阅应答发送失败："+err.Error(), "")
	}
}

func (d *device) applySubscription(flow *sipFlow, req *sip.Request, res *sip.Response, localTag, remoteTag, scope, event string, expiry int, ready <-chan struct{}) (int, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State != "registered" || d.flow != flow || flow.ctx.Err() != nil {
		return 480, "Temporarily Unavailable"
	}
	now := time.Now()
	flow.pruneCatalog(now)
	callID := req.CallID().Value()
	key := callID + "\x00" + strings.ToLower(event)
	var sub catalogSubscription
	if prev := flow.catalog[key]; prev != nil {
		fromTag, _ := prev.from.Params.Get("tag")
		toTag, _ := prev.to.Params.Get("tag")
		if localTag != fromTag || remoteTag != toTag {
			return 481, "Call/Transaction Does Not Exist"
		}
		if req.CSeq().SeqNo <= prev.remoteSeq {
			return 500, "CSeq Out of Order"
		}
		if scope != "" && scope != prev.scope {
			return 400, "Bad Request"
		}
		sub = *prev
	} else {
		if localTag != "" || expiry == 0 {
			return 481, "Call/Transaction Does Not Exist"
		}
		if len(flow.catalog) >= maxCatalogSubscriptions {
			return 503, "Service Unavailable"
		}
		sub = catalogSubscription{callID: callID, event: event, scope: scope,
			from: sip.FromHeader{Address: *res.To().Address.Clone(), Params: res.To().Params.Clone()},
			to:   sip.ToHeader{Address: *req.From().Address.Clone(), Params: req.From().Params.Clone()}}
	}
	if len(flow.catalogJobs) >= cap(flow.catalogJobs) {
		return 503, "Service Unavailable"
	}
	sub.until = now.Add(time.Duration(expiry) * time.Second)
	sub.remoteSeq = req.CSeq().SeqNo
	sub.recipient = *req.Contact().Address.Clone()
	var requests []*sip.Request
	if expiry == 0 {
		delete(flow.catalog, key)
		requests = append(requests, d.catalogNotify(flow, &sub, "terminated;reason=deactivated", nil))
	} else {
		state := "active;expires=" + strconv.Itoa(expiry)
		for _, channel := range d.channels {
			if d.offline[channel] && sub.includes(d.cfg.DeviceID, channel) {
				requests = append(requests, d.catalogNotify(flow, &sub, state, d.catalogBody(sub.scope, channel, false)))
			}
		}
		if len(requests) == 0 {
			requests = append(requests, d.catalogNotify(flow, &sub, state, d.catalogBody(sub.scope, "", true)))
		}
		flow.catalog[key] = &sub
	}
	flow.catalogJobs <- catalogJob{ready: ready, requests: requests}
	return 200, "OK"
}

func (f *sipFlow) pruneCatalog(now time.Time) {
	for key, sub := range f.catalog {
		if !now.Before(sub.until) {
			delete(f.catalog, key)
		}
	}
}

func (s *catalogSubscription) includes(device, channel string) bool {
	return s.scope == device || s.scope == channel
}

func (d *device) catalogNotify(flow *sipFlow, s *catalogSubscription, state string, body []byte) *sip.Request {
	req := sip.NewRequest(sip.NOTIFY, *s.recipient.Clone())
	req.SetDestination(d.platform.String())
	req.SetTransport(strings.ToUpper(d.cfg.SIPTransport))
	req.SetBody(body)
	req.AppendHeader(sip.HeaderClone(&s.from))
	req.AppendHeader(sip.HeaderClone(&s.to))
	callID := sip.CallIDHeader(s.callID)
	req.AppendHeader(&callID)
	s.cseq++
	req.AppendHeader(&sip.CSeqHeader{SeqNo: s.cseq, MethodName: sip.NOTIFY})
	req.AppendHeader(d.contact(flow))
	req.AppendHeader(sip.NewHeader("Event", s.event))
	req.AppendHeader(sip.NewHeader("Subscription-State", state))
	if len(body) > 0 {
		req.AppendHeader(sip.NewHeader("Content-Type", manscdp))
	}
	return req
}

func (d *device) catalogBody(scope, channel string, online bool) []byte {
	num, item := 0, ""
	if channel != "" {
		event := "ON"
		if !online {
			event = "OFF"
		}
		num = 1
		item = fmt.Sprintf("<Item><DeviceID>%s</DeviceID><Event>%s</Event><Status>%s</Status></Item>", channel, event, event)
	}
	return fmt.Appendf(nil, `%s<Notify><CmdType>Catalog</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><SumNum>%d</SumNum><DeviceList Num="%d">%s</DeviceList></Notify>`,
		xml.Header, d.xmlSN.Add(1), scope, num, num, item)
}

func (d *device) sendCatalog(flow *sipFlow) {
	for {
		select {
		case <-flow.ctx.Done():
			return
		case job := <-flow.catalogJobs:
			if job.ready != nil {
				select {
				case <-job.ready:
				case <-flow.ctx.Done():
					return
				}
			}
			for _, req := range job.requests {
				ctx, cancel := context.WithTimeout(flow.ctx, 4*time.Second)
				response, err := flow.client.Do(ctx, req)
				cancel()
				if err != nil || response == nil || !response.IsSuccess() {
					if flow.ctx.Err() == nil {
						d.log("system", "目录 NOTIFY 未获平台确认", "")
					}
				}
			}
		}
	}
}

func (m *Manager) SetChannelOnline(deviceID, channelID string, online bool) error {
	if f := m.currentFleet(); f != nil {
		for _, d := range f.devices {
			if d.cfg.DeviceID == deviceID && d.hasChannel(channelID) {
				return d.setChannelOnline(channelID, online)
			}
		}
	}
	return ErrChannelNotFound
}

func (d *device) setChannelOnline(channel string, online bool) error {
	d.mu.Lock()
	flow := d.flow
	if d.status.State != "registered" || flow == nil || flow.ctx.Err() != nil {
		d.mu.Unlock()
		return ErrChannelUnavailable
	}
	if d.offline[channel] == !online {
		d.mu.Unlock()
		return nil
	}
	now := time.Now()
	flow.pruneCatalog(now)
	var requests []*sip.Request
	for _, sub := range flow.catalog {
		if !sub.includes(d.cfg.DeviceID, channel) {
			continue
		}
		if len(flow.catalogJobs) >= cap(flow.catalogJobs) {
			d.mu.Unlock()
			return ErrCatalogBusy
		}
		remaining := max(1, int(sub.until.Sub(now).Seconds()))
		state := "active;expires=" + strconv.Itoa(remaining)
		requests = append(requests, d.catalogNotify(flow, sub, state, d.catalogBody(sub.scope, channel, online)))
	}
	if len(requests) > 0 {
		flow.catalogJobs <- catalogJob{requests: requests}
	}
	if online {
		delete(d.offline, channel)
	} else {
		d.offline[channel] = true
		if session := d.media[channel]; session != nil {
			d.closeMediaLocked(session)
		}
	}
	d.mu.Unlock()
	state := "上线"
	if !online {
		state = "离线"
	}
	d.log("system", "通道 "+channel+" "+state, "")
	return nil
}
