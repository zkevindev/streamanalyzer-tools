package onvif

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	recordingNS      = "http://www.onvif.org/ver10/recording/wsdl"
	searchNS         = "http://www.onvif.org/ver10/search/wsdl"
	replayNS         = "http://www.onvif.org/ver10/replay/wsdl"
	recordingPath    = "/onvif/recording_service"
	searchPath       = "/onvif/search_service"
	replayPath       = "/onvif/replay_service"
	recordedRTSPPath = "/recording/rec-1"
	recordingToken   = "rec-1"
	replayTrackToken = "rec-1-video"
	recordingCaps    = `<trc:Capabilities DynamicRecordings="false" DynamicTracks="false" MaxRecordings="1"/>`
	searchCaps       = `<tse:Capabilities MetadataSearch="false"/>`
	replayCaps       = `<trp:Capabilities ReversePlayback="false" RTP_RTSP_TCP="true"/>`
)

var ErrRecordingUnavailable = errors.New("请先启用 ONVIF 录像场景")

type RecordingFixtureStatus struct {
	Token          string    `json:"token"`
	RecordingURL   string    `json:"recording_url"`
	SearchURL      string    `json:"search_url"`
	ReplayURL      string    `json:"replay_url"`
	StreamURL      string    `json:"stream_url"`
	From           time.Time `json:"from"`
	Until          time.Time `json:"until"`
	RecordedMotion time.Time `json:"recorded_motion"`
	MissingMotion  time.Time `json:"missing_motion"`
	Plays          uint64    `json:"plays"`
	MissingRange   uint64    `json:"missing_range"`
}

type fixtureSearch struct {
	expiry   time.Time
	lease    time.Duration
	include  bool
	reported bool
}

type recordingFixture struct {
	info     RecordingFixtureStatus
	mu       sync.Mutex
	searches map[string]fixtureSearch
	plays    atomic.Uint64
	missing  atomic.Uint64
}

func newRecordingFixture(clockOffset int) *recordingFixture {
	anchor := time.Now().UTC().Truncate(time.Second).Add(time.Duration(clockOffset) * time.Second)
	from := anchor.Add(-2 * time.Minute)
	return &recordingFixture{info: RecordingFixtureStatus{Token: recordingToken, From: from, Until: from.Add(30 * time.Second),
		RecordedMotion: from.Add(10 * time.Second), MissingMotion: from.Add(50 * time.Second)}, searches: make(map[string]fixtureSearch)}
}

func (f *recordingFixture) snapshot() *RecordingFixtureStatus {
	status := f.info
	status.Plays, status.MissingRange = f.plays.Load(), f.missing.Load()
	return &status
}

func (m *Manager) NotifyRecordingMotion(recorded bool) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.camera
	if c == nil || c.recording == nil {
		return Status{}, ErrRecordingUnavailable
	}
	at := c.recording.info.MissingMotion
	if recorded {
		at = c.recording.info.RecordedMotion
	}
	b := &c.events
	b.mu.Lock()
	b.prune(time.Now())
	for _, subscription := range b.points {
		b.enqueue(subscription, motionEvent{at: at, active: true, operation: "Changed"})
		b.enqueue(subscription, motionEvent{at: at.Add(time.Second), active: false, operation: "Changed"})
	}
	b.status.Changes += 2
	b.mu.Unlock()
	return c.snapshot(), nil
}

func recordingSource() string {
	return "<tt:Source><tt:SourceId>source-1</tt:SourceId><tt:Name>Main Stream</tt:Name><tt:Location>Lab</tt:Location>" +
		"<tt:Description>ONVIF replay fixture</tt:Description><tt:Address>urn:streamanalyzer:source-1</tt:Address></tt:Source>"
}

func (f *recordingFixture) information() string {
	from, until := f.info.From.Format(time.RFC3339), f.info.Until.Format(time.RFC3339)
	return "<tt:RecordingToken>" + recordingToken + "</tt:RecordingToken>" + recordingSource() +
		"<tt:EarliestRecording>" + from + "</tt:EarliestRecording><tt:LatestRecording>" + until + "</tt:LatestRecording><tt:Content>Motion fixture</tt:Content>" +
		"<tt:Track><tt:TrackToken>" + replayTrackToken + "</tt:TrackToken><tt:TrackType>Video</tt:TrackType><tt:Description>H.264</tt:Description>" +
		"<tt:DataFrom>" + from + "</tt:DataFrom><tt:DataTo>" + until + "</tt:DataTo></tt:Track><tt:RecordingStatus>Stopped</tt:RecordingStatus>"
}

func (c *camera) recordingReply(op operation) (string, *soapFault) {
	switch op.XMLName.Local {
	case "GetServiceCapabilities":
		return recordingCaps, nil
	case "GetRecordings":
		return "<trc:RecordingItem><tt:RecordingToken>" + recordingToken + "</tt:RecordingToken><tt:Configuration>" + recordingSource() +
			"<tt:Content>Motion fixture</tt:Content><tt:MaximumRetentionTime>PT2M</tt:MaximumRetentionTime></tt:Configuration>" +
			"<tt:Tracks><tt:Track><tt:TrackToken>" + replayTrackToken + "</tt:TrackToken><tt:Configuration><tt:TrackType>Video</tt:TrackType><tt:Description>H.264</tt:Description></tt:Configuration></tt:Track></tt:Tracks></trc:RecordingItem>", nil
	default:
		return "", unsupportedAction()
	}
}

func (c *camera) searchReply(op operation) (string, *soapFault) {
	f := c.recording
	switch op.XMLName.Local {
	case "GetServiceCapabilities":
		return searchCaps, nil
	case "GetRecordingSummary":
		return fmt.Sprintf("<tse:Summary><tt:DataFrom>%s</tt:DataFrom><tt:DataUntil>%s</tt:DataUntil><tt:NumberRecordings>1</tt:NumberRecordings></tse:Summary>",
			f.info.From.Format(time.RFC3339), f.info.Until.Format(time.RFC3339)), nil
	case "GetRecordingInformation":
		if op.RecordingToken != recordingToken {
			return "", invalidToken("Unknown recording token")
		}
		return "<tse:RecordingInformation>" + f.information() + "</tse:RecordingInformation>", nil
	case "FindRecordings", "GetRecordingSearchResults", "EndSearch":
		return f.search(op)
	default:
		return "", unsupportedAction()
	}
}

func (f *recordingFixture) search(op operation) (string, *soapFault) {
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	for token, search := range f.searches {
		if !now.Before(search.expiry) {
			delete(f.searches, token)
		}
	}
	if op.XMLName.Local == "FindRecordings" {
		lease, ok := eventLease(op.KeepAliveTime, now)
		if !ok || len(f.searches) >= 16 {
			return "", invalidToken("Invalid or unavailable recording search")
		}
		token := uuid.NewString()
		include := len(op.Scope.IncludedRecordings) == 0
		for _, requested := range op.Scope.IncludedRecordings {
			include = include || strings.TrimSpace(requested) == recordingToken
		}
		f.searches[token] = fixtureSearch{lease: min(lease, time.Minute), expiry: now.Add(min(lease, time.Minute)), include: include}
		return "<tse:SearchToken>" + token + "</tse:SearchToken>", nil
	}
	job, ok := f.searches[op.SearchToken]
	if !ok {
		return "", invalidToken("Unknown recording search token")
	}
	if op.XMLName.Local == "EndSearch" {
		delete(f.searches, op.SearchToken)
		return "<tse:Endpoint>" + f.info.Until.Format(time.RFC3339) + "</tse:Endpoint>", nil
	}
	body := "<tse:ResultList><tt:SearchState>Completed</tt:SearchState>"
	if job.include && !job.reported {
		body += "<tt:RecordingInformation>" + f.information() + "</tt:RecordingInformation>"
	}
	job.reported, job.expiry = true, now.Add(job.lease)
	f.searches[op.SearchToken] = job
	return body + "</tse:ResultList>", nil
}

func (c *camera) replayReply(op operation) (string, *soapFault) {
	switch op.XMLName.Local {
	case "GetServiceCapabilities":
		return replayCaps, nil
	case "GetReplayUri":
		if op.RecordingToken != recordingToken {
			return "", invalidToken("Unknown recording token")
		}
		if op.StreamSetup.Stream == "RTP-Multicast" || op.StreamSetup.Transport.Tunnel != nil {
			return "", invalidToken("Recording replay requires RTSP unicast")
		}
		return "<trp:Uri>" + c.recording.info.StreamURL + "</trp:Uri>", nil
	default:
		return "", unsupportedAction()
	}
}
