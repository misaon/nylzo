package server

import (
	"crypto/hpke"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	corekeys "github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	// challengesFullRetry waits for the minute purge, which frees the challenges that expired.
	challengesFullRetry = time.Minute
	// clockBehindRetry waits for a clock that stepped back behind the epoch ratchet, which never goes back.
	clockBehindRetry = time.Minute
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
	Share    int    `json:"share"`
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
	creation, refusal := s.parseCreation(r.Header)
	if refusal != nil {
		writeError(w, http.StatusBadRequest, *refusal)
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
			Share:    share.Suite,
			Server:   s.settings.Version,
		},
	}
	if creation.requested {
		created, err := s.issueID(&creation, now)
		if errors.Is(err, epoch.ErrClockBehind) {
			s.events.WarnContext(r.Context(), "epoch-clock-behind")
			setRetryAfter(w, clockBehindRetry)
			writeError(w, http.StatusServiceUnavailable, errUnavailable)
			return
		}
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
	requested    bool
	ttlSeconds   uint32
	shareKey     hpke.PublicKey
	registration [opaque.RegistrationRequestLen]byte
}

// parseCreation reads the TTL, share key and registration headers, which a client sends together when it is about to
// create a message; a client of protocol v2 sends the first two only.
func (s *Server) parseCreation(header http.Header) (creationRequest, *api.ErrorResponse) {
	values, refusal := creationHeaders(header)
	if refusal != nil || values == nil {
		return creationRequest{}, refusal
	}
	parsed, err := strconv.ParseUint(values.ttl, 10, 32)
	seconds := uint32(parsed)
	if err != nil || seconds < envelope.MinTTLSeconds ||
		time.Duration(seconds)*time.Second > s.settings.Limits.MaxTTL ||
		strconv.FormatUint(parsed, 10) != values.ttl {
		return creationRequest{}, &errBadRequest
	}
	key, err := share.ParseKey(values.shareKey)
	if err != nil {
		if errors.Is(err, share.ErrOutdated) {
			return creationRequest{}, &errClientOutdated
		}
		return creationRequest{}, &errBadRequest
	}
	creation := creationRequest{requested: true, ttlSeconds: seconds, shareKey: key}
	if !parseElement(creation.registration[:], values.registration) {
		return creationRequest{}, &errBadRequest
	}
	return creation, nil
}

type creationValues struct {
	ttl, shareKey, registration string
}

// creationHeaders accepts all three headers once or none of them; a request with the first two only comes from a
// client of protocol v2.
func creationHeaders(header http.Header) (*creationValues, *api.ErrorResponse) {
	ttlValues, keyValues := header.Values(api.TTLHeaderName), header.Values(api.ShareKeyHeaderName)
	registrationValues := header.Values(api.RegistrationHeaderName)
	switch {
	case len(ttlValues) == 0 && len(keyValues) == 0 && len(registrationValues) == 0:
		return nil, nil
	case len(ttlValues) == 1 && len(keyValues) == 1 && len(registrationValues) == 0:
		return nil, &errClientOutdated
	case len(ttlValues) != 1 || len(keyValues) != 1 || len(registrationValues) != 1:
		return nil, &errBadRequest
	default:
		return &creationValues{ttl: ttlValues[0], shareKey: keyValues[0], registration: registrationValues[0]}, nil
	}
}

// parseElement accepts the canonical encoding of a ristretto255 element other than the identity.
func parseElement(target []byte, encoded string) bool {
	if !core.DecodeInto(target, encoded) {
		return false
	}
	_, err := opaque.DeserializeElement(target)
	return err == nil
}

// issueID fixes the deadline of a message made with the new id, the TTL from now rounded up to the minute, and hands
// out the share of the epoch that the deadline ends, made by the generation of the current hour (spec section 5.6).
func (s *Server) issueID(creation *creationRequest, now time.Time) (api.InstanceCreate, error) {
	id := core.NewID()
	deadline := store.ExpiresAt(now, time.Duration(creation.ttlSeconds)*time.Second)
	shareEpoch := epoch.Of(deadline)
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
	token, expires := s.idTokens.Issue(id, shareEpoch, creation.ttlSeconds, generation, now)
	wrapped, err := share.Wrap(&expiryShare, creation.shareKey)
	if err != nil {
		return api.InstanceCreate{}, err
	}
	var response [opaque.RegistrationResponseLen]byte
	secretmode.Do(func() {
		var keys opaque.ServerKeys
		keys, _, err = s.ring.OPAQUEKeys(id[:], s.ring.CurrentID())
		if err == nil {
			credential := corekeys.CredentialIdentifier(id, &expiryShare)
			defer clear(credential)
			response, err = opaque.CreateRegistrationResponse(creation.registration[:], &keys, credential)
		}
		keys.Wipe()
	})
	if err != nil {
		return api.InstanceCreate{}, err
	}
	return api.InstanceCreate{
		ID:           id.String(),
		IDToken:      token,
		ExpiresAt:    formatTime(expires),
		Epoch:        &shareEpoch,
		Deadline:     formatTime(deadline),
		Share:        core.Encode(wrapped[:]),
		Registration: core.Encode(response[:]),
	}, nil
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
