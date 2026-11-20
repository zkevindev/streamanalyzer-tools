package diagnostic

import (
	"testing"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
)

func TestSelectHLSVariantAndAlignPlayerSwitches(t *testing.T) {
	playlist := &m3u8.Playlist{Variants: []m3u8.Variant{{URI: "low.m3u8", Bandwidth: 300000}, {URI: "high.m3u8", Bandwidth: 1200000}}}
	variant, index, ok := selectHLSVariant(playlist, nil)
	if !ok || index != 1 || variant.Bandwidth != 1200000 {
		t.Fatalf("default variant is not highest: %+v %d", variant, index)
	}
	requested := 0
	variant, index, ok = selectHLSVariant(playlist, &requested)
	if !ok || index != 0 || variant.Bandwidth != 300000 {
		t.Fatalf("explicit variant was not selected: %+v %d", variant, index)
	}
	level, width, height := 0, 640, 360
	bandwidth := int64(300000)
	report := Report{Manifest: Manifest{Protocol: ProtocolHLS, HLSVariant: &requested, HLSVariantBandwidth: 300000},
		Player:        &PlayerReport{StartOffsetMS: 500, Events: []PlayerEvent{{AtMS: 100, Kind: "level_switch", Level: &level, Bandwidth: &bandwidth, Width: &width, Height: &height}}},
		PlayerNetwork: &PlayerNetworkReport{Requests: []PlayerNetworkRequest{{Number: 7, StartMS: 700}}}}
	abr := buildHLSABR(report)
	if abr == nil || len(abr.Switches) != 1 || abr.Switches[0].AtMS != 600 || abr.Switches[0].NearestPlayerRequest != 7 ||
		abr.Switches[0].RequestDeltaMS != 100 || !abr.Switches[0].MatchesSampledVariant || abr.MatchedSwitches != 1 {
		t.Fatalf("incorrect ABR alignment: %+v", abr)
	}
	if _, err := validateStoredHLSABR(Report{HLSABR: abr}, report); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerLevelSwitchValidation(t *testing.T) {
	level, bandwidth := 1, int64(500000)
	record := PlayerRecording{Version: 1, StartedAt: time.Now(), Source: "independent_pull", Events: []PlayerEvent{{Kind: "level_switch", Level: &level, Bandwidth: &bandwidth}}}
	if err := validatePlayerRecording(record, time.Now()); err != nil {
		t.Fatal(err)
	}
	bad := int64(-1)
	record.Events[0].Bandwidth = &bad
	if err := validatePlayerRecording(record, time.Now()); err == nil {
		t.Fatal("accepted invalid ABR bandwidth")
	}
}
