package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEchoCountsBytesAndHeaders(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 5<<20)
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(body))
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	rec := httptest.NewRecorder()

	handleEcho(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got echoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ReceivedBytes != int64(len(body)) {
		t.Fatalf("received_bytes = %d, want %d", got.ReceivedBytes, len(body))
	}
	if got.XRealIP != "203.0.113.9" || got.XForwardedFor != "203.0.113.9, 10.0.0.1" {
		t.Fatalf("headers not echoed: %+v", got)
	}
}

func TestEchoRejectsOversizedBody(t *testing.T) {
	body := bytes.Repeat([]byte("x"), maxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleEcho(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestStartReportsUptime(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/start", nil)
	rec := httptest.NewRecorder()

	handleStart(rec, req)

	var got startResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UptimeMs < 0 || got.StartedAt.IsZero() {
		t.Fatalf("bad start response: %+v", got)
	}
}
