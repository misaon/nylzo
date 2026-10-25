package server

import (
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const TransparencyPath = "/.well-known/nylzo/transparency.json"

type instanceResponse struct {
	Limits   instanceLimits   `json:"limits"`
	Work     instanceWork     `json:"pow"`
	Create   instanceCreate   `json:"create"`
	Versions instanceVersions `json:"versions"`
}

type instanceLimits struct {
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxTextBytes   int64 `json:"max_text_bytes"`
	MaxObjectBytes int64 `json:"max_object_bytes"`
	MaxTTL         int64 `json:"max_ttl_s"`
	DefaultTTL     int64 `json:"default_ttl_s"`
	MaxOpens       int   `json:"max_opens"`
}

type instanceWork struct {
	Required   bool   `json:"required"`
	Difficulty int    `json:"difficulty"`
	Challenge  string `json:"challenge,omitempty"`
}

type instanceCreate struct {
	ID        string `json:"id"`
	IDToken   string `json:"id_token"`
	ExpiresAt string `json:"expires_at"`
}

type instanceVersions struct {
	API      int    `json:"api"`
	Envelope int    `json:"envelope"`
	Fragment int    `json:"fragment"`
	Server   string `json:"server"`
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

type transparencyResponse struct {
	Server    string                `json:"server"`
	Operator  *config.Operator      `json:"operator,omitempty"`
	Limits    instanceLimits        `json:"limits"`
	Rates     transparencyRates     `json:"rates"`
	Retention transparencyRetention `json:"retention"`
}

type transparencyRates struct {
	CreatePerHour      int64 `json:"create_per_hour"`
	CreatePerDay       int64 `json:"create_per_day"`
	CreateBytesPerDay  int64 `json:"create_bytes_per_day"`
	ReadPerHour        int64 `json:"read_per_hour"`
	AbusePerHour       int64 `json:"abuse_per_hour"`
	CoarseMultiplier   int64 `json:"coarse_multiplier"`
	GlobalDailyCreates int64 `json:"global_daily_creates"`
	PoWDifficulty      int   `json:"pow_difficulty"`
}

type transparencyRetention struct {
	TombstoneSeconds    int64 `json:"tombstone_s"`
	AbuseReportSeconds  int64 `json:"abuse_report_s"`
	DailyCounterSeconds int64 `json:"daily_counter_s"`
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	now := s.clock()
	id := core.NewID()
	token, expires := s.idTokens.Issue(id, now)
	response := instanceResponse{
		Limits: s.instanceLimits(),
		Work:   instanceWork{Difficulty: s.settings.PoWDifficulty},
		Create: instanceCreate{ID: id.String(), IDToken: token, ExpiresAt: formatTime(expires)},
		Versions: instanceVersions{
			API:      1,
			Envelope: core.Version,
			Fragment: core.FragmentVersion,
			Server:   s.settings.Version,
		},
	}
	gate, err := s.globalGate(r.Context(), now)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if gate.workNeeded {
		response.Work.Required = true
		response.Work.Challenge = s.work.Challenge(now)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleChallenge(w http.ResponseWriter, _ *http.Request) {
	challenge, expires := s.challenges.Issue(s.clock())
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: core.Encode(challenge[:]), ExpiresAt: formatTime(expires)})
}

func (s *Server) handleTransparency(w http.ResponseWriter, _ *http.Request) {
	rates := s.settings.Rates
	day := ratelimit.Day(time.Unix(0, 0))
	response := transparencyResponse{
		Server: s.settings.Version,
		Limits: s.instanceLimits(),
		Rates: transparencyRates{
			CreatePerHour:      rates.CreatePerHour,
			CreatePerDay:       rates.CreatePerDay,
			CreateBytesPerDay:  rates.CreateBytesPerDay,
			ReadPerHour:        rates.ReadPerHour,
			AbusePerHour:       rates.AbusePerHour,
			CoarseMultiplier:   rates.CoarseMultiplier,
			GlobalDailyCreates: rates.GlobalDailyCreates,
			PoWDifficulty:      s.settings.PoWDifficulty,
		},
		Retention: transparencyRetention{
			TombstoneSeconds:    int64(store.TombstoneTTL / time.Second),
			AbuseReportSeconds:  int64(abuse.Retention / time.Second),
			DailyCounterSeconds: int64(day.PurgeAt().Sub(day.End) / time.Second),
		},
	}
	if operator := s.settings.Operator; operator != (config.Operator{}) {
		response.Operator = &operator
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) instanceLimits() instanceLimits {
	limits := s.settings.Limits
	return instanceLimits{
		MaxFileBytes:   limits.MaxFileBytes,
		MaxTextBytes:   core.MaxTextBytes,
		MaxObjectBytes: limits.MaxObjectBytes,
		MaxTTL:         int64(limits.MaxTTL / time.Second),
		DefaultTTL:     int64(limits.DefaultTTL / time.Second),
		MaxOpens:       limits.MaxOpens,
	}
}
