package server

import (
	"crypto/ecdh"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/proof"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	// storeSlack covers rounding expires_at up to the minute and writing the arrived upload (spec section 5.6).
	storeSlack = 2 * time.Minute
	// challengesFullRetry waits for the minute purge, which frees the challenges that expired.
	challengesFullRetry = time.Minute
)

type instanceResponse struct {
	Limits   api.InstanceLimits  `json:"limits"`
	Work     api.Work            `json:"pow"`
	Create   *api.InstanceCreate `json:"create,omitempty"`
	Versions instanceVersions    `json:"versions"`
	Build    buildinfo.Info      `json:"build"`
}

type instanceVersions struct {
	API      int    `json:"api"`
	Envelope int    `json:"envelope"`
	Fragment int    `json:"fragment"`
	Server   string `json:"server"`
}

type transparencyResponse struct {
	Build     buildinfo.Info        `json:"build"`
	Server    string                `json:"server"`
	Operator  *config.Operator      `json:"operator,omitempty"`
	Limits    api.InstanceLimits    `json:"limits"`
	Rates     transparencyRates     `json:"rates"`
	Retention transparencyRetention `json:"retention"`
}

type transparencyRates struct {
	CreatePerHour      int64 `json:"create_per_hour"`
	CreatePerDay       int64 `json:"create_per_day"`
	CreateBytesPerDay  int64 `json:"create_bytes_per_day"`
	ReadPerHour        int64 `json:"read_per_hour"`
	ReadBytesPerHour   int64 `json:"read_bytes_per_hour"`
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
	now := time.Now()
	creation, ok := s.parseCreation(r.Header)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	response := instanceResponse{
		Limits: s.instanceLimits(),
		Build:  s.buildInformation(),
		Work:   api.Work{Difficulty: s.settings.PoWDifficulty},
		Versions: instanceVersions{
			API:      1,
			Envelope: int(envelope.Version),
			Fragment: int(core.FragmentVersion),
			Server:   s.settings.Version,
		},
	}
	if creation.requested {
		created, err := s.issueID(&creation, now)
		if err != nil {
			s.writeInternal(w, r, err)
			return
		}
		response.Create = &created
	}
	gate, err := s.globalGate(r.Context(), now)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if gate.workNeeded {
		response.Work.Required = true
		response.Work.Difficulty = gate.difficulty
		response.Work.Challenge = s.work.Challenge(now, gate.difficulty)
	}
	writeJSON(w, http.StatusOK, response)
}

type creationRequest struct {
	requested bool
	ttl       time.Duration
	shareKey  *ecdh.PublicKey
}

// parseCreation reads the TTL and share key headers, which a client sends together when it is about to create a message.
func (s *Server) parseCreation(header http.Header) (creationRequest, bool) {
	ttlValues, keyValues := header.Values(api.TTLHeaderName), header.Values(api.ShareKeyHeaderName)
	if len(ttlValues) == 0 && len(keyValues) == 0 {
		return creationRequest{}, true
	}
	if len(ttlValues) != 1 || len(keyValues) != 1 {
		return creationRequest{}, false
	}
	seconds, err := strconv.ParseInt(ttlValues[0], 10, 64)
	if err != nil || seconds < envelope.MinTTLSeconds || seconds > int64(s.settings.Limits.MaxTTL/time.Second) ||
		strconv.FormatInt(seconds, 10) != ttlValues[0] {
		return creationRequest{}, false
	}
	key, err := share.ParseKey(keyValues[0])
	if err != nil {
		return creationRequest{}, false
	}
	return creationRequest{requested: true, ttl: time.Duration(seconds) * time.Second, shareKey: key}, true
}

// issueID hands out an id with the share of the hour in which a message made with it can expire at the latest, made
// by the generation of the current hour.
func (s *Server) issueID(creation *creationRequest, now time.Time) (api.InstanceCreate, error) {
	id := core.NewID()
	shareEpoch := epoch.Hour(latestExpiry(now, creation.ttl, s.settings.Limits.MaxObjectBytes))
	var (
		generation  epoch.GenerationID
		expiryShare [core.ShareLen]byte
		err         error
	)
	secretmode.Do(func() {
		generation, expiryShare, err = s.epochs.Issue(shareEpoch, seal.LocatorOf(id[:]), now)
	})
	defer clear(expiryShare[:])
	if err != nil {
		return api.InstanceCreate{}, err
	}
	token, expires := s.idTokens.Issue(id, shareEpoch, generation, now)
	wrapped, err := share.Wrap(&expiryShare, creation.shareKey)
	if err != nil {
		return api.InstanceCreate{}, err
	}
	return api.InstanceCreate{
		ID:        id.String(),
		IDToken:   token,
		ExpiresAt: formatTime(expires),
		Epoch:     &shareEpoch,
		Share:     core.Encode(wrapped[:]),
	}, nil
}

// latestExpiry is the latest expiry of a message whose id is issued now: the client may start its upload until the id
// token expires, and the upload of the largest object may take its whole transfer time.
func latestExpiry(now time.Time, ttl time.Duration, maxObjectBytes int64) time.Time {
	upload := max(createBodyTimeout, store.TransferTime(maxObjectBytes))
	return now.Add(proof.IDTokenTTL() + upload + storeSlack + ttl)
}

func (s *Server) handleChallenge(w http.ResponseWriter, _ *http.Request) {
	challenge, expires, issued := s.challenges.Issue(time.Now())
	if !issued {
		setRetryAfter(w, challengesFullRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, api.Challenge{Challenge: core.Encode(challenge[:]), ExpiresAt: formatTime(expires)})
}

func (s *Server) handleTransparency(w http.ResponseWriter, _ *http.Request) {
	rates := s.settings.Rates
	day := ratelimit.Day(time.Unix(0, 0))
	response := transparencyResponse{
		Server: s.settings.Version,
		Limits: s.instanceLimits(),
		Build:  s.buildInformation(),
		Rates: transparencyRates{
			CreatePerHour:      rates.CreatePerHour,
			CreatePerDay:       rates.CreatePerDay,
			CreateBytesPerDay:  rates.CreateBytesPerDay,
			ReadPerHour:        rates.ReadPerHour,
			ReadBytesPerHour:   rates.ReadBytesPerHour,
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

func (s *Server) instanceLimits() api.InstanceLimits {
	limits := s.settings.Limits
	return api.InstanceLimits{
		MaxFileBytes:      limits.MaxFileBytes,
		MaxTextBytes:      envelope.MaxTextBytes,
		MaxObjectBytes:    limits.MaxObjectBytes,
		MaxTTLSeconds:     int64(limits.MaxTTL / time.Second),
		DefaultTTLSeconds: int64(limits.DefaultTTL / time.Second),
		MaxOpens:          limits.MaxOpens,
	}
}

func (s *Server) buildInformation() buildinfo.Info {
	information := s.settings.Build
	information.Version = s.settings.Version
	return information
}
