package appserver

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPusherSimulatorPageAndWHIPProxy(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	base := "http://" + app.addr

	// 1. Page renders and nav links /simulators/pusher
	page := diagnosticRequest(t, app, "GET", "/simulators/pusher", http.StatusOK, nil)
	if !strings.Contains(string(page), `href="/simulators/pusher"`) {
		t.Fatalf("expected nav link to /simulators/pusher in page")
	}

	// 2. Empty tasks list
	raw := diagnosticRequest(t, app, "GET", "/api/simulators/pusher/tasks", http.StatusOK, nil)
	var listResp struct {
		Tasks    []any `json:"tasks"`
		MaxTasks int   `json:"max_tasks"`
	}
	if err := json.Unmarshal(raw, &listResp); err != nil {
		t.Fatalf("decode tasks list failed: %v", err)
	}
	if listResp.MaxTasks <= 0 {
		t.Fatalf("expected positive max_tasks, got %+v", listResp)
	}

	// 3. WHIP proxy POST + DELETE
	var upstreamDeleted bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/whip/res/99")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("v=0\r\ns=mock-answer\r\n"))
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/whip/res/99" {
			upstreamDeleted = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer upstream.Close()

	postBody, _ := json.Marshal(map[string]string{
		"target_url":   upstream.URL + "/whip",
		"bearer_token": "tok",
		"offer_sdp":    "v=0\r\ns=mock-offer\r\n",
	})
	rawProxy := diagnosticRequest(t, app, "POST", "/api/simulators/pusher/whip-proxy", http.StatusOK, postBody)
	var proxyResp struct {
		AnswerSDP   string `json:"answer_sdp"`
		ResourceURL string `json:"resource_url"`
		StatusCode  int    `json:"status_code"`
	}
	if err := json.Unmarshal(rawProxy, &proxyResp); err != nil {
		t.Fatalf("decode whip-proxy response failed: %v", err)
	}
	if !strings.Contains(proxyResp.AnswerSDP, "mock-answer") || proxyResp.ResourceURL != upstream.URL+"/whip/res/99" {
		t.Fatalf("unexpected whip-proxy POST response: %+v", proxyResp)
	}

	delBody, _ := json.Marshal(map[string]string{
		"resource_url": proxyResp.ResourceURL,
		"bearer_token": "tok",
	})
	delReq, _ := http.NewRequest(http.MethodDelete, base+"/api/simulators/pusher/whip-proxy", bytes.NewReader(delBody))
	delReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE whip-proxy failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !upstreamDeleted {
		t.Fatalf("expected whip-proxy DELETE to succeed, code=%d upstreamDeleted=%v", resp.StatusCode, upstreamDeleted)
	}

	// 4. Upload endpoint rejects invalid scheme cleanly
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	_ = mw.WriteField("config", `{"protocol":"rtmp","target_url":"http://invalid","source_kind":"upload"}`)
	fw, _ := mw.CreateFormFile("file", "demo.flv")
	_, _ = fw.Write([]byte("FLV\x01\x05\x00\x00\x00\x09\x00\x00\x00\x00"))
	_ = mw.Close()

	resp, err = http.Post(base+"/api/simulators/pusher/tasks/upload", mw.FormDataContentType(), &mp)
	if err != nil {
		t.Fatalf("POST upload failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid rtmp scheme on upload, got %d", resp.StatusCode)
	}
}
