package gb28181

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const recordTimeLayout = "2006-01-02T15:04:05"

type RecordingInfo struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationMS int64     `json:"duration_ms"`
}

func (m *localMedia) setRecording(cfg Config, now time.Time) error {
	value := cfg.RecordingStart
	if m.duration <= 0 {
		if value != "" || cfg.RecordingCount > 1 {
			return errors.New("无法识别所选文件的录像时长")
		}
		return nil
	}
	span := time.Duration(math.Ceil(m.duration.Seconds())) * time.Second
	count := max(cfg.RecordingCount, 1)
	gap := time.Duration(cfg.RecordingGap) * time.Second
	start := now.Truncate(time.Second).Add(-span*time.Duration(count) - gap*time.Duration(count-1))
	if value != "" {
		var err error
		start, err = time.Parse(time.RFC3339, value)
		if err != nil || start.Unix() <= 0 {
			return errors.New("录像开始时间应为有效的 RFC3339 日期")
		}
		start = start.Truncate(time.Second)
	}
	for range count {
		m.recordings = append(m.recordings, RecordingInfo{Start: start, End: start.Add(span), DurationMS: m.duration.Milliseconds()})
		start = start.Add(span + gap)
	}
	return nil
}

func parseRecordTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return at, nil
	}
	return time.ParseInLocation(recordTimeLayout, value, time.Local)
}

func (d *device) recordInfo(query simulatorQuery) ([][]byte, error) {
	from, err := parseRecordTime(strings.TrimSpace(query.StartTime))
	if err != nil {
		return nil, err
	}
	to, err := parseRecordTime(strings.TrimSpace(query.EndTime))
	if err != nil {
		return nil, err
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return nil, errors.New("录像查询起止时间无效")
	}
	var items []string
	kind := strings.ToLower(strings.TrimSpace(query.Type))
	if channel := d.channelFor(query.DeviceID); channel != "" && d.file != nil && (kind == "" || kind == "all" || kind == "time") {
		for i, record := range d.file.recordings {
			if (from.IsZero() || from.Before(record.End)) && (to.IsZero() || to.After(record.Start)) {
				name := "Local Recording"
				if len(d.file.recordings) > 1 {
					name = fmt.Sprintf("%s %d", name, i+1)
				}
				items = append(items, fmt.Sprintf("<Item><DeviceID>%s</DeviceID><Name>%s</Name><StartTime>%s</StartTime><EndTime>%s</EndTime><Secrecy>0</Secrecy><Type>time</Type><RecorderID>%s</RecorderID></Item>",
					channel, name, record.Start.In(time.Local).Format(recordTimeLayout), record.End.In(time.Local).Format(recordTimeLayout), d.cfg.DeviceID))
			}
		}
	}
	page := func(list []string) []byte {
		return fmt.Appendf(nil, `%s<Response><CmdType>RecordInfo</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Name>Test Camera</Name><SumNum>%d</SumNum><RecordList Num="%d">%s</RecordList></Response>`,
			xml.Header, query.SN, query.DeviceID, len(items), len(list), strings.Join(list, ""))
	}
	if d.cfg.SIPTransport == "tcp" || len(items) < 2 {
		return [][]byte{page(items)}, nil
	}
	var replies [][]byte
	for _, item := range items {
		replies = append(replies, page([]string{item}))
	}
	return replies, nil
}

type fileRange struct {
	start time.Duration
	end   time.Duration
}

type recordingPart struct {
	recordIndex int
	origin      time.Time
	start       time.Time
	end         time.Time
}

type recordingWindow struct {
	start, end time.Time
	parts      []recordingPart
}

func (m *localMedia) playbackRange(offer *mediaOffer) (recordingWindow, error) {
	var window recordingWindow
	if m == nil || len(m.recordings) == 0 {
		return window, errors.New("请选择具有已知时长的本地录像文件")
	}
	start, end := time.Unix(offer.start, 0), time.Unix(offer.end, 0)
	var announcedEnd time.Time
	for i, record := range m.recordings {
		begin := start
		if begin.Before(record.Start) {
			begin = record.Start
		}
		finish := end
		if limit := record.Start.Add(m.duration); finish.After(limit) {
			finish = limit
		}
		if !begin.Before(finish) {
			continue
		}
		window.parts = append(window.parts, recordingPart{recordIndex: i + 1, origin: record.Start, start: begin, end: finish})
		announcedEnd = end
		if announcedEnd.After(record.End) {
			announcedEnd = record.End
		}
	}
	if len(window.parts) == 0 {
		return window, errors.New("请求时间段与本地录像无交集")
	}
	window.start, window.end = window.parts[0].start, window.parts[len(window.parts)-1].end
	offer.start, offer.end = window.start.Unix(), announcedEnd.Unix()
	return window, nil
}

func (w recordingWindow) selectParts(begin, end time.Duration) []recordingPart {
	start, finish := w.start.Add(begin), w.start.Add(end)
	var out []recordingPart
	for _, part := range w.parts {
		if part.start.Before(start) {
			part.start = start
		}
		if part.end.After(finish) {
			part.end = finish
		}
		if part.start.Before(part.end) {
			out = append(out, part)
		}
	}
	return out
}

func secondsArg(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', 6, 64)
}
