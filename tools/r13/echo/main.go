package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	maxBodyBytes      = 6 << 20
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 120 * time.Second
	writeTimeout      = 120 * time.Second
	idleTimeout       = 30 * time.Second
	maxHeaderBytes    = 8 << 10
)

var startedAt = time.Now()

type echoResponse struct {
	ReceivedBytes         int64               `json:"received_bytes"`
	ContentLength         int64               `json:"content_length"`
	TransferEncoding      []string            `json:"transfer_encoding,omitempty"`
	ReadMillis            float64             `json:"read_ms"`
	ReadError             string              `json:"read_error,omitempty"`
	RemoteAddr            string              `json:"remote_addr"`
	XRealIP               string              `json:"x_real_ip"`
	XForwardedFor         string              `json:"x_forwarded_for"`
	XEnvoyExternalAddress string              `json:"x_envoy_external_address"`
	Proto                 string              `json:"proto"`
	Host                  string              `json:"host"`
	TLS                   bool                `json:"tls"`
	Headers               map[string][]string `json:"headers"`
	Instance              string              `json:"instance"`
	StartedAt             time.Time           `json:"started_at"`
}

type startResponse struct {
	StartedAt time.Time `json:"started_at"`
	Now       time.Time `json:"now"`
	UptimeMs  float64   `json:"uptime_ms"`
	Instance  string    `json:"instance"`
	Proto     string    `json:"proto"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /echo", handleEcho)
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /start", handleStart)
	mux.HandleFunc("GET /{$}", handleIndex)

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		Protocols:         &protocols,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("r13-echo listening on :%s (started %s, instance %s)", port, startedAt.Format(time.RFC3339Nano), instanceName())

	select {
	case err := <-errCh:
		log.Fatalf("server: %v", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	readStart := time.Now()
	n, readErr := io.Copy(io.Discard, r.Body)
	readDur := time.Since(readStart)

	resp := echoResponse{
		ReceivedBytes:         n,
		ContentLength:         r.ContentLength,
		TransferEncoding:      r.TransferEncoding,
		ReadMillis:            float64(readDur.Microseconds()) / 1000,
		RemoteAddr:            r.RemoteAddr,
		XRealIP:               r.Header.Get("X-Real-IP"),
		XForwardedFor:         r.Header.Get("X-Forwarded-For"),
		XEnvoyExternalAddress: r.Header.Get("X-Envoy-External-Address"),
		Proto:                 r.Proto,
		Host:                  r.Host,
		TLS:                   r.TLS != nil,
		Headers:               r.Header,
		Instance:              instanceName(),
		StartedAt:             startedAt,
	}

	status := http.StatusOK
	if readErr != nil {
		resp.ReadError = readErr.Error()
		status = http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(readErr, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
	}
	writeJSON(w, status, resp)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func handleStart(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	writeJSON(w, http.StatusOK, startResponse{
		StartedAt: startedAt,
		Now:       now,
		UptimeMs:  float64(now.Sub(startedAt).Microseconds()) / 1000,
		Instance:  instanceName(),
		Proto:     r.Proto,
	})
}

func handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "r13-echo\nPOST /echo\nGET /healthz\nGET /start\n")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}

func instanceName() string {
	host, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %s status=%d content_length=%d remote=%s xff=%q xrip=%q dur=%s",
			r.Proto, r.Method, r.URL.Path, rec.status, r.ContentLength, r.RemoteAddr,
			r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Real-IP"), time.Since(start).Round(time.Millisecond))
	})
}
