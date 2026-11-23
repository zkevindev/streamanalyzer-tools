package dash

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MPD represents a parsed ISO/IEC 23009-1 MPEG-DASH Media Presentation Description.
type MPD struct {
	Type                        string   `json:"type"`
	Profiles                    string   `json:"profiles,omitempty"`
	MediaPresentationDuration   string   `json:"media_presentation_duration,omitempty"`
	MediaPresentationDurationMS int64    `json:"media_presentation_duration_ms,omitempty"`
	MinBufferTime               string   `json:"min_buffer_time,omitempty"`
	MinBufferTimeMS             int64    `json:"min_buffer_time_ms,omitempty"`
	MinimumUpdatePeriod         string   `json:"minimum_update_period,omitempty"`
	MinimumUpdatePeriodMS       int64    `json:"minimum_update_period_ms,omitempty"`
	AvailabilityStartTime       string   `json:"availability_start_time,omitempty"`
	PublishTime                 string   `json:"publish_time,omitempty"`
	BaseURLs                    []string `json:"base_urls,omitempty"`
	Periods                     []Period `json:"periods"`
}

type Period struct {
	ID             string          `json:"id,omitempty"`
	Start          string          `json:"start,omitempty"`
	StartMS        int64           `json:"start_ms,omitempty"`
	Duration       string          `json:"duration,omitempty"`
	DurationMS     int64           `json:"duration_ms,omitempty"`
	BaseURLs       []string        `json:"base_urls,omitempty"`
	AdaptationSets []AdaptationSet `json:"adaptation_sets"`
}

type AdaptationSet struct {
	ID               string           `json:"id,omitempty"`
	MimeType         string           `json:"mime_type,omitempty"`
	ContentType      string           `json:"content_type,omitempty"`
	Codecs           string           `json:"codecs,omitempty"`
	Lang             string           `json:"lang,omitempty"`
	SegmentAlignment string           `json:"segment_alignment,omitempty"`
	Width            int              `json:"width,omitempty"`
	Height           int              `json:"height,omitempty"`
	FrameRate        string           `json:"frame_rate,omitempty"`
	BaseURLs         []string         `json:"base_urls,omitempty"`
	DRM              []string         `json:"drm,omitempty"`
	SegmentTemplate  *SegmentTemplate `json:"segment_template,omitempty"`
	SegmentList      *SegmentList     `json:"segment_list,omitempty"`
	Representations  []Representation `json:"representations"`
}

type Representation struct {
	ID                string           `json:"id"`
	Bandwidth         int64            `json:"bandwidth"`
	MimeType          string           `json:"mime_type,omitempty"`
	Codecs            string           `json:"codecs,omitempty"`
	Width             int              `json:"width,omitempty"`
	Height            int              `json:"height,omitempty"`
	FrameRate         string           `json:"frame_rate,omitempty"`
	AudioSamplingRate string           `json:"audio_sampling_rate,omitempty"`
	BaseURLs          []string         `json:"base_urls,omitempty"`
	SegmentBase       *SegmentBase     `json:"segment_base,omitempty"`
	SegmentTemplate   *SegmentTemplate `json:"segment_template,omitempty"`
	SegmentList       *SegmentList     `json:"segment_list,omitempty"`
}

type SegmentTemplate struct {
	Timescale      uint64          `json:"timescale,omitempty"`
	Duration       uint64          `json:"duration,omitempty"`
	StartNumber    uint64          `json:"start_number,omitempty"`
	Initialization string          `json:"initialization,omitempty"`
	Media          string          `json:"media,omitempty"`
	Timeline       []TimelineEntry `json:"timeline,omitempty"`
}

type TimelineEntry struct {
	T *uint64 `json:"t,omitempty"`
	D uint64  `json:"d"`
	R int64   `json:"r,omitempty"`
}

type SegmentList struct {
	Timescale         uint64   `json:"timescale,omitempty"`
	Duration          uint64   `json:"duration,omitempty"`
	StartNumber       uint64   `json:"start_number,omitempty"`
	InitializationURL string   `json:"initialization_url,omitempty"`
	SegmentURLs       []string `json:"segment_urls,omitempty"`
}

type SegmentBase struct {
	Timescale           uint64 `json:"timescale,omitempty"`
	IndexRange          string `json:"index_range,omitempty"`
	InitializationRange string `json:"initialization_range,omitempty"`
}

type ResolvedSegment struct {
	Number     uint64 `json:"number"`
	Time       uint64 `json:"time"`
	DurationMS int64  `json:"duration_ms"`
	URL        string `json:"url"`
	GapMS      int64  `json:"gap_ms,omitempty"`
}

// Raw XML decoding structs (ISO/IEC 23009-1).
type xmlMPD struct {
	XMLName                   xml.Name    `xml:"MPD"`
	Type                      string      `xml:"type,attr"`
	Profiles                  string      `xml:"profiles,attr"`
	MediaPresentationDuration string      `xml:"mediaPresentationDuration,attr"`
	MinBufferTime             string      `xml:"minBufferTime,attr"`
	MinimumUpdatePeriod       string      `xml:"minimumUpdatePeriod,attr"`
	AvailabilityStartTime     string      `xml:"availabilityStartTime,attr"`
	PublishTime               string      `xml:"publishTime,attr"`
	BaseURLs                  []string    `xml:"BaseURL"`
	Periods                   []xmlPeriod `xml:"Period"`
}

type xmlPeriod struct {
	ID             string             `xml:"id,attr"`
	Start          string             `xml:"start,attr"`
	Duration       string             `xml:"duration,attr"`
	BaseURLs       []string           `xml:"BaseURL"`
	AdaptationSets []xmlAdaptationSet `xml:"AdaptationSet"`
}

type xmlAdaptationSet struct {
	ID                 string                 `xml:"id,attr"`
	MimeType           string                 `xml:"mimeType,attr"`
	ContentType        string                 `xml:"contentType,attr"`
	Codecs             string                 `xml:"codecs,attr"`
	Lang               string                 `xml:"lang,attr"`
	SegmentAlignment   string                 `xml:"segmentAlignment,attr"`
	Width              int                    `xml:"width,attr"`
	Height             int                    `xml:"height,attr"`
	FrameRate          string                 `xml:"frameRate,attr"`
	BaseURLs           []string               `xml:"BaseURL"`
	ContentProtections []xmlContentProtection `xml:"ContentProtection"`
	SegmentTemplate    *xmlSegmentTemplate    `xml:"SegmentTemplate"`
	SegmentList        *xmlSegmentList        `xml:"SegmentList"`
	Representations    []xmlRepresentation    `xml:"Representation"`
}

type xmlContentProtection struct {
	SchemeIDURI string `xml:"schemeIdUri,attr"`
	Value       string `xml:"value,attr"`
}

type xmlRepresentation struct {
	ID                string              `xml:"id,attr"`
	Bandwidth         int64               `xml:"bandwidth,attr"`
	MimeType          string              `xml:"mimeType,attr"`
	Codecs            string              `xml:"codecs,attr"`
	Width             int                 `xml:"width,attr"`
	Height            int                 `xml:"height,attr"`
	FrameRate         string              `xml:"frameRate,attr"`
	AudioSamplingRate string              `xml:"audioSamplingRate,attr"`
	BaseURLs          []string            `xml:"BaseURL"`
	SegmentBase       *xmlSegmentBase     `xml:"SegmentBase"`
	SegmentTemplate   *xmlSegmentTemplate `xml:"SegmentTemplate"`
	SegmentList       *xmlSegmentList     `xml:"SegmentList"`
}

type xmlSegmentTemplate struct {
	Timescale       uint64              `xml:"timescale,attr"`
	Duration        uint64              `xml:"duration,attr"`
	StartNumber     uint64              `xml:"startNumber,attr"`
	Initialization  string              `xml:"initialization,attr"`
	Media           string              `xml:"media,attr"`
	SegmentTimeline *xmlSegmentTimeline `xml:"SegmentTimeline"`
}

type xmlSegmentTimeline struct {
	Entries []xmlS `xml:"S"`
}

type xmlS struct {
	T *uint64 `xml:"t,attr"`
	D uint64  `xml:"d,attr"`
	R int64   `xml:"r,attr"`
}

type xmlSegmentList struct {
	Timescale      uint64          `xml:"timescale,attr"`
	Duration       uint64          `xml:"duration,attr"`
	StartNumber    uint64          `xml:"startNumber,attr"`
	Initialization *xmlURLRef      `xml:"Initialization"`
	SegmentURLs    []xmlSegmentURL `xml:"SegmentURL"`
}

type xmlURLRef struct {
	SourceURL string `xml:"sourceURL,attr"`
	Range     string `xml:"range,attr"`
}

type xmlSegmentURL struct {
	Media string `xml:"media,attr"`
}

type xmlSegmentBase struct {
	Timescale      uint64     `xml:"timescale,attr"`
	IndexRange     string     `xml:"indexRange,attr"`
	Initialization *xmlURLRef `xml:"Initialization"`
}

// LooksLikeMPD checks whether a byte slice looks like an MPEG-DASH MPD XML document.
func LooksLikeMPD(data []byte) bool {
	head := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(head) == 0 || head[0] != '<' {
		return false
	}
	if len(head) > 2048 {
		head = head[:2048]
	}
	return bytes.Contains(head, []byte("<MPD")) || bytes.Contains(head, []byte(":MPD"))
}

// ParseMPD parses raw MPD XML bytes into a normalized MPD structure.
func ParseMPD(data []byte) (*MPD, error) {
	cleaned := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var raw xmlMPD
	if err := xml.Unmarshal(cleaned, &raw); err != nil {
		return nil, fmt.Errorf("解析 MPD XML 失败: %w", err)
	}
	if strings.ToLower(raw.XMLName.Local) != "mpd" {
		return nil, fmt.Errorf("不是有效的 MPEG-DASH MPD 文档（根节点 <%s>）", raw.XMLName.Local)
	}
	mpdType := strings.ToLower(strings.TrimSpace(raw.Type))
	if mpdType == "" {
		mpdType = "static"
	}
	out := &MPD{
		Type:                        mpdType,
		Profiles:                    strings.TrimSpace(raw.Profiles),
		MediaPresentationDuration:   strings.TrimSpace(raw.MediaPresentationDuration),
		MediaPresentationDurationMS: ParseISO8601DurationMS(raw.MediaPresentationDuration),
		MinBufferTime:               strings.TrimSpace(raw.MinBufferTime),
		MinBufferTimeMS:             ParseISO8601DurationMS(raw.MinBufferTime),
		MinimumUpdatePeriod:         strings.TrimSpace(raw.MinimumUpdatePeriod),
		MinimumUpdatePeriodMS:       ParseISO8601DurationMS(raw.MinimumUpdatePeriod),
		AvailabilityStartTime:       strings.TrimSpace(raw.AvailabilityStartTime),
		PublishTime:                 strings.TrimSpace(raw.PublishTime),
		BaseURLs:                    trimSlice(raw.BaseURLs),
	}

	for _, rp := range raw.Periods {
		p := Period{
			ID:         strings.TrimSpace(rp.ID),
			Start:      strings.TrimSpace(rp.Start),
			StartMS:    ParseISO8601DurationMS(rp.Start),
			Duration:   strings.TrimSpace(rp.Duration),
			DurationMS: ParseISO8601DurationMS(rp.Duration),
			BaseURLs:   trimSlice(rp.BaseURLs),
		}
		for _, ras := range rp.AdaptationSets {
			as := AdaptationSet{
				ID:               strings.TrimSpace(ras.ID),
				MimeType:         strings.TrimSpace(ras.MimeType),
				ContentType:      inferContentType(ras.ContentType, ras.MimeType, ras.Codecs),
				Codecs:           strings.TrimSpace(ras.Codecs),
				Lang:             strings.TrimSpace(ras.Lang),
				SegmentAlignment: strings.TrimSpace(ras.SegmentAlignment),
				Width:            ras.Width,
				Height:           ras.Height,
				FrameRate:        strings.TrimSpace(ras.FrameRate),
				BaseURLs:         trimSlice(ras.BaseURLs),
				SegmentTemplate:  convertSegmentTemplate(ras.SegmentTemplate),
				SegmentList:      convertSegmentList(ras.SegmentList),
			}
			for _, cp := range ras.ContentProtections {
				label := formatDRM(cp.SchemeIDURI, cp.Value)
				if label != "" {
					as.DRM = append(as.DRM, label)
				}
			}
			for _, rr := range ras.Representations {
				rep := Representation{
					ID:                strings.TrimSpace(rr.ID),
					Bandwidth:         rr.Bandwidth,
					MimeType:          firstNonEmpty(strings.TrimSpace(rr.MimeType), as.MimeType),
					Codecs:            firstNonEmpty(strings.TrimSpace(rr.Codecs), as.Codecs),
					Width:             firstNonZero(rr.Width, as.Width),
					Height:            firstNonZero(rr.Height, as.Height),
					FrameRate:         firstNonEmpty(strings.TrimSpace(rr.FrameRate), as.FrameRate),
					AudioSamplingRate: strings.TrimSpace(rr.AudioSamplingRate),
					BaseURLs:          trimSlice(rr.BaseURLs),
					SegmentBase:       convertSegmentBase(rr.SegmentBase),
					SegmentTemplate:   convertSegmentTemplate(rr.SegmentTemplate),
					SegmentList:       convertSegmentList(rr.SegmentList),
				}
				if as.ContentType == "" {
					as.ContentType = inferContentType("", rep.MimeType, rep.Codecs)
				}
				as.Representations = append(as.Representations, rep)
			}
			p.AdaptationSets = append(p.AdaptationSets, as)
		}
		out.Periods = append(out.Periods, p)
	}
	return out, nil
}

func convertSegmentTemplate(raw *xmlSegmentTemplate) *SegmentTemplate {
	if raw == nil {
		return nil
	}
	st := &SegmentTemplate{
		Timescale:      raw.Timescale,
		Duration:       raw.Duration,
		StartNumber:    raw.StartNumber,
		Initialization: strings.TrimSpace(raw.Initialization),
		Media:          strings.TrimSpace(raw.Media),
	}
	if raw.SegmentTimeline != nil {
		for _, s := range raw.SegmentTimeline.Entries {
			st.Timeline = append(st.Timeline, TimelineEntry{T: s.T, D: s.D, R: s.R})
		}
	}
	return st
}

func convertSegmentList(raw *xmlSegmentList) *SegmentList {
	if raw == nil {
		return nil
	}
	sl := &SegmentList{
		Timescale:   raw.Timescale,
		Duration:    raw.Duration,
		StartNumber: raw.StartNumber,
	}
	if raw.Initialization != nil {
		sl.InitializationURL = strings.TrimSpace(raw.Initialization.SourceURL)
	}
	for _, u := range raw.SegmentURLs {
		if m := strings.TrimSpace(u.Media); m != "" {
			sl.SegmentURLs = append(sl.SegmentURLs, m)
		}
	}
	return sl
}

func convertSegmentBase(raw *xmlSegmentBase) *SegmentBase {
	if raw == nil {
		return nil
	}
	sb := &SegmentBase{
		Timescale:  raw.Timescale,
		IndexRange: strings.TrimSpace(raw.IndexRange),
	}
	if raw.Initialization != nil {
		sb.InitializationRange = strings.TrimSpace(raw.Initialization.Range)
	}
	return sb
}

func mergeSegmentTemplate(parent, child *SegmentTemplate) *SegmentTemplate {
	if parent == nil && child == nil {
		return nil
	}
	out := &SegmentTemplate{}
	if parent != nil {
		*out = *parent
	}
	if child != nil {
		if child.Timescale > 0 {
			out.Timescale = child.Timescale
		}
		if child.Duration > 0 {
			out.Duration = child.Duration
		}
		if child.StartNumber > 0 {
			out.StartNumber = child.StartNumber
		}
		if child.Initialization != "" {
			out.Initialization = child.Initialization
		}
		if child.Media != "" {
			out.Media = child.Media
		}
		if len(child.Timeline) > 0 {
			out.Timeline = child.Timeline
		}
	}
	if out.Timescale == 0 {
		out.Timescale = 1
	}
	if out.StartNumber == 0 {
		out.StartNumber = 1
	}
	return out
}

func resolveBaseChain(docURL string, chains ...[]string) string {
	cur := strings.TrimSpace(docURL)
	for _, list := range chains {
		if len(list) == 0 || list[0] == "" {
			continue
		}
		cur = resolveRelativeURL(cur, list[0])
	}
	return cur
}

func resolveRelativeURL(baseStr, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return baseStr
	}
	if baseStr == "" {
		return ref
	}
	bu, err := url.Parse(baseStr)
	if err != nil {
		return ref
	}
	ru, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return bu.ResolveReference(ru).String()
}

var templateTokenRe = regexp.MustCompile(`\$(RepresentationID|Number|Time|Bandwidth)(%0(\d+)d)?\$`)

func expandTemplate(tmpl, repID string, bandwidth int64, number, timeVal uint64) string {
	replaced := strings.ReplaceAll(tmpl, "$$", "\x00")
	replaced = templateTokenRe.ReplaceAllStringFunc(replaced, func(match string) string {
		parts := templateTokenRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		width := 0
		if len(parts) >= 4 && parts[3] != "" {
			width, _ = strconv.Atoi(parts[3])
		}
		fmtUint := func(v uint64) string {
			if width > 0 {
				return fmt.Sprintf("%0*d", width, v)
			}
			return strconv.FormatUint(v, 10)
		}
		switch parts[1] {
		case "RepresentationID":
			return repID
		case "Bandwidth":
			if width > 0 {
				return fmt.Sprintf("%0*d", width, bandwidth)
			}
			return strconv.FormatInt(bandwidth, 10)
		case "Number":
			return fmtUint(number)
		case "Time":
			return fmtUint(timeVal)
		default:
			return match
		}
	})
	return strings.ReplaceAll(replaced, "\x00", "$")
}

// ResolveInitURL resolves the initialization segment URL for a representation (if any).
func ResolveInitURL(docURL string, mpd *MPD, p Period, as AdaptationSet, rep Representation) string {
	base := resolveBaseChain(docURL, mpd.BaseURLs, p.BaseURLs, as.BaseURLs, rep.BaseURLs)
	if st := mergeSegmentTemplate(as.SegmentTemplate, rep.SegmentTemplate); st != nil && st.Initialization != "" {
		return resolveRelativeURL(base, expandTemplate(st.Initialization, rep.ID, rep.Bandwidth, st.StartNumber, 0))
	}
	if rep.SegmentList != nil && rep.SegmentList.InitializationURL != "" {
		return resolveRelativeURL(base, rep.SegmentList.InitializationURL)
	}
	if as.SegmentList != nil && as.SegmentList.InitializationURL != "" {
		return resolveRelativeURL(base, as.SegmentList.InitializationURL)
	}
	return ""
}

// ExpandMediaSegments resolves up to `limit` media segments for a representation and returns
// both the resolved list and the count of detected SegmentTimeline discontinuities/gaps.
func ExpandMediaSegments(docURL string, mpd *MPD, p Period, as AdaptationSet, rep Representation, limit int) ([]ResolvedSegment, int) {
	if limit <= 0 {
		limit = 32
	}
	base := resolveBaseChain(docURL, mpd.BaseURLs, p.BaseURLs, as.BaseURLs, rep.BaseURLs)
	var out []ResolvedSegment
	gaps := 0

	if st := mergeSegmentTemplate(as.SegmentTemplate, rep.SegmentTemplate); st != nil && st.Media != "" {
		scale := st.Timescale
		if scale == 0 {
			scale = 1
		}
		if len(st.Timeline) > 0 {
			num := st.StartNumber
			var curT uint64
			hasPrev := false
			for _, entry := range st.Timeline {
				var gapMS int64
				if entry.T != nil {
					if hasPrev && *entry.T > curT {
						gapMS = int64((*entry.T - curT) * 1000 / scale)
						if gapMS > 50 {
							gaps++
						}
					}
					curT = *entry.T
				}
				reps := entry.R
				if reps < 0 {
					reps = 0
				}
				if reps > int64(limit) {
					reps = int64(limit)
				}
				for r := int64(0); r <= reps && len(out) < limit; r++ {
					durMS := int64(entry.D * 1000 / scale)
					segURL := resolveRelativeURL(base, expandTemplate(st.Media, rep.ID, rep.Bandwidth, num, curT))
					segGap := int64(0)
					if r == 0 {
						segGap = gapMS
					}
					out = append(out, ResolvedSegment{
						Number:     num,
						Time:       curT,
						DurationMS: durMS,
						URL:        segURL,
						GapMS:      segGap,
					})
					num++
					curT += entry.D
					hasPrev = true
				}
				if len(out) >= limit {
					break
				}
			}
			return out, gaps
		}

		// Fixed duration template
		if st.Duration > 0 {
			durMS := int64(st.Duration * 1000 / scale)
			count := limit
			totalMS := p.DurationMS
			if totalMS <= 0 {
				totalMS = mpd.MediaPresentationDurationMS
			}
			if totalMS > 0 && durMS > 0 {
				est := int((totalMS + durMS - 1) / durMS)
				if est > 0 && est < count {
					count = est
				}
			}
			for i := 0; i < count; i++ {
				num := st.StartNumber + uint64(i)
				tVal := uint64(i) * st.Duration
				segURL := resolveRelativeURL(base, expandTemplate(st.Media, rep.ID, rep.Bandwidth, num, tVal))
				out = append(out, ResolvedSegment{
					Number:     num,
					Time:       tVal,
					DurationMS: durMS,
					URL:        segURL,
				})
			}
			return out, 0
		}
	}

	// SegmentList fallback
	sl := rep.SegmentList
	if sl == nil {
		sl = as.SegmentList
	}
	if sl != nil && len(sl.SegmentURLs) > 0 {
		scale := sl.Timescale
		if scale == 0 {
			scale = 1
		}
		durMS := int64(0)
		if sl.Duration > 0 {
			durMS = int64(sl.Duration * 1000 / scale)
		}
		startNum := sl.StartNumber
		if startNum == 0 {
			startNum = 1
		}
		for i, u := range sl.SegmentURLs {
			if i >= limit {
				break
			}
			out = append(out, ResolvedSegment{
				Number:     startNum + uint64(i),
				DurationMS: durMS,
				URL:        resolveRelativeURL(base, u),
			})
		}
		return out, 0
	}

	// SegmentBase / single BaseURL media file fallback
	if base != "" && base != docURL {
		out = append(out, ResolvedSegment{
			Number:     1,
			DurationMS: firstNonZero64(p.DurationMS, mpd.MediaPresentationDurationMS),
			URL:        base,
		})
	}
	return out, 0
}

var isoDurRe = regexp.MustCompile(`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// ParseISO8601DurationMS parses an ISO 8601 duration (e.g. "PT1H2M3.5S") into milliseconds.
func ParseISO8601DurationMS(raw string) int64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	m := isoDurRe.FindStringSubmatch(s)
	if len(m) == 0 {
		return 0
	}
	var totalSec float64
	if m[3] != "" {
		d, _ := strconv.ParseFloat(m[3], 64)
		totalSec += d * 86400
	}
	if m[4] != "" {
		h, _ := strconv.ParseFloat(m[4], 64)
		totalSec += h * 3600
	}
	if m[5] != "" {
		mn, _ := strconv.ParseFloat(m[5], 64)
		totalSec += mn * 60
	}
	if m[6] != "" {
		sec, _ := strconv.ParseFloat(m[6], 64)
		totalSec += sec
	}
	return int64(totalSec*1000 + 0.5)
}

func inferContentType(contentType, mimeType, codecs string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct != "" {
		return ct
	}
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mt, "video/"):
		return "video"
	case strings.HasPrefix(mt, "audio/"):
		return "audio"
	case strings.HasPrefix(mt, "text/") || strings.Contains(mt, "ttml") || strings.Contains(mt, "vtt"):
		return "text"
	}
	c := strings.ToLower(strings.TrimSpace(codecs))
	switch {
	case strings.HasPrefix(c, "avc") || strings.HasPrefix(c, "hev") || strings.HasPrefix(c, "hvc") || strings.HasPrefix(c, "av01") || strings.HasPrefix(c, "vp"):
		return "video"
	case strings.HasPrefix(c, "mp4a") || strings.HasPrefix(c, "opus") || strings.HasPrefix(c, "ec-3") || strings.HasPrefix(c, "ac-3"):
		return "audio"
	}
	return ""
}

func formatDRM(schemeURI, value string) string {
	s := strings.ToLower(strings.TrimSpace(schemeURI))
	v := strings.TrimSpace(value)
	switch {
	case strings.Contains(s, "edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"):
		return "Widevine"
	case strings.Contains(s, "9a04f079-9840-4286-ab92-e65be0885f95"):
		return "PlayReady"
	case strings.Contains(s, "94ce86fb-07ff-4f43-adb8-93d2fa968ca2"):
		return "FairPlay"
	case strings.Contains(s, "1077efec-c0b2-4d02-ace3-3c1e52e2fb4b"):
		return "ClearKey"
	case strings.Contains(s, "mp4protection"):
		if v != "" {
			return "CENC (" + v + ")"
		}
		return "CENC"
	default:
		if v != "" {
			return v
		}
		return strings.TrimSpace(schemeURI)
	}
}

func trimSlice(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func firstNonZero64(a, b int64) int64 {
	if a != 0 {
		return a
	}
	return b
}
