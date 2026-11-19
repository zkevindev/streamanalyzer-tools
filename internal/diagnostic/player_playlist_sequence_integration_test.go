package diagnostic

import (
	"encoding/json"
	"testing"
)

func TestPlaylistSequenceSnapshotAndModelPreserveZeroWithoutAliasing(t *testing.T) {
	n := newPlayerNetwork()
	entry := n.begin("index.m3u8", "playlist", "GET")
	n.header(entry, 200)
	n.body(entry, 1)
	n.playlistSequence(entry, 0)
	n.finish(entry, "complete")
	value := n.snapshot(1, true)
	if value.Requests[0].PlaylistSequence == nil || *value.Requests[0].PlaylistSequence != 0 {
		t.Fatal("observed zero playlist sequence lost")
	}
	*value.Requests[0].PlaylistSequence = 99
	if got := n.snapshot(1, true); *got.Requests[0].PlaylistSequence != 0 {
		t.Fatal("returned sequence pointer changed frozen network snapshot")
	}
	input, err := modelInput(Report{PlayerNetwork: n.snapshot(1, true)})
	if err != nil {
		t.Fatal(err)
	}
	var model struct {
		Network struct {
			Requests []struct {
				Sequence *uint64 `json:"playlist_sequence"`
			} `json:"requests"`
		} `json:"player_network"`
	}
	if err := json.Unmarshal(input, &model); err != nil || len(model.Network.Requests) != 1 || model.Network.Requests[0].Sequence == nil || *model.Network.Requests[0].Sequence != 0 {
		t.Fatal("numeric model projection omitted observed playlist sequence")
	}
	if validLegacyPlayerNetworkJSON([]byte(`{"version":1,"requests":[{"playlist_sequence":null}]}`), 1) {
		t.Fatal("new sequence field silently entered a legacy ledger")
	}
}
