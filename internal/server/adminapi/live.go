package adminapi

import (
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/server/hostusage"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/visits"
)

// UsageShown is how many samples a live answer carries: five minutes at one sample every five seconds.
const UsageShown = 60

type liveResponse struct {
	visits.Live

	Challenge        string          `json:"challenge,omitempty"`
	SessionExpiresAt int64           `json:"session_expires_at"`
	UsageSupported   bool            `json:"usage_supported"`
	Usage            []usagePoint    `json:"usage"`
	Refused          []refusedClient `json:"refused"`
}

type usagePoint struct {
	At           int64   `json:"at"`
	Processor    float64 `json:"processor"`
	Load         float64 `json:"load"`
	MemoryUsed   uint64  `json:"memory_used"`
	MemoryTotal  uint64  `json:"memory_total"`
	ProcessBytes uint64  `json:"process_bytes"`
}

type refusedClient struct {
	Client  string           `json:"client"`
	Country string           `json:"country"`
	Reasons map[string]int64 `json:"reasons"`
	Count   int64            `json:"count"`
	First   int64            `json:"first"`
	Last    int64            `json:"last"`
}

func (a *API) handleLive(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	samples := a.Usage.History()
	response := liveResponse{
		Live:             a.Visits.Live(now),
		Challenge:        a.nextChallenge(now),
		SessionExpiresAt: sessionOf(r).expiresAt.Unix(),
		UsageSupported:   hostusage.Supported(),
		Usage:            make([]usagePoint, 0, len(samples)),
		Refused:          a.refused(),
	}
	for index := range samples {
		sample := &samples[index]
		response.Usage = append(response.Usage, usagePoint{
			At:           sample.At.Unix(),
			Processor:    sample.Processor,
			Load:         sample.Load,
			MemoryUsed:   sample.MemoryUsed,
			MemoryTotal:  sample.MemoryTotal,
			ProcessBytes: sample.ProcessBytes,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) refused() []refusedClient {
	listed := a.Refusals.List()
	clients := make([]refusedClient, 0, len(listed))
	for index := range listed {
		clients = append(clients, a.describeRefused(&listed[index]))
	}
	return clients
}

func (a *API) describeRefused(refused *ratelimit.Refused) refusedClient {
	name := refused.Client.String()
	if refused.Client.Addr().Is4() {
		name = refused.Client.Addr().String()
	}
	return refusedClient{
		Client:  name,
		Country: a.Countries.Lookup(refused.Client.Addr()).String(),
		Reasons: refused.Reasons,
		Count:   refused.Count,
		First:   refused.First.Unix(),
		Last:    refused.Last.Unix(),
	}
}
