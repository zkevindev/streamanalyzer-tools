package mediasource

import (
	"slices"
	"time"
)

type RTMPPublisherSwitch struct {
	AudioBeforeMS  *uint32   `json:"audio_before_ms,omitempty"`
	AudioAfterMS   *uint32   `json:"audio_after_ms,omitempty"`
	To             string    `json:"to"`
	DecisionAt     time.Time `json:"decision_at"`
	KeyframeAt     time.Time `json:"keyframe_at"`
	KeyframeWaitMS int64     `json:"keyframe_wait_ms"`
	MediaGapMS     int64     `json:"media_gap_ms"`
}

type RTMPFailoverStatus struct {
	StallTrack         string                `json:"stall_track,omitempty"`
	PrimaryLastVideoAt time.Time             `json:"primary_last_video_at,omitzero"`
	PrimaryLastAudioAt time.Time             `json:"primary_last_audio_at,omitzero"`
	FailbackStableMS   int                   `json:"failback_stable_ms,omitempty"`
	ActivePublisher    string                `json:"active_publisher,omitempty"`
	RecoveryRestarts   uint64                `json:"recovery_restarts"`
	Switches           []RTMPPublisherSwitch `json:"switches,omitempty"`
	BackupURL          string                `json:"backup_url"`
	StallMS            int                   `json:"stall_ms"`
	BackupPublishedAt  time.Time             `json:"backup_published_at,omitzero"`
	PrimaryLastMediaAt time.Time             `json:"primary_last_media_at,omitzero"`
	StallDetectedAt    time.Time             `json:"stall_detected_at,omitzero"`
	SwitchedAt         time.Time             `json:"switched_at,omitzero"`
	GapMS              *int64                `json:"gap_ms,omitempty"`
	PrimaryConnected   bool                  `json:"primary_connected"`
}

type rtmpFailover struct {
	onBackup                          bool
	primaryAudio, primaryVideo        *relayPacket
	resumeStart, resumeLast           time.Time
	primaryPublished, activeLastMedia time.Time
	activeAudioStamp                  *uint32
	report                            RTMPFailoverStatus
	standby                           *rtmpWire
	demoted                           *rtmpWire
	oldNumber                         uint64
	backupAudio, backupVideo          *relayPacket
}

// Called with the relay lock. A live standby observes the primary timeout
// independently of the primary connection's control traffic.
func (s *rtmpSource) standbyMedia(wire *rtmpWire, kind byte, stamp uint32, data []byte, now time.Time, config, key bool) bool {
	r := s.relay
	f := r.failover
	if config {
		cacheRelayConfig(&f.backupAudio, &f.backupVideo, kind, stamp, data)
		return false
	}
	primary := r.currentPublication()
	last := f.report.PrimaryLastMediaAt
	if f.report.StallTrack == "video" {
		last = f.report.PrimaryLastVideoAt
	}
	if r.publisher == nil || primary == nil || last.IsZero() ||
		now.Sub(last) < time.Duration(f.report.StallMS)*time.Millisecond {
		return false
	}
	if f.report.StallDetectedAt.IsZero() {
		f.report.StallDetectedAt = now
		s.status.LastStage = "publisher_stalled"
		s.status.FaultHits++
	}
	if !switchReady(key, data, f.backupVideo) {
		return false
	}
	f.report.SwitchedAt = now
	gap := now.Sub(last).Milliseconds()
	f.report.GapMS = &gap
	f.oldNumber, f.primaryPublished = primary.Number, primary.PublishedAt
	f.primaryAudio, f.primaryVideo = r.initAudio, r.initVideo
	f.demoted, f.standby, f.onBackup = r.publisher, nil, true
	f.resumeStart, f.resumeLast = time.Time{}, time.Time{}
	f.recordSwitch("backup", f.report.StallDetectedAt, now)
	s.activatePublisher(wire, stamp, now, f.report.BackupPublishedAt, f.backupAudio, f.backupVideo)
	f.backupAudio, f.backupVideo = nil, nil
	return true
}

func cacheRelayConfig(audio, video **relayPacket, kind byte, stamp uint32, data []byte) {
	packet := &relayPacket{media: kind, timestamp: stamp, payload: data}
	if kind == relayAudio {
		*audio = packet
	} else {
		*video = packet
	}
}

func switchReady(key bool, data []byte, config *relayPacket) bool {
	return key && (config != nil || data[0]&0x80 == 0 && data[0]&0x0f != 7 && data[0]&0x0f != 12)
}

func (s *rtmpSource) recoverPrimary(wire *rtmpWire, kind byte, stamp uint32, data []byte, now time.Time, config, key bool) bool {
	r, f := s.relay, s.relay.failover
	if f.report.FailbackStableMS == 0 {
		return false
	}
	if config {
		cacheRelayConfig(&f.primaryAudio, &f.primaryVideo, kind, stamp, data)
		return false
	}
	if f.report.StallTrack == "video" && kind != relayVideo {
		return false
	}
	if f.resumeLast.IsZero() || now.Sub(f.resumeLast) >= time.Duration(f.report.StallMS)*time.Millisecond {
		if !f.resumeLast.IsZero() {
			f.report.RecoveryRestarts++
		}
		f.resumeStart = now
	}
	f.resumeLast = now
	decision := f.resumeStart.Add(time.Duration(f.report.FailbackStableMS) * time.Millisecond)
	if now.Before(decision) || !switchReady(key, data, f.primaryVideo) {
		return false
	}

	f.standby, f.demoted, f.onBackup = r.publisher, nil, false
	f.backupAudio, f.backupVideo = r.initAudio, r.initVideo
	f.report.StallDetectedAt = time.Time{}
	f.recordSwitch("primary", decision, now)
	s.activatePublisher(wire, stamp, now, f.primaryPublished, f.primaryAudio, f.primaryVideo)
	f.primaryAudio, f.primaryVideo = nil, nil
	return true
}

func (f *rtmpFailover) recordSwitch(to string, decision, now time.Time) {
	gap := int64(0)
	if !f.activeLastMedia.IsZero() {
		gap = now.Sub(f.activeLastMedia).Milliseconds()
	}
	if len(f.report.Switches) == 16 {
		f.report.Switches = slices.Delete(f.report.Switches, 0, 1)
	}
	f.report.Switches = append(f.report.Switches, RTMPPublisherSwitch{
		To: to, AudioBeforeMS: f.activeAudioStamp, DecisionAt: decision, KeyframeAt: now, KeyframeWaitMS: now.Sub(decision).Milliseconds(), MediaGapMS: gap,
	})
	f.activeAudioStamp = nil
}

func (s *rtmpSource) activatePublisher(wire *rtmpWire, stamp uint32, now, published time.Time, audio, video *relayPacket) {
	r := s.relay
	r.publisher, r.firstStamp, r.initAudio, r.initVideo = wire, nil, nil, nil
	r.appendPublication(published)
	for _, config := range []*relayPacket{audio, video} {
		if config != nil {
			s.forwardPacket(config.media, stamp, config.payload, now, true)
		}
	}
}

// The audio positions are on the outgoing relay time base, on the same
// playing connection. Configuration packets have no audio presentation time.
func (f *rtmpFailover) forwardedAudio(timestamp uint32) {
	f.activeAudioStamp = &timestamp
	if n := len(f.report.Switches); n > 0 && f.report.Switches[n-1].AudioAfterMS == nil {
		f.report.Switches[n-1].AudioAfterMS = &timestamp
	}
}
