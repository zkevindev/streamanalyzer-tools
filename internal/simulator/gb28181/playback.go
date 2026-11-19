package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

var (
	errPlaybackSeek = errors.New("录像定位")
	errPlaybackStop = errors.New("结束回放")
)

type playbackCommand struct {
	method string
	scale  float64
	seek   bool
	start  time.Duration
	end    time.Duration
}

func parsePlaybackCommand(body string, duration time.Duration) (playbackCommand, error) {
	first, rest, _ := strings.Cut(strings.TrimSpace(body), "\n")
	fields := strings.Fields(first)
	if len(fields) != 2 || !strings.EqualFold(fields[1], "MANSRTSP/1.0") {
		return playbackCommand{}, errors.New("回放控制需要 MANSRTSP/1.0")
	}
	command := playbackCommand{method: strings.ToUpper(fields[0]), scale: 1, end: duration}
	switch command.method {
	case "PAUSE", "TEARDOWN":
		return command, nil
	case "PLAY":
	default:
		return command, errors.New("未知回放控制命令")
	}
	for _, line := range strings.Split(rest, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "scale":
			scale, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(scale) || scale < 0.25 || scale > 4 {
				return command, errors.New("回放倍速应为 0.25–4")
			}
			command.scale = scale
		case "range":
			start, end, ok := strings.Cut(strings.TrimPrefix(value, "npt="), "-")
			if !strings.HasPrefix(value, "npt=") || !ok {
				return command, errors.New("回放 Range 应为 npt=秒数-")
			}
			if strings.EqualFold(strings.TrimSpace(start), "now") && strings.TrimSpace(end) == "" {
				continue
			}
			offset, err := parsePlaybackOffset(start, duration)
			if err != nil || offset >= duration {
				return command, errors.New("回放起点超出会话时间范围")
			}
			command.seek, command.start = true, offset
			if strings.TrimSpace(end) != "" {
				finish, err := parsePlaybackOffset(end, duration)
				if err != nil || finish <= offset {
					return command, errors.New("回放结束时间无效")
				}
				command.end = finish
			}
		}
	}
	return command, nil
}

func parsePlaybackOffset(value string, limit time.Duration) (time.Duration, error) {
	offset, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(offset) || offset < 0 || offset > limit.Seconds() {
		return 0, errors.New("回放偏移超出范围")
	}
	return time.Duration(offset * float64(time.Second)), nil
}

type playbackSession struct {
	window   recordingWindow
	commands chan playbackCommand
}

func (p *playbackSession) run(ctx context.Context, file *localMedia, send func(mediaUnit) error, mark func(string, string)) error {
	parts := p.window.parts
	var nextParts []recordingPart
	scale := 1.0
	paused := false
	var output int64
	var sent bool
	apply := func(command playbackCommand) error {
		switch command.method {
		case "PAUSE":
			paused = true
			mark("playback_pause", "")
		case "PLAY":
			paused = false
			scale = command.scale
			if command.seek {
				nextParts = p.window.selectParts(command.start, command.end)
				mark("playback_seek", fmt.Sprintf("%.3f s · %.2fx", command.start.Seconds(), scale))
				return errPlaybackSeek
			}
			mark("playback_play", fmt.Sprintf("%.2fx", scale))
		case "TEARDOWN":
			return errPlaybackStop
		}
		return nil
	}
	var previousEnd time.Time
	for len(parts) > 0 {
		part := parts[0]
		if !previousEnd.IsZero() && previousEnd.Before(part.start) {
			mark("playback_gap", fmt.Sprintf("%s → %s", previousEnd.Format(time.RFC3339), part.start.Format(time.RFC3339)))
		}
		mark("playback_recording", fmt.Sprintf("录像 %d · %s", part.recordIndex, part.start.Format(time.RFC3339)))
		first := true
		var lastDTS int64
		var lastWall time.Time
		consume := func(unit mediaUnit) error {
			delta := int64(0)
			if !first {
				delta = max(0, unit.dts-lastDTS)
			}
			for {
				if paused {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case command := <-p.commands:
						if err := apply(command); err != nil {
							return err
						}
						lastWall = time.Now()
					}
					continue
				}
				// Apply commands already queued before the next packet.
				select {
				case <-ctx.Done():
					return ctx.Err()
				case command := <-p.commands:
					if err := apply(command); err != nil {
						return err
					}
					continue
				default:
				}
				delay := time.Duration(float64(clockDuration(delta))/scale) - time.Since(lastWall)
				if delay <= 0 {
					break
				}
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case command := <-p.commands:
					timer.Stop()
					if err := apply(command); err != nil {
						return err
					}
					continue
				}
				break
			}
			if first && sent {
				output++
			}
			output += int64(math.Round(float64(delta) / scale))
			first, lastDTS, lastWall = false, unit.dts, time.Now()
			lead := int64(math.Round(float64(unit.pts-unit.dts) / scale))
			unit.dts, unit.pts = output, output+lead
			if scale != 1 && unit.stream == audioStreamID && len(unit.data) > 0 {
				data := make([]byte, max(1, int(math.Round(float64(len(unit.data))/scale))))
				for i := range data {
					data[i] = unit.data[min(len(unit.data)-1, int(float64(i)*scale))]
				}
				unit.data = data
			}
			sent = true
			return send(unit)
		}
		span := fileRange{start: part.start.Sub(part.origin), end: part.end.Sub(part.origin)}
		err := file.read(ctx, &span, consume)
		switch {
		case errors.Is(err, errPlaybackSeek):
			parts, previousEnd = nextParts, time.Time{}
		case errors.Is(err, errPlaybackStop):
			return io.EOF
		case errors.Is(err, io.EOF):
			parts, previousEnd = parts[1:], part.end
		default:
			return err
		}
	}
	return io.EOF
}

func (d *device) onPlaybackInfo(req *sip.Request, tx sip.ServerTransaction) {
	d.mu.Lock()
	session := d.dialog(req)
	if session == nil || session.playback == nil || session.ctx.Err() != nil {
		d.mu.Unlock()
		d.respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	if !session.started || session.status.MediaState == "playback_done" {
		d.mu.Unlock()
		d.respond(req, tx, 491, "Request Pending")
		return
	}
	window := session.playback.window
	command, err := parsePlaybackCommand(string(req.Body()), window.end.Sub(window.start))
	if err != nil || command.seek && len(window.selectParts(command.start, command.end)) == 0 {
		d.mu.Unlock()
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	select {
	case session.playback.commands <- command:
		d.mu.Unlock()
		d.respond(req, tx, 200, "OK")
	default:
		d.mu.Unlock()
		d.respond(req, tx, 503, "Service Unavailable")
	}
}

func (d *device) finishPlayback(session *mediaSession) {
	d.mu.Lock()
	if d.media[session.channelID] != session || session.ctx.Err() != nil {
		d.mu.Unlock()
		return
	}
	session.evidence.mark("playback_done", "", time.Now(), true)
	session.conn.Close()
	session.status.MediaState = "playback_done"
	d.mu.Unlock()
	body := fmt.Sprintf("%s<Notify><CmdType>MediaStatus</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><NotifyType>121</NotifyType></Notify>",
		xml.Header, d.xmlSN.Add(1), session.channelID)
	if err := d.sendXML(session.flow, []byte(body)); err != nil && session.ctx.Err() == nil {
		d.log("system", "回放结束通知未获确认："+err.Error(), "")
	}
}
