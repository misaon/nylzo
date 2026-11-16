package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	// visitQueue bounds the reports that wait for the backend; a page served while it is full is not counted.
	visitQueue   = 256
	visitTimeout = 2 * time.Second
	// visitPause is how long the frontend sends no report after the backend did not take one: it counts no visits,
	// it is not set up for reports, or it did not answer.
	visitPause = time.Minute
	// maxVisitAnswer bounds what the frontend reads of the backend's answer.
	maxVisitAnswer = 64
	// maxReportedUserAgent is longer than any browser's; a page asked for under a longer one is not reported, so
	// no client can make the backend refuse a report for its headers.
	maxReportedUserAgent = 512
	fallbackReportHost   = "localhost"
)

// A visitReport tells the backend that the frontend served a counted page: to whom, and to which program.
type visitReport struct {
	next       caddyhttp.Handler
	server     *caddyhttp.Server
	body       []byte
	remoteAddr string
	host       string
	userAgent  string
}

// startReports addresses the reports to the instance's own name, never to a Host a client sent.
func (f *Frontend) startReports(ctx context.Context) {
	f.reportHost = fallbackReportHost
	if public, err := url.Parse(f.PublicURL); err == nil && public.Host != "" {
		f.reportHost = public.Host
	}
	f.visits = make(chan visitReport, visitQueue)
	f.visitBudget = ratelimit.NewLimiter(api.DocumentReportsPerSecond, api.DocumentReportsBurst)
	go f.sendVisits(ctx)
}

// reportVisit queues the report and returns at once, so no page waits for the backend and none depends on it.
func (f *Frontend) reportVisit(r *http.Request, next caddyhttp.Handler) {
	visit, counted := webclient.DocumentVisit(r)
	now := time.Now()
	if !counted || len(r.UserAgent()) > maxReportedUserAgent || now.UnixNano() < f.visitsPaused.Load() ||
		!f.visitBudget.Allow(ratelimit.Bucket{}, now) {
		return
	}
	body, _ := json.Marshal(visit)
	server, ok := r.Context().Value(caddyhttp.ServerCtxKey).(*caddyhttp.Server)
	if !ok {
		return
	}
	report := visitReport{
		next:       next,
		server:     server,
		body:       body,
		remoteAddr: clientAddress(r),
		host:       f.reportHost,
		userAgent:  r.UserAgent(),
	}
	select {
	case f.visits <- report:
	default:
	}
}

// clientAddress is the address Caddy resolved for the client, which differs from the peer when Caddy itself
// stands behind a proxy it trusts.
func clientAddress(r *http.Request) string {
	if address, ok := caddyhttp.GetVar(r.Context(), caddyhttp.ClientIPVarKey).(string); ok && address != "" {
		return net.JoinHostPort(address, "0")
	}
	return r.RemoteAddr
}

func (f *Frontend) sendVisits(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case report := <-f.visits:
			if time.Now().UnixNano() < f.visitsPaused.Load() {
				continue
			}
			if !sendVisit(ctx, &report) {
				f.visitsPaused.Store(time.Now().Add(visitPause).UnixNano())
			}
		}
	}
}

// sendVisit posts the report through the route's own proxy as a request of its own, which carries nothing of the
// page request but the peer address and the User-Agent, so nothing a client sends shapes it beyond those two. It
// reports whether the backend takes reports. The proxy panics with http.ErrAbortHandler when the backend breaks
// off its answer; outside a request nothing but this function stops that panic from ending the process.
func sendVisit(ctx context.Context, report *visitReport) bool {
	taking := false
	func() {
		defer func() { _ = recover() }()
		taking = postVisit(ctx, report)
	}()
	return taking
}

func postVisit(ctx context.Context, report *visitReport) bool {
	ctx, cancel := context.WithTimeout(ctx, visitTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"http://"+report.host+api.DocumentVisitsPath,
		bytes.NewReader(report.body),
	)
	if err != nil {
		return true
	}
	request.RemoteAddr = report.remoteAddr
	request.RequestURI = api.DocumentVisitsPath
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", report.userAgent)
	response := &visitAnswer{header: make(http.Header)}
	request = caddyhttp.PrepareRequest(request, caddy.NewReplacer(), response, report.server)
	_ = report.next.ServeHTTP(response, request)
	return takesReports(response)
}

// takesReports is false for a backend that counts nothing, has no such address or did not answer. Another refusal
// is about this one report and stops no other.
func takesReports(response *visitAnswer) bool {
	switch response.status {
	case http.StatusOK:
		var answer api.DocumentVisits
		return json.Unmarshal(response.body, &answer) == nil && answer.Counted
	case http.StatusNotFound:
		return false
	default:
		return response.status >= http.StatusBadRequest && response.status < http.StatusInternalServerError
	}
}

// visitAnswer keeps the status and the first bytes of the backend's answer and hands nothing to a client.
type visitAnswer struct {
	header http.Header
	status int
	body   []byte
}

func (v *visitAnswer) Header() http.Header {
	return v.header
}

func (v *visitAnswer) WriteHeader(status int) {
	if v.status == 0 {
		v.status = status
	}
}

func (v *visitAnswer) Write(body []byte) (int, error) {
	v.WriteHeader(http.StatusOK)
	v.body = append(v.body, body[:min(len(body), maxVisitAnswer-len(v.body))]...)
	return len(body), nil
}
