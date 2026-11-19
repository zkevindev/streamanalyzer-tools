package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"streamanalyzer/internal/hlsdelivery/media"
)

func networkProxyRequest(t *testing.T, player *PlayerSession, method, resource string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, "http://local.invalid/player/"+resource, nil)
	request.Header.Set("Range", "bytes=0-4")
	response := httptest.NewRecorder()
	player.ServeHLS(response, request, strings.Split(resource, "?")[0])
	return response
}

func TestPlayerNetworkProxyTracksRetriesKindsRedirectAndRangeWithoutURLs(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/live.m3u8?secret=redirect", http.StatusFound)
		case "/live.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-MAP:URI=\"/init.mp4?secret=init\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key?secret=key\"\n#EXTINF:1,\n/segment.ts?secret=media\n#EXT-X-ENDLIST\n")
		case "/segment.ts":
			if r.Header.Get("Range") != "bytes=0-4" {
				t.Error("range lost")
			}
			w.Header().Set("Content-Range", "bytes 0-4/20")
			w.WriteHeader(http.StatusPartialContent)
			io.WriteString(w, "12345")
		case "/init.mp4", "/key":
			io.WriteString(w, "resource")
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	target, _ := url.Parse(origin.URL + "/start?secret=source")
	player := newPlayerSession(t.Context(), ProtocolHLS, target, captureClient())
	defer player.Close()
	// A sampler/source fetch has no relationship to this playback ledger.
	response, err := http.Get(origin.URL + "/live.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(player.hls.network.entries) != 0 {
		t.Fatal("independent sampler fetch polluted playback evidence")
	}
	if got := networkProxyRequest(t, player, "GET", "index.m3u8?token=local-secret"); got.Code != 200 {
		t.Fatal(got.Code)
	}
	ids := map[string]string{}
	player.hls.mu.Lock()
	for id, resource := range player.hls.byID {
		ids[resource.kind] = id
	}
	player.hls.mu.Unlock()
	for _, kind := range []string{"init", "key", "media"} {
		if ids[kind] == "" {
			t.Fatalf("resource kind not discovered: %s", kind)
		}
		if got := networkProxyRequest(t, player, "GET", ids[kind]); got.Code != 200 && got.Code != 206 {
			t.Fatal(got.Code)
		}
	}
	networkProxyRequest(t, player, "GET", ids["media"])
	networkProxyRequest(t, player, "HEAD", ids["media"])
	value := player.hls.network.snapshot(1, false)
	if len(value.Requests) != 6 || value.Requests[0].HTTPStatus != 200 || value.Requests[3].HTTPStatus != 206 || value.Requests[3].Bytes != 5 || value.Requests[3].ResourceID != value.Requests[4].ResourceID || value.Requests[4].Number == value.Requests[3].Number {
		t.Fatalf("redirect/range/retry observations incorrect: %+v", value)
	}
	head := value.Requests[5]
	if head.FirstBodyMS != nil || head.Bytes != 0 || head.HTTPStatus != 206 {
		t.Fatalf("HEAD invented a body: %+v", head)
	}
	for _, request := range value.Requests {
		if request.HeaderMS == nil || request.Outcome != "complete" {
			t.Fatalf("missing upstream completion: %+v", request)
		}
	}
	data, _ := json.Marshal(value)
	for _, secret := range []string{"secret=", "local-secret", origin.URL, "Content-Range", "127.0.0.1"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("network evidence leaked source metadata: %s", data)
		}
	}
	if err := validatePlayerNetwork(value, &PlayerReport{Revision: 1}, ProtocolHLS, false); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerNetworkCanceledTimeoutHTTPAndTruncatedBody(t *testing.T) {
	for _, mode := range []string{"canceled", "timeout", "http_error", "transport_error"} {
		t.Run(mode, func(t *testing.T) {
			ready := make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http_error" {
					w.WriteHeader(503)
					return
				}
				if mode == "transport_error" {
					w.Header().Set("Content-Length", "100")
					io.WriteString(w, "short")
					return
				}
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(ready)
				<-r.Context().Done()
			}))
			defer origin.Close()
			target, _ := url.Parse(origin.URL + "/segment.ts")
			player := newPlayerSession(t.Context(), ProtocolHLS, target, captureClient())
			defer player.Close()
			resource := player.hls.remember(target, false)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 50*time.Millisecond)
				defer stop()
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				player.ServeHLS(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, "GET", "http://local/"+resource, nil), resource)
			}()
			if mode == "canceled" {
				select {
				case <-ready:
					deadline := time.Now().Add(time.Second)
					for {
						player.hls.network.mu.Lock()
						receiving := len(player.hls.network.entries) > 0 && player.hls.network.entries[0].receiveActive
						player.hls.network.mu.Unlock()
						if receiving {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("proxy did not begin observing the response body")
						}
						time.Sleep(time.Millisecond)
					}
					cancel()
				case <-time.After(time.Second):
					t.Fatal("upstream did not start")
				}
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("proxy did not finish")
			}
			value := player.hls.network.snapshot(1, false)
			if len(value.Requests) != 1 || value.Requests[0].Outcome != mode {
				t.Fatalf("unsafe or incorrect outcome: %+v", value)
			}
			request := value.Requests[0]
			if mode == "http_error" {
				if request.FailureStage != "response_headers" || request.FailureCode != "http_error" {
					t.Fatalf("HTTP status failure phase incorrect: %+v", request)
				}
			} else if request.FailureCode != mode || (request.HeaderMS != nil && request.FailureStage != "receive") {
				t.Fatalf("body read failure was confused with setup or downstream write: %+v", request)
			}
			if mode == "transport_error" && (request.Bytes != 5 || request.FirstBodyMS == nil) {
				t.Fatalf("truncated response lost upstream partial bytes: %+v", request)
			}
			if mode != "transport_error" && request.FirstBodyMS != nil {
				t.Fatalf("no body bytes were observed: %+v", request)
			}
		})
	}
}

func TestPlayerNetworkSnapshotsFreezeInflightAndDistinguishZeroFromUnknown(t *testing.T) {
	n := newPlayerNetwork()
	n.setOrigin(time.Now().Add(time.Hour)) // Clamp to zero to exercise measured timestamp zero deterministically.
	first := n.begin("r1.ts", "media", "GET")
	n.header(first, 200)
	n.body(first, 1)
	n.finish(first, "complete")
	n.begin("r2.ts", "media", "GET")
	partial := n.snapshot(1, false)
	if partial.Requests[0].FirstBodyMS == nil || *partial.Requests[0].FirstBodyMS != 0 || partial.Requests[1].FirstBodyMS != nil || partial.Requests[1].Outcome != "incomplete" {
		t.Fatalf("unknown and measured zero conflated: %+v", partial)
	}
	third := n.begin("r3.ts", "media", "GET")
	if !reflect.DeepEqual(partial, n.snapshot(1, false)) {
		t.Fatal("retry of the same player revision changed network evidence")
	}
	final := n.snapshot(2, true)
	n.header(third, 206)
	n.body(third, 20)
	n.finish(third, "complete")
	if n.begin("r4.ts", "media", "GET") != nil || !reflect.DeepEqual(final, n.snapshot(3, true)) {
		t.Fatal("final snapshot was not immutable")
	}
	if len(final.Requests) != 3 || final.Requests[2].Outcome != "incomplete" {
		t.Fatalf("final snapshot hid an in-flight request: %+v", final)
	}
	if err := validatePlayerNetwork(final, &PlayerReport{Revision: 2}, ProtocolHLS, true); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerNetworkConcurrentBoundedCaptureAndSnapshots(t *testing.T) {
	n := newPlayerNetwork()
	var wait sync.WaitGroup
	for index := 0; index < 1024; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			entry := n.begin("r1.ts", "media", "GET")
			n.header(entry, 200)
			n.body(entry, 1)
			n.finish(entry, "complete")
		}()
	}
	for revision := 1; revision <= 10; revision++ {
		n.snapshot(revision, false)
	}
	wait.Wait()
	value := n.snapshot(11, true)
	if len(value.Requests) != maxPlayerNetworkRequests || value.DroppedRequests != 1024-maxPlayerNetworkRequests {
		t.Fatalf("request ledger exceeded bound or lost truncation: %+v", value)
	}
	if err := validatePlayerNetwork(value, &PlayerReport{Revision: 11}, ProtocolHLS, true); err != nil {
		t.Fatal(err)
	}
}

func networkServiceFixture(t *testing.T) (*Service, string, string, *httptest.Server, PlayerRecording) {
	t.Helper()
	data := media.SyntheticMPEGTS(true)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n/a.ts\n#EXT-X-ENDLIST\n")
			return
		}
		w.Write(data)
	}))
	t.Cleanup(origin.Close)
	base := t.TempDir()
	service, err := Open(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	target, _ := url.Parse(origin.URL + "/live.m3u8?private=source")
	run := newRun("sample", ProtocolHLS, origin.URL)
	run.Status, run.ReportReady = "done", true
	dir := service.runDir(run.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	report, err := sampleHLS(t.Context(), service.client, dir, target, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	if err := service.insertRun(run); err != nil {
		t.Fatal(err)
	}
	player := newPlayerSession(context.Background(), ProtocolHLS, target, service.client)
	player.hls.network.setOrigin(run.CreatedAt)
	service.players[run.ID] = &playerSessionState{session: player, token: "network-test-token", created: run.CreatedAt}
	record := PlayerRecording{Version: 1, Revision: 1, Source: "independent_pull", Engine: "native-hls", StartedAt: run.CreatedAt, ElapsedMS: 100, Events: []PlayerEvent{{Kind: "start"}}, Samples: []PlayerSample{}}
	return service, run.ID, base, origin, record
}

func TestPlayerNetworkSaveRetryAtomicDownloadOfflineImportAndRestart(t *testing.T) {
	service, id, base, origin, record := networkServiceFixture(t)
	player, _ := service.PlayerSession(id, "network-test-token")
	networkProxyRequest(t, player, "GET", "index.m3u8")
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	first, _ := service.Detail(id)
	networkProxyRequest(t, player, "GET", "r1.ts")
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	retry, _ := service.Detail(id)
	if !reflect.DeepEqual(first.Report.PlayerNetwork, retry.Report.PlayerNetwork) || first.Report.Manifest.PlayerNetwork.SHA256 != retry.Report.Manifest.PlayerNetwork.SHA256 {
		t.Fatal("same recording revision changed its network artifact")
	}
	record.Revision, record.ElapsedMS = 2, 200
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	second, _ := service.Detail(id)
	w := &blockingPlayerDownload{started: make(chan struct{}), resume: make(chan struct{})}
	download := make(chan error, 1)
	go func() { download <- service.WriteBundle(id, w) }()
	select {
	case <-w.started:
	case <-time.After(time.Second):
		close(w.resume)
		t.Fatal("download did not begin")
	}
	networkProxyRequest(t, player, "GET", "r1.ts")
	record.Revision, record.ElapsedMS, record.Complete = 3, 300, true
	saved := make(chan error, 1)
	go func() { _, err := service.SavePlayerRecording(id, "network-test-token", record); saved <- err }()
	select {
	case err := <-saved:
		if err != nil {
			close(w.resume)
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(w.resume)
		t.Fatal("slow ZIP download blocked recording")
	}
	close(w.resume)
	if err := <-download; err != nil {
		t.Fatal(err)
	}
	final, _ := service.Detail(id)
	if len(final.Report.PlayerNetwork.Requests) != 3 || !final.Report.PlayerNetwork.Complete || final.Report.Manifest.Version != playerNetworkSchemaVersion {
		t.Fatalf("final snapshot incomplete: %+v", final.Report.PlayerNetwork)
	}
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	origin.Close() // Import must not consult the source or a model.
	imported, err := service.Import(bytes.NewReader(w.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := service.Detail(imported.ID)
	if !reflect.DeepEqual(replay.Report.PlayerNetwork, second.Report.PlayerNetwork) || replay.Report.Player.Revision != 2 {
		t.Fatal("download mixed network and player revisions")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Detail(id)
	if err != nil || !reflect.DeepEqual(after.Report.PlayerNetwork, final.Report.PlayerNetwork) {
		t.Fatalf("restart lost frozen proxy evidence: %v", err)
	}
}

func TestPlayerNetworkDelayedCaptureOriginMatchesPlayerClockAndKeepsRetryStable(t *testing.T) {
	service, id, _, _, record := networkServiceFixture(t)
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	report := *detail.Report
	report.Manifest.Observation.StartedAt = record.StartedAt.Add(time.Second)
	if err := persistReport(service.runDir(id), report); err != nil {
		t.Fatal(err)
	}
	player, _ := service.PlayerSession(id, "network-test-token")
	// Advance the test's monotonic clock without sleeping. The observation
	// still begins exactly 1000 ms after the canonical run clock.
	player.hls.network.mu.Lock()
	player.hls.network.origin = player.hls.network.origin.Add(-1500 * time.Millisecond)
	player.hls.network.mu.Unlock()
	networkProxyRequest(t, player, "GET", "index.m3u8")
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	player.hls.network.mu.Lock()
	raw := clonePlayerNetwork(player.hls.network.last)
	player.hls.network.mu.Unlock()
	detail, err = service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	actual := detail.Report.PlayerNetwork.Requests[0]
	if actual.StartMS != raw.Requests[0].StartMS-1000 || actual.EndMS != raw.Requests[0].EndMS-1000 || *actual.HeaderMS != *raw.Requests[0].HeaderMS-1000 || *actual.FirstBodyMS != *raw.Requests[0].FirstBodyMS-1000 || detail.Report.Player.StartOffsetMS != -1000 {
		t.Fatalf("network and player evidence use different capture clocks: raw=%+v actual=%+v player=%+v", raw.Requests[0], actual, detail.Report.Player)
	}
	artifact := *detail.Report.Manifest.PlayerNetwork
	networkProxyRequest(t, player, "GET", "r1.ts")
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	retry, _ := service.Detail(id)
	if *retry.Report.Manifest.PlayerNetwork != artifact || !reflect.DeepEqual(retry.Report.PlayerNetwork, detail.Report.PlayerNetwork) {
		t.Fatal("clock translation changed a same-revision network snapshot")
	}
	// An actual proxy request before capture starts has a bounded negative
	// offset; zero would imply a different observation time.
	raw.Requests[0].StartMS, raw.Requests[0].EndMS = 100, 200
	raw.Requests[0].Hops = nil // This fixture isolates top-level negative offsets.
	raw.Requests[0].HeaderMS, raw.Requests[0].FirstBodyMS = assessmentNumber(110), assessmentNumber(120)
	if err := persistPlayerNetwork(service.runDir(id), &report, raw, record.StartedAt); err != nil {
		t.Fatal(err)
	}
	if report.PlayerNetwork.Requests[0].StartMS != -900 || report.PlayerNetwork.Requests[0].EndMS != -800 {
		t.Fatal("pre-capture network offsets were clamped or not translated")
	}
}

func networkRewriteBundle(t *testing.T, data []byte, mutate func(map[string][]byte)) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name] = content
	}
	mutate(files)
	var result bytes.Buffer
	writer := zip.NewWriter(&result)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestPlayerNetworkImportValidatesCanonicalArtifactAndRebuildsSummary(t *testing.T) {
	service, id, _, _, record := networkServiceFixture(t)
	player, _ := service.PlayerSession(id, "network-test-token")
	networkProxyRequest(t, player, "GET", "index.m3u8")
	record.Complete = true
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*PlayerNetworkReport)
	}{
		{"boundary", func(value *PlayerNetworkReport) { value.Boundary = "browser_received" }},
		{"revision", func(value *PlayerNetworkReport) { value.PlayerRevision++ }},
		{"incomplete state", func(value *PlayerNetworkReport) { value.Complete = false }},
		{"private URL", func(value *PlayerNetworkReport) {
			value.Requests[0].ResourceID = "https://private.invalid?token=secret"
		}},
		{"unsupported outcome", func(value *PlayerNetworkReport) { value.Requests[0].Outcome = "specific secret error" }},
		{"invalid clock", func(value *PlayerNetworkReport) { value.Requests[0].EndMS = -60001 }},
		{"unknown byte time", func(value *PlayerNetworkReport) { value.Requests[0].FirstBodyMS = nil }},
		{"over limit", func(value *PlayerNetworkReport) {
			for len(value.Requests) <= maxPlayerNetworkRequests {
				value.Requests = append(value.Requests, value.Requests[0])
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			modified := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
				var value PlayerNetworkReport
				if err := json.Unmarshal(files[playerNetworkName], &value); err != nil {
					t.Fatal(err)
				}
				test.mutate(&value)
				files[playerNetworkName], _ = json.Marshal(value)
				var manifest Manifest
				json.Unmarshal(files[manifestName], &manifest)
				hash := sha256.Sum256(files[playerNetworkName])
				manifest.PlayerNetwork = &Artifact{Bytes: int64(len(files[playerNetworkName])), SHA256: hex.EncodeToString(hash[:])}
				files[manifestName], _ = json.Marshal(manifest)
			})
			if _, err := service.Import(bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("semantically invalid network evidence with a valid checksum accepted: %v", err)
			}
		})
	}
	modified := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
		files[playerNetworkName] = bytes.Replace(files[playerNetworkName], []byte("upstream_proxy"), []byte("upstream_fake!"), 1)
	})
	if _, err := service.Import(bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed artifact digest accepted: %v", err)
	}
	modified = networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
		files[reportName] = []byte(`{"player_network":{"boundary":"fake-browser-proof"}}`)
	})
	imported, err := service.Import(bytes.NewReader(modified))
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := service.Detail(imported.ID)
	if replay.Report.PlayerNetwork == nil || replay.Report.PlayerNetwork.Boundary != "upstream_proxy" {
		t.Fatal("import trusted report JSON instead of rebuilding from verified canonical sidecar")
	}
	legacy := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
		delete(files, playerNetworkName)
		var manifest Manifest
		json.Unmarshal(files[manifestName], &manifest)
		manifest.PlayerNetwork = nil
		manifest.Version = playerSchemaVersion
		files[manifestName], _ = json.Marshal(manifest)
	})
	legacyRun, err := service.Import(bytes.NewReader(legacy))
	if err != nil {
		t.Fatal(err)
	}
	legacyDetail, _ := service.Detail(legacyRun.ID)
	if legacyDetail.Report.PlayerNetwork != nil || slices.ContainsFunc(legacyDetail.Report.Findings, func(f Finding) bool { return strings.Contains(f.Code, "player_request") }) {
		t.Fatal("legacy evidence was converted into invented actual-playback network observations")
	}
	// The immutable canonical sidecar survives separately from exported names.
	entries, _ := os.ReadDir(service.runDir(id))
	if !slices.ContainsFunc(entries, func(entry os.DirEntry) bool {
		return strings.HasPrefix(entry.Name(), "player-network-") && filepath.Ext(entry.Name()) == ".json"
	}) {
		t.Fatal("hash-addressed network artifact missing")
	}
}
