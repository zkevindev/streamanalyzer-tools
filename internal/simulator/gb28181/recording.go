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

func (m *localMedia) setRecording(value string, now time.Time) error {
	if m.duration <= 0 {
		if value != "" {
			return errors.New("无法识别所选文件的录像时长")
		}
		return nil
	}
	span := time.Duration(math.Ceil(m.duration.Seconds())) * time.Second
	start := now.Truncate(time.Second).Add(-span)
	if value != "" {
		var err error
		start, err = time.Parse(time.RFC3339, value)
		if err != nil || start.Unix() <= 0 {
			return errors.New("录像开始时间应为有效的 RFC3339 日期")
		}
		start = start.Truncate(time.Second)
	}
	m.recording = &RecordingInfo{Start: start, End: start.Add(span), DurationMS: m.duration.Milliseconds()}
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

func (d *device) recordInfo(query simulatorQuery) ([]byte, error) {
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
	count, item := 0, ""
	kind := strings.ToLower(strings.TrimSpace(query.Type))
	if d.file != nil && d.file.recording != nil && (kind == "" || kind == "all" || kind == "time") {
		record := d.file.recording
		if (from.IsZero() || from.Before(record.End)) && (to.IsZero() || to.After(record.Start)) {
			count = 1
			item = fmt.Sprintf("<Item><DeviceID>%s</DeviceID><Name>Local Recording</Name><StartTime>%s</StartTime><EndTime>%s</EndTime><Secrecy>0</Secrecy><Type>time</Type><RecorderID>%s</RecorderID></Item>",
				d.cfg.ChannelID, record.Start.In(time.Local).Format(recordTimeLayout), record.End.In(time.Local).Format(recordTimeLayout), d.cfg.DeviceID)
		}
	}
	return []byte(fmt.Sprintf("%s<Response><CmdType>RecordInfo</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Name>Test Camera</Name><SumNum>%d</SumNum><RecordList Num=\"%d\">%s</RecordList></Response>",
		xml.Header, query.SN, query.DeviceID, count, count, item)), nil
}

type fileRange struct {
	start time.Duration
	end   time.Duration
}

func (m *localMedia) playbackRange(offer *mediaOffer) (fileRange, error) {
	if m == nil || m.recording == nil {
		return fileRange{}, errors.New("请选择具有已知时长的本地录像文件")
	}
	start := time.Unix(offer.start, 0)
	end := time.Unix(offer.end, 0)
	if start.Before(m.recording.Start) {
		start = m.recording.Start
	}
	if end.After(m.recording.End) {
		end = m.recording.End
	}
	if !start.Before(end) {
		return fileRange{}, errors.New("请求时间段与本地录像无交集")
	}
	begin := start.Sub(m.recording.Start)
	finish := min(end.Sub(m.recording.Start), m.duration)
	if begin >= finish {
		return fileRange{}, errors.New("请求时间段与本地录像无交集")
	}
	offer.start, offer.end = start.Unix(), end.Unix()
	return fileRange{start: begin, end: finish}, nil
}

func secondsArg(value time.Duration) string {
	return strconv.FormatFloat(value.Seconds(), 'f', 6, 64)
}
