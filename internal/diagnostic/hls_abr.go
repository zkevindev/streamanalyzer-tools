package diagnostic

import (
	"fmt"
	"reflect"
)

type HLSABRSwitch struct {
	AtMS                  int64 `json:"at_ms"`
	Level                 int   `json:"level"`
	Bandwidth             int64 `json:"bandwidth"`
	Width                 int   `json:"width,omitempty"`
	Height                int   `json:"height,omitempty"`
	NearestPlayerRequest  int   `json:"nearest_player_request,omitempty"`
	RequestDeltaMS        int64 `json:"request_delta_ms,omitempty"`
	MatchesSampledVariant bool  `json:"matches_sampled_variant"`
}

type HLSABRReport struct {
	Version          string         `json:"version"`
	SampledVariant   *int           `json:"sampled_variant,omitempty"`
	SampledBandwidth int            `json:"sampled_bandwidth,omitempty"`
	Switches         []HLSABRSwitch `json:"switches"`
	MatchedSwitches  int            `json:"matched_switches"`
	Boundary         string         `json:"boundary"`
}

func buildHLSABR(report Report) *HLSABRReport {
	if report.Manifest.Protocol != ProtocolHLS {
		return nil
	}
	result := &HLSABRReport{Version: "hls-abr/1", SampledVariant: report.Manifest.HLSVariant,
		SampledBandwidth: report.Manifest.HLSVariantBandwidth, Switches: []HLSABRSwitch{},
		Boundary: "hls.js 档位切换事件换算到后端采样时间轴，并关联最近的本地代理请求；请求关联表示时间接近，不证明单个分片导致切换。原生 HLS 不提供统一档位事件。"}
	if report.Player != nil {
		for _, event := range report.Player.Events {
			if event.Kind != "level_switch" || event.Level == nil || event.Bandwidth == nil {
				continue
			}
			item := HLSABRSwitch{AtMS: event.AtMS + report.Player.StartOffsetMS, Level: *event.Level, Bandwidth: *event.Bandwidth,
				MatchesSampledVariant: report.Manifest.HLSVariantBandwidth > 0 && *event.Bandwidth == int64(report.Manifest.HLSVariantBandwidth)}
			if event.Width != nil {
				item.Width = *event.Width
			}
			if event.Height != nil {
				item.Height = *event.Height
			}
			if item.MatchesSampledVariant {
				result.MatchedSwitches++
			}
			var nearest *PlayerNetworkRequest
			if report.PlayerNetwork != nil {
				for index := range report.PlayerNetwork.Requests {
					request := &report.PlayerNetwork.Requests[index]
					delta := abs64(request.StartMS - item.AtMS)
					if delta > 2000 {
						continue
					}
					if nearest == nil || delta < abs64(nearest.StartMS-item.AtMS) {
						nearest = request
					}
				}
			}
			if nearest != nil {
				item.NearestPlayerRequest = nearest.Number
				item.RequestDeltaMS = nearest.StartMS - item.AtMS
			}
			result.Switches = append(result.Switches, item)
		}
	}
	if result.SampledVariant == nil && len(result.Switches) == 0 {
		return nil
	}
	return result
}

func validateStoredHLSABR(stored, replayed Report) (*HLSABRReport, error) {
	if stored.HLSABR == nil {
		return nil, nil
	}
	expected := buildHLSABR(replayed)
	if expected == nil || !reflect.DeepEqual(stored.HLSABR, expected) {
		return nil, fmt.Errorf("%w: HLS ABR 对齐报告与播放器事件不一致", ErrInvalid)
	}
	return stored.HLSABR, nil
}
