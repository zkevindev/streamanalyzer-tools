package gb28181

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"golang.org/x/net/html/charset"
)

const manscdp = "Application/MANSCDP+xml"

func (d *device) routes(server *sipgo.Server, flow *sipFlow) {
	server.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		d.respond(req, tx, 200, "OK")
	})
	server.OnMessage(func(req *sip.Request, tx sip.ServerTransaction) {
		d.onMessage(flow, req, tx)
	})
	server.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		d.onInvite(flow, req, tx)
	})
	server.OnRequest(sip.SUBSCRIBE, func(req *sip.Request, tx sip.ServerTransaction) { d.onSubscribe(flow, req, tx) })
	server.OnRequest(sip.INFO, d.onPlaybackInfo)
	server.OnAck(d.onAck)
	server.OnBye(d.onBye)
	server.OnNoRoute(func(req *sip.Request, tx sip.ServerTransaction) {
		d.respond(req, tx, 501, "Not Implemented")
	})
}

func (d *device) respond(req *sip.Request, tx sip.ServerTransaction, code int, reason string) {
	if tx != nil {
		if err := tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil)); err != nil {
			d.log("system", "SIP 应答发送失败："+err.Error(), "")
		}
	}
}

func (d *device) sendXML(flow *sipFlow, body []byte) error {
	ctx, cancel := context.WithTimeout(flow.ctx, 4*time.Second)
	defer cancel()
	req := d.request(sip.MESSAGE, body)
	req.AppendHeader(sip.NewHeader("Content-Type", manscdp))
	response, err := flow.client.Do(ctx, req)
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("MESSAGE 事务结束前未收到响应")
	}
	if !response.IsSuccess() {
		return fmt.Errorf("平台返回 SIP %d", response.StatusCode)
	}
	return nil
}

func (d *device) keepalive() []byte {
	return []byte(fmt.Sprintf("%s<Notify><CmdType>Keepalive</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Status>OK</Status></Notify>",
		xml.Header, d.xmlSN.Add(1), d.cfg.DeviceID))
}

type simulatorQuery struct {
	XMLName   xml.Name
	Command   string `xml:"CmdType"`
	SN        uint32
	SourceID  string
	TargetID  string
	DeviceID  string
	StartTime string
	EndTime   string
	Type      string
}

func (d *device) onMessage(flow *sipFlow, req *sip.Request, tx sip.ServerTransaction) {
	var query simulatorQuery
	decoder := xml.NewDecoder(bytes.NewReader(req.Body()))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&query); err != nil {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	if query.XMLName.Local == "Notify" && query.Command == "Broadcast" {
		d.onBroadcast(flow, req, tx, query)
		return
	}
	if query.XMLName.Local != "Query" {
		d.respond(req, tx, 501, "Not Implemented")
		return
	}
	if d.channelFor(query.DeviceID) == "" && (d.outputID == "" || query.DeviceID != d.outputID) {
		d.respond(req, tx, 404, "Not Found")
		return
	}
	var replies [][]byte
	switch query.Command {
	case "Catalog":
		replies = d.catalogReplies(query)
	case "RecordInfo":
		reply, err := d.recordInfo(query)
		if err != nil {
			d.respond(req, tx, 400, "Bad Request")
			return
		}
		replies = [][]byte{reply}
	default:
		reply := d.queryReply(query.Command, query.SN, query.DeviceID)
		if reply == nil {
			d.respond(req, tx, 501, "Not Implemented")
			return
		}
		replies = [][]byte{reply}
	}
	flow.awaitRegistration()
	d.mu.Lock()
	if d.status.State != "registered" || flow.ctx.Err() != nil {
		d.mu.Unlock()
		d.respond(req, tx, 480, "Temporarily Unavailable")
		return
	}
	d.workers.Add(1)
	d.mu.Unlock()
	defer d.workers.Done()
	d.respond(req, tx, 200, "OK")
	for _, reply := range replies {
		if err := d.sendXML(flow, reply); err != nil {
			if flow.ctx.Err() == nil {
				d.log("system", query.Command+" 应答未获确认："+err.Error(), "")
			}
			return
		}
	}
}

func (d *device) catalogReplies(query simulatorQuery) [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	channels := d.channels
	if query.DeviceID != d.cfg.DeviceID {
		channels = []string{query.DeviceID}
	} else if d.outputID != "" {
		channels = append(slices.Clone(d.channels), d.outputID)
	}
	// A single Catalog item fits the SIP UDP datagram limit. Keep the total
	// and SN constant across replies, and let TCP carry the complete list.
	pageSize := len(channels)
	if d.cfg.SIPTransport == "udp" {
		pageSize = 1
	}
	var replies [][]byte
	for from := 0; from < len(channels); from += pageSize {
		page := channels[from:min(from+pageSize, len(channels))]
		var body strings.Builder
		fmt.Fprintf(&body, `<SumNum>%d</SumNum><DeviceList Num="%d">`, len(channels), len(page))
		for _, channel := range page {
			if channel == d.outputID {
				body.WriteString(d.voiceOutputItem())
				continue
			}
			status := "ON"
			if d.offline[channel] {
				status = "OFF"
			}
			fmt.Fprintf(&body, `<Item><DeviceID>%s</DeviceID><Name>Test Camera</Name><Manufacturer>Stream Analyzer</Manufacturer><Model>Simulator</Model><Owner>Simulator</Owner><CivilCode>%s</CivilCode><Address>Laboratory</Address><Parental>0</Parental><ParentID>%s</ParentID><SafetyWay>0</SafetyWay><RegisterWay>1</RegisterWay><Secrecy>0</Secrecy><Status>%s</Status></Item>`,
				channel, channel[:6], d.cfg.DeviceID, status)
		}
		body.WriteString("</DeviceList>")
		replies = append(replies, queryResponse(query.Command, query.SN, query.DeviceID, body.String()))
	}
	return replies
}

func (d *device) queryReply(command string, sn uint32, target string) []byte {
	var detail string
	switch command {
	case "DeviceInfo":
		detail = fmt.Sprintf("<Result>OK</Result><DeviceName>Test Camera</DeviceName><Manufacturer>Stream Analyzer</Manufacturer><Model>Simulator</Model><Firmware>1.0</Firmware><Channel>%d</Channel>", len(d.channels))
	case "DeviceStatus":
		detail = "<Result>OK</Result><Online>ONLINE</Online><Status>OK</Status><Encode>ON</Encode><Record>OFF</Record>"
	default:
		return nil
	}
	return queryResponse(command, sn, target, detail)
}

func queryResponse(command string, sn uint32, target, detail string) []byte {
	return []byte(fmt.Sprintf("%s<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>%s</DeviceID>%s</Response>",
		xml.Header, command, sn, target, detail))
}
