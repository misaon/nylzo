package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
)

func BenchmarkSignedMetadataWithASpentChallenge(b *testing.B) {
	s, _ := testServer(b, openStore(b), func(c *config.Server) {
		c.Rates.SourcePerSecond, c.Rates.SourceBurst = 1e9, 1e9
		c.Rates.BucketPerSecond, c.Rates.BucketBurst = 1e9, 1e9
		c.Rates.ReadPerHour = 0
	})
	path := "/api/v1/messages/" + core.NewID().String()
	c, _ := s.challenges.Issue(s.clock().Add(-2 * time.Minute))
	auth := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, c, nil).Authorization()
	for b.Loop() {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			b.Fatalf("status %d", w.Code)
		}
	}
}
