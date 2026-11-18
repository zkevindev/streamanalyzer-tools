package gb28181

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
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

func (d *device) onMessage(flow *sipFlow, req *sip.Request, tx sip.ServerTransaction) {
	var query struct {
		XMLName  xml.Name
		Command  string `xml:"CmdType"`
		SN       uint32
		DeviceID string
	}
	decoder := xml.NewDecoder(bytes.NewReader(req.Body()))
	decoder.CharsetReader = charset.NewReaderLabel
	if err := decoder.Decode(&query); err != nil {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	if query.XMLName.Local != "Query" {
		d.respond(req, tx, 501, "Not Implemented")
		return
	}
	if query.DeviceID != d.cfg.DeviceID && query.DeviceID != d.cfg.ChannelID {
		d.respond(req, tx, 404, "Not Found")
		return
	}
	reply := d.queryReply(query.Command, query.SN, query.DeviceID)
	if reply == nil {
		d.respond(req, tx, 501, "Not Implemented")
		return
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
	if err := d.sendXML(flow, reply); err != nil && flow.ctx.Err() == nil {
		d.log("system", query.Command+" 应答未获确认："+err.Error(), "")
	}
}

func (d *device) queryReply(command string, sn uint32, target string) []byte {
	var detail string
	switch command {
	case "Catalog":
		channel := d.cfg.ChannelID
		detail = fmt.Sprintf(`<SumNum>1</SumNum><DeviceList Num="1">
<Item><DeviceID>%s</DeviceID><Name>Test Camera</Name><Manufacturer>Stream Analyzer</Manufacturer><Model>Simulator</Model><Owner>Simulator</Owner><CivilCode>%s</CivilCode><Address>Laboratory</Address><Parental>0</Parental><ParentID>%s</ParentID><SafetyWay>0</SafetyWay><RegisterWay>1</RegisterWay><Secrecy>0</Secrecy><Status>ON</Status></Item>
</DeviceList>`, channel, channel[:6], d.cfg.DeviceID)
	case "DeviceInfo":
		detail = "<Result>OK</Result><DeviceName>Test Camera</DeviceName><Manufacturer>Stream Analyzer</Manufacturer><Model>Simulator</Model><Firmware>1.0</Firmware><Channel>1</Channel>"
	case "DeviceStatus":
		detail = "<Result>OK</Result><Online>ONLINE</Online><Status>OK</Status><Encode>ON</Encode><Record>OFF</Record>"
	default:
		return nil
	}
	return []byte(fmt.Sprintf("%s<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>%s</DeviceID>%s</Response>",
		xml.Header, command, sn, target, detail))
}
