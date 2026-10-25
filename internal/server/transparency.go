package server

import (
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const TransparencyPath = "/.well-known/nylzo/transparency.json"

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
	TombstoneS    int64 `json:"tombstone_s"`
	AbuseReportS  int64 `json:"abuse_report_s"`
	DailyCounterS int64 `json:"daily_counter_s"`
}

type transparencyResponse struct {
	Server    string                `json:"server"`
	Operator  *config.Operator      `json:"operator,omitempty"`
	Limits    instanceLimits        `json:"limits"`
	Rates     transparencyRates     `json:"rates"`
	Retention transparencyRetention `json:"retention"`
}

func (s *Server) transparency() transparencyResponse {
	r := s.cfg.Rates
	day := ratelimit.Day(time.Unix(0, 0))
	resp := transparencyResponse{
		Server: s.cfg.Version,
		Limits: s.instanceLimits(),
		Rates: transparencyRates{
			CreatePerHour: r.CreatePerHour, CreatePerDay: r.CreatePerDay, CreateBytesPerDay: r.CreateBytesPerDay,
			ReadPerHour: r.ReadPerHour, AbusePerHour: r.AbusePerHour, CoarseMultiplier: r.CoarseMultiplier,
			GlobalDailyCreates: r.GlobalDailyCreates, PoWDifficulty: s.cfg.PoWDifficulty,
		},
		Retention: transparencyRetention{
			TombstoneS:    int64(store.TombstoneTTL / time.Second),
			AbuseReportS:  int64(abuseRetention / time.Second),
			DailyCounterS: int64(day.PurgeAt().Sub(day.End) / time.Second),
		},
	}
	if s.cfg.Operator != (config.Operator{}) {
		operator := s.cfg.Operator
		resp.Operator = &operator
	}
	return resp
}

func (s *Server) instanceLimits() instanceLimits {
	l := s.cfg.Limits
	return instanceLimits{
		MaxFileBytes:   l.MaxFileBytes,
		MaxTextBytes:   core.MaxTextBytes,
		MaxObjectBytes: l.MaxObjectBytes,
		MaxTTL:         int64(l.MaxTTL / time.Second),
		DefaultTTL:     int64(l.DefaultTTL / time.Second),
		MaxOpens:       l.MaxOpens,
	}
}

func (s *Server) handleTransparency(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.transparency())
}
