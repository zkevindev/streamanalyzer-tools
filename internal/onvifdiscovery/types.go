// Package onvifdiscovery discovers local ONVIF devices and reads their media
// profiles. Discovery and selection state is temporary; credentials are never
// retained in a manager, returned in a list, or written to disk.
package onvifdiscovery

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

const (
	maxDevices    = 128
	maxProfiles   = 64
	maxJobs       = 8
	maxSelections = 32
	stateTTL      = 10 * time.Minute
)

type Error struct {
	Code       string `json:"code"`
	Stage      string `json:"stage,omitempty"`
	Message    string `json:"error"`
	HTTPStatus int    `json:"upstream_status,omitempty"`
}

func (e *Error) Error() string     { return e.Message }
func invalid(message string) error { return &Error{Code: "invalid_input", Message: message} }

type Interface struct {
	Name     string `json:"name"`
	Index    int    `json:"index"`
	IP       string `json:"ip"`
	Prefix   string `json:"prefix"`
	Loopback bool   `json:"loopback"`
}

type Device struct {
	ID         string   `json:"id"`
	EndpointID string   `json:"endpoint_id"`
	PeerIP     string   `json:"peer_ip"`
	Name       string   `json:"name,omitempty"`
	ServiceURL string   `json:"service_url"`
	Types      []string `json:"types"`
}

type Discovery struct {
	ID                   string    `json:"id"`
	State                string    `json:"state"`
	Interface            Interface `json:"interface"`
	Seconds              int       `json:"seconds"`
	StartedAt            time.Time `json:"started_at"`
	Devices              []Device  `json:"devices"`
	DroppedDevices       int       `json:"dropped_devices"`
	IgnoredResponses     int       `json:"ignored_responses"`
	ResponseLimitReached bool      `json:"response_limit_reached"`
	Error                *Error    `json:"failure,omitempty"`
}

type DiscoverRequest struct {
	BindIP  string `json:"bind_ip"`
	Seconds int    `json:"seconds"`
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ProfilesRequest struct {
	DiscoveryID string `json:"discovery_id,omitempty"`
	DeviceID    string `json:"device_id,omitempty"`
	ServiceURL  string `json:"service_url,omitempty"`
	BindIP      string `json:"bind_ip"`
	Credentials
}

type Profile struct {
	Token      string `json:"token"`
	Name       string `json:"name"`
	VideoCodec string `json:"video_codec,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	FrameRate  int    `json:"frame_rate,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`
}

type ProfilesResult struct {
	SelectionID        string    `json:"selection_id"`
	DeviceURL          string    `json:"device_url"`
	MediaURL           string    `json:"media_url"`
	PeerIP             string    `json:"peer_ip"`
	Profiles           []Profile `json:"profiles"`
	DroppedProfiles    int       `json:"dropped_profiles"`
	ClockStatus        string    `json:"clock_status"`
	ClockOffsetSeconds *int64    `json:"clock_offset_seconds,omitempty"`
}

type StreamRequest struct {
	SelectionID  string `json:"selection_id"`
	ProfileToken string `json:"profile_token"`
	Transport    string `json:"transport"`
	Credentials
}

type StreamResult struct {
	URL             string `json:"url"`
	Protocol        string `json:"protocol"`
	Transport       string `json:"transport"`
	RemovedUserInfo bool   `json:"removed_userinfo"`
	DifferentHost   bool   `json:"different_host"`
}

func identifier() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("crypto/rand unavailable")
	}
	return hex.EncodeToString(raw[:])
}
