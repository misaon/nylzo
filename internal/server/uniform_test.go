package server_test

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

func headersWithoutDate(h http.Header) map[string][]string {
	out := map[string][]string{}
	for k, v := range h {
		if k == "Date" {
			continue
		}
		out[k] = v
	}
	return out
}

func sameHeaders(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if va[i] != vb[i] {
				return false
			}
		}
	}
	return true
}

func TestAK21UniformNotFoundAndConflict(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.ReadPerHour = 1 << 30
	cfg.Rates.BucketPerSecond = 1 << 20
	cfg.Rates.BucketBurst = 1 << 20
	h := newHarness(t, cfg)
	live := newMessage(t, core.KDFNone, "", 1, 3600, "live")
	h.mustCreate(live)
	ended := newMessage(t, core.KDFNone, "", 1, 3600, "ended")
	h.mustCreate(ended)
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/messages/" + ended.id.String() + "/burn", headers: map[string]string{"Authorization": "Bearer " + ended.readToken}}); resp.StatusCode != 204 {
		t.Fatalf("burn: %d", resp.StatusCode)
	}
	if rec, err := h.store.Get(t.Context(), ended.id.String()); err != nil || rec.Tombstone == nil {
		t.Fatal("the ended message must be a tombstone before probing 409")
	}
	wrongSeed := core.NewSecret()
	livePath := "/api/v1/messages/" + live.id.String()
	classes := []struct {
		name     string
		endpoint string
		req      func() request
	}{
		{"unknown-id", "metadata", func() request {
			path := "/api/v1/messages/" + core.NewID().String()
			return signedRequest(wrongSeed, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil)
		}},
		{"wrong-R", "metadata", func() request {
			return signedRequest(wrongSeed, core.PurposeRead, "GET", livePath, h.srv.ChallengeForTest(), nil)
		}},
		{"unknown-id", "status", func() request {
			path := "/api/v1/messages/" + core.NewID().String() + "/status"
			return signedRequest(wrongSeed, core.PurposeManage, "GET", path, h.srv.ChallengeForTest(), nil)
		}},
		{"wrong-M", "status", func() request {
			return signedRequest(wrongSeed, core.PurposeManage, "GET", livePath+"/status", h.srv.ChallengeForTest(), nil)
		}},
		{"bad-sig", "metadata", func() request {
			return rawAuth(request{method: "GET", path: livePath}, badSignature(&live.link.R, core.PurposeRead, "GET", livePath, h.srv.ChallengeForTest()))
		}},
		{"stale", "metadata", func() request {
			return signedRequest(&live.link.R, core.PurposeRead, "GET", livePath, h.srv.ChallengeAtForTest(h.clock.Now().Add(-2*time.Minute)), nil)
		}},
		{"malformed", "metadata", func() request {
			return rawAuth(request{method: "GET", path: livePath}, core.AuthScheme+" x.y.z")
		}},
		{"bad-sig", "status", func() request {
			return rawAuth(request{method: "GET", path: livePath + "/status"}, badSignature(live.manage, core.PurposeManage, "GET", livePath+"/status", h.srv.ChallengeForTest()))
		}},
	}
	var refBody []byte
	var refHeaders map[string][]string
	for _, c := range classes {
		resp, body := h.do(c.req())
		if resp.StatusCode != 404 {
			t.Fatalf("%s: %d", c.name, resp.StatusCode)
		}
		hdr := headersWithoutDate(resp.Header)
		if refBody == nil {
			refBody, refHeaders = body, hdr
			continue
		}
		if string(body) != string(refBody) || !sameHeaders(hdr, refHeaders) {
			t.Fatalf("%s differs: %s %v vs %s %v", c.name, body, hdr, refBody, refHeaders)
		}
	}
	for _, m := range []*message{live, ended} {
		resp, body := h.create(m)
		if resp.StatusCode != 409 || string(body) != `{"error":"conflict"}` {
			t.Fatalf("409 for %s: %d %s", m.text, resp.StatusCode, body)
		}
	}
	const samples = 1000
	durations := make([][]time.Duration, len(classes))
	for i := 0; i < samples; i++ {
		for ci, c := range classes {
			req := c.req()
			start := time.Now()
			resp, _ := h.do(req)
			durations[ci] = append(durations[ci], time.Since(start))
			if resp.StatusCode != 404 {
				t.Fatalf("%s sample %d: %d", c.name, i, resp.StatusCode)
			}
		}
	}
	medians := make([]time.Duration, len(classes))
	p95s := make([]time.Duration, len(classes))
	for ci := range classes {
		d := durations[ci]
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		medians[ci] = d[len(d)/2]
		p95s[ci] = d[len(d)*95/100]
		t.Logf("AK21 %-8s %-10s median %s p95 %s", classes[ci].endpoint, classes[ci].name, medians[ci], p95s[ci])
	}
	minMedian, minP95 := map[string]time.Duration{}, map[string]time.Duration{}
	for ci, c := range classes {
		if m, ok := minMedian[c.endpoint]; !ok || medians[ci] < m {
			minMedian[c.endpoint] = medians[ci]
		}
		if p, ok := minP95[c.endpoint]; !ok || p95s[ci] < p {
			minP95[c.endpoint] = p95s[ci]
		}
	}
	for ci, c := range classes {
		// gstack-timing-ok: AK21 timing uniformity is the contract under test (eng review D2)
		if medians[ci] > 3*minMedian[c.endpoint] {
			t.Fatalf("%s %s median %s exceeds 3x the fastest class %s on the same endpoint", c.endpoint, c.name, medians[ci], minMedian[c.endpoint])
		}
		// gstack-timing-ok: AK21 timing uniformity is the contract under test (eng review D2)
		if p95s[ci] > 5*minP95[c.endpoint] {
			t.Fatalf("%s %s p95 %s exceeds 5x the fastest class %s on the same endpoint", c.endpoint, c.name, p95s[ci], minP95[c.endpoint])
		}
	}
}
