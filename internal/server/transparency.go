package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

const maxOperatorField = 200

type Operator struct {
	Name    string `json:"name,omitempty"`
	Contact string `json:"contact,omitempty"`
	Hosting string `json:"hosting,omitempty"`
}

func (o Operator) set() bool {
	return o != Operator{}
}

func (o Operator) validate() error {
	for _, field := range []struct{ key, value string }{
		{"NYLZO_OPERATOR", o.Name}, {"NYLZO_CONTACT", o.Contact}, {"NYLZO_HOSTING", o.Hosting},
	} {
		if !plainText(field.value) {
			return errors.New(field.key + " must be valid UTF-8 text of at most 200 bytes without control or format characters")
		}
	}
	if o.Contact != "" && !ValidContact(o.Contact) {
		return errors.New("NYLZO_CONTACT must be an email address or an https:// address")
	}
	return nil
}

func plainText(s string) bool {
	return len(s) <= maxOperatorField && utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	})
}

var (
	emailContact = regexp.MustCompile(`^[A-Za-z0-9_%+-]+(\.[A-Za-z0-9_%+-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	httpsContact = regexp.MustCompile(`^https://[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*(:[0-9]{1,5})?(/[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*)?$`)
)

func ValidContact(contact string) bool {
	return emailContact.MatchString(contact) || httpsContact.MatchString(contact)
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
	TombstoneS    int64 `json:"tombstone_s"`
	AbuseReportS  int64 `json:"abuse_report_s"`
	DailyCounterS int64 `json:"daily_counter_s"`
}

type transparencyResponse struct {
	Server    string                `json:"server"`
	Operator  *Operator             `json:"operator,omitempty"`
	Limits    instanceLimits        `json:"limits"`
	Rates     transparencyRates     `json:"rates"`
	Retention transparencyRetention `json:"retention"`
}

func (s *Server) transparency() transparencyResponse {
	r := s.cfg.Rates
	day := dayWindow(time.Unix(0, 0))
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
			DailyCounterS: int64(day.purgeAt().Sub(day.end) / time.Second),
		},
	}
	if s.cfg.Operator.set() {
		operator := s.cfg.Operator
		resp.Operator = &operator
	}
	return resp
}

func (s *Server) instanceLimits() instanceLimits {
	l := s.cfg.Limits
	return instanceLimits{MaxFileBytes: l.MaxFileBytes, MaxTextBytes: core.MaxTextBytes, MaxObjectBytes: l.MaxObjectBytes, MaxTTL: int64(l.MaxTTL / time.Second), DefaultTTL: int64(l.DefaultTTL / time.Second), MaxOpens: l.MaxOpens, DefaultKDF: int(l.DefaultKDF)}
}

func (s *Server) handleTransparency(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.transparency())
}
