package mediasource

import "time"

type RTMPFailoverStatus struct {
	BackupURL          string    `json:"backup_url"`
	StallMS            int       `json:"stall_ms"`
	BackupPublishedAt  time.Time `json:"backup_published_at,omitzero"`
	PrimaryLastMediaAt time.Time `json:"primary_last_media_at,omitzero"`
	StallDetectedAt    time.Time `json:"stall_detected_at,omitzero"`
	SwitchedAt         time.Time `json:"switched_at,omitzero"`
	GapMS              *int64    `json:"gap_ms,omitempty"`
	PrimaryConnected   bool      `json:"primary_connected"`
}

type rtmpFailover struct {
	report                   RTMPFailoverStatus
	standby                  *rtmpWire
	demoted                  *rtmpWire
	oldNumber                uint64
	backupAudio, backupVideo *relayPacket
}

// Called with the relay lock. A live standby observes the primary timeout
// independently of the primary connection's control traffic.
func (s *rtmpSource) standbyMedia(wire *rtmpWire, kind byte, stamp uint32, data []byte, now time.Time, config, key bool) bool {
	r := s.relay
	f := r.failover
	if config {
		packet := &relayPacket{media: kind, timestamp: stamp, payload: data}
		if kind == relayAudio {
			f.backupAudio = packet
		} else {
			f.backupVideo = packet
		}
		return false
	}
	primary := r.currentPublication()
	if r.publisher == nil || primary == nil || f.report.PrimaryLastMediaAt.IsZero() ||
		now.Sub(f.report.PrimaryLastMediaAt) < time.Duration(f.report.StallMS)*time.Millisecond {
		return false
	}
	if f.report.StallDetectedAt.IsZero() {
		f.report.StallDetectedAt = now
		s.status.LastStage = "publisher_stalled"
		s.status.FaultHits++
	}
	if !key || f.backupVideo == nil && (data[0]&0x80 != 0 || data[0]&0x0f == 7 || data[0]&0x0f == 12) {
		return false
	}
	f.report.SwitchedAt = now
	gap := now.Sub(f.report.PrimaryLastMediaAt).Milliseconds()
	f.report.GapMS = &gap
	f.oldNumber = primary.Number
	f.demoted, f.standby, r.publisher = r.publisher, nil, wire
	r.firstStamp, r.initAudio, r.initVideo = nil, nil, nil
	r.appendPublication(f.report.BackupPublishedAt)
	for _, config := range []*relayPacket{f.backupAudio, f.backupVideo} {
		if config != nil {
			s.forwardPacket(config.media, stamp, config.payload, now, true)
		}
	}
	f.backupAudio, f.backupVideo = nil, nil
	return true
}
