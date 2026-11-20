package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSimulatorHistoryPersistsAndReturnsSummaries(t *testing.T) {
	dir := t.TempDir()
	open := func() *gin.Engine {
		r := gin.New()
		RegisterSimulatorHistoryRoutes(r, dir)
		return r
	}
	r := open()
	call := func(method, query, body string, status int, output any) {
		t.Helper()
		request := httptest.NewRequest(method, "http://127.0.0.1/api/simulators/runs"+query, strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:45456"
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s %s returned %d: %s", method, query, response.Code, response.Body)
		}
		if output != nil {
			if err := json.Unmarshal(response.Body.Bytes(), output); err != nil {
				t.Fatal(err)
			}
		}
	}
	const early = `{"preset":{"version":1,"name":"UDP 源端口","scenario":"rtsp-source_port"},"started_at":"2026-11-20T00:00:00.000Z","passed":true,"detail":"通过","elapsed_ms":95,"evidence":{"udp":{"id":"22222222222222222222222222222222"}}}`
	const later = `{"preset":{"version":1,"name":"HLS 逾时","scenario":"hls-delay"},"started_at":"2026-11-20T00:10:00.000Z","passed":false,"detail":"读取逾时","elapsed_ms":1000}`
	var first, again simulatorRunRecord
	call("POST", "", early, 200, &first)
	call("POST", "", later, 200, nil)
	call("POST", "", early, 200, &again)
	if first.ID == "" || first.ID != again.ID {
		t.Fatal("replaying a saved browser record must be idempotent")
	}
	call("POST", "", `{"preset":{"version":2}}`, 400, nil)

	r = open()
	var stored []simulatorRunRecord
	call("GET", "", "", 200, &stored)
	if len(stored) != 2 || stored[0].Preset.Name != "HLS 逾时" || stored[1].ID != first.ID || len(stored[1].Evidence) == 0 {
		t.Fatalf("unexpected stored records: %+v", stored)
	}
	var summaries []simulatorRunRecord
	call("GET", "?summary=1", "", 200, &summaries)
	if len(summaries) != 2 || summaries[1].ID != first.ID || len(summaries[1].Evidence) != 0 {
		t.Fatalf("unexpected summary records: %+v", summaries)
	}
}
