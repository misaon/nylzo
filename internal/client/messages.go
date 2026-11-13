package client

import (
	"context"
	"crypto/hpke"
	"encoding/json"
	"maps"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/core/report"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

// Issued is an id with its token, the deadline of its message, the expiry share, still wrapped to the client's
// one-time key, and the OPAQUE registration response for the id.
type Issued struct {
	ID           core.ID
	IDToken      string
	Epoch        uint32
	Deadline     time.Time
	Share        [share.WrappedLen]byte
	Registration [opaque.RegistrationResponseLen]byte
}

type Instance struct {
	Limits api.InstanceLimits
	PoW    api.Work
	Create *Issued
}

// Creation asks the instance for an id, for a message of this TTL, with its share wrapped to ShareKey and an answer
// to the OPAQUE registration request.
type Creation struct {
	TTLSeconds   uint32
	ShareKey     hpke.PublicKey
	Registration [opaque.RegistrationRequestLen]byte
}

type CreateRequest struct {
	ID           core.ID
	IDToken      string
	Epoch        uint32
	Deadline     time.Time
	ReadKey      [signing.PublicKeyLen]byte
	Registration [opaque.RecordLen]byte
	ManageKey    [signing.PublicKeyLen]byte
	KDF          core.KDF
	OpensMax     uint16
	TTLSeconds   uint32
	Object       []byte
	PoWToken     string
}

type Report struct {
	Reason    string
	Contact   string
	GoodFaith bool
}

type signedRequest struct {
	method  string
	path    string
	purpose string
	seed    *[core.SeedLen]byte
	// challenge is the challenge of a handshake, which a claim or burn must sign; without it the request fetches one.
	challenge *[signing.ChallengeLen]byte
	headers   map[string]string
	// attemptHeaders runs before every attempt, so a retry never repeats a share key (Protocol §5.6).
	attemptHeaders func() map[string]string
	body           []byte
}

// Instance reads the limits, and with a creation also issues an id; it refuses a difficulty above the spec maximum.
func (c *Client) Instance(ctx context.Context, creation *Creation) (Instance, error) {
	var headers map[string]string
	if creation != nil {
		headers = map[string]string{
			api.TTLHeaderName:          strconv.FormatUint(uint64(creation.TTLSeconds), 10),
			api.ShareKeyHeaderName:     share.FormatKey(creation.ShareKey),
			api.RegistrationHeaderName: core.Encode(creation.Registration[:]),
		}
	}
	response, err := c.do(ctx, http.MethodGet, api.InstancePath, headers, nil, shortTimeout)
	if err != nil {
		return Instance{}, err
	}
	var body struct {
		Limits *api.InstanceLimits `json:"limits"`
		PoW    *api.Work           `json:"pow"`
		Create *api.InstanceCreate `json:"create"`
	}
	err = readJSON(response, &body)
	if err != nil {
		return Instance{}, err
	}
	if body.Limits == nil || body.PoW == nil || (creation != nil && body.Create == nil) ||
		body.PoW.Difficulty < 0 || body.PoW.Difficulty > proofofwork.MaxDifficulty {
		return Instance{}, ErrResponse
	}
	instance := Instance{Limits: *body.Limits, PoW: *body.PoW}
	if creation == nil {
		return instance, nil
	}
	issued, err := issuedFrom(body.Create, creation.TTLSeconds)
	if err != nil {
		return Instance{}, err
	}
	instance.Create = issued
	return instance, nil
}

func issuedFrom(create *api.InstanceCreate, ttlSeconds uint32) (*Issued, error) {
	if create.Deadline == "" {
		return nil, ErrServerOutdated
	}
	id, err := core.ParseID(create.ID)
	wrapped, shareErr := core.DecodeExact(create.Share, share.WrappedLen)
	registration, registrationErr := core.DecodeExact(create.Registration, opaque.RegistrationResponseLen)
	deadline, deadlineOK := issuedDeadline(create, ttlSeconds)
	if err != nil || shareErr != nil || registrationErr != nil || create.IDToken == "" || create.Epoch == nil ||
		!deadlineOK || int64(*create.Epoch) != deadline.Unix()/secondsPerMinute-1 {
		return nil, ErrResponse
	}
	return &Issued{
		ID:           id,
		IDToken:      create.IDToken,
		Epoch:        *create.Epoch,
		Deadline:     deadline,
		Share:        [share.WrappedLen]byte(wrapped),
		Registration: [opaque.RegistrationResponseLen]byte(registration),
	}, nil
}

// secondsPerMinute turns a deadline into its epoch, the last minute before it (spec section 5.6).
const secondsPerMinute = 60

// issuedDeadline accepts a deadline that is a whole minute and ends the TTL counted from the issue of the id, the
// token's expiry less its lifetime, plus less than the minute of rounding and the second the expiry was cut to; the
// server reads both from one clock, so the check needs no correct client clock (spec section 6.6).
func issuedDeadline(issued *api.InstanceCreate, ttlSeconds uint32) (time.Time, bool) {
	deadline, deadlineErr := time.Parse(time.RFC3339, issued.Deadline)
	expires, expiresErr := time.Parse(time.RFC3339, issued.ExpiresAt)
	if deadlineErr != nil || expiresErr != nil {
		return time.Time{}, false
	}
	rounding := deadline.Sub(expires.Add(-api.IDTokenLifetime)) - time.Duration(ttlSeconds)*time.Second
	seconds := deadline.Unix()
	wholeMinute := seconds%secondsPerMinute == 0 && deadline.Nanosecond() == 0
	return deadline, seconds > 0 && seconds <= math.MaxUint32 && wholeMinute &&
		rounding >= 0 && rounding < time.Minute+time.Second
}

func (c *Client) Create(ctx context.Context, request *CreateRequest) (time.Time, error) {
	header, _ := json.Marshal(api.CreateHeader{
		ID:           request.ID.String(),
		IDToken:      request.IDToken,
		Epoch:        &request.Epoch,
		ReadKey:      core.Encode(request.ReadKey[:]),
		Registration: new(core.Encode(request.Registration[:])),
		ManageKey:    core.Encode(request.ManageKey[:]),
		KDFID:        int(request.KDF),
		OpensMax:     int(request.OpensMax),
		TTLSeconds:   int64(request.TTLSeconds),
		Size:         int64(len(request.Object)),
	})
	headers := map[string]string{"Content-Type": "application/octet-stream", api.CreateHeaderName: core.Encode(header)}
	if request.PoWToken != "" {
		headers[api.WorkHeaderName] = request.PoWToken
	}
	timeout := TransferTime(int64(len(request.Object)))
	response, err := c.do(ctx, http.MethodPost, api.MessagesPath, headers, request.Object, timeout)
	if err != nil {
		return time.Time{}, err
	}
	var body struct {
		ID        string `json:"id"`
		ExpiresAt string `json:"expires_at"`
	}
	err = readJSON(response, &body)
	if err != nil {
		return time.Time{}, err
	}
	expires, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil || body.ID != request.ID.String() || !expires.Equal(request.Deadline) {
		return time.Time{}, ErrResponse
	}
	return expires, nil
}

func (c *Client) Metadata(ctx context.Context, id core.ID, read *[core.SeedLen]byte) (api.Metadata, error) {
	request := signedRequest{
		method:  http.MethodGet,
		path:    api.MessagePath(id.String()),
		purpose: signing.PurposeRead,
		seed:    read,
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return api.Metadata{}, err
	}
	var metadata api.Metadata
	err = readJSON(response, &metadata)
	if err != nil {
		return api.Metadata{}, err
	}
	if !api.KnownState(metadata.State) || metadata.Size < 0 || metadata.Size > envelope.MaxObjectBytes ||
		!timesValid(metadata.CreatedAt, metadata.ExpiresAt, metadata.FirstOpenedAt, metadata.EndedAt) {
		return api.Metadata{}, ErrResponse
	}
	return metadata, nil
}

func (c *Client) Status(ctx context.Context, id core.ID, manage *[core.SeedLen]byte) (api.Status, error) {
	request := signedRequest{
		method:  http.MethodGet,
		path:    api.MessagePath(id.String()) + api.StatusAction,
		purpose: signing.PurposeManage,
		seed:    manage,
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return api.Status{}, err
	}
	var status api.Status
	err = readJSON(response, &status)
	if err != nil {
		return api.Status{}, err
	}
	if !api.KnownState(status.State) ||
		!timesValid(status.CreatedAt, status.ExpiresAt, status.FirstOpenedAt, status.EndedAt) {
		return api.Status{}, ErrResponse
	}
	return status, nil
}

func (c *Client) Cancel(ctx context.Context, id core.ID, manage *[core.SeedLen]byte) error {
	request := signedRequest{
		method:  http.MethodPost,
		path:    api.MessagePath(id.String()) + api.CancelAction,
		purpose: signing.PurposeManage,
		seed:    manage,
	}
	return c.finish(ctx, &request)
}

// Burn finishes a login with its KE3, which every message needs, with or without a passphrase.
func (c *Client) Burn(ctx context.Context, id core.ID, read *[core.SeedLen]byte, login *Login) error {
	request := signedRequest{
		method:    http.MethodPost,
		path:      api.MessagePath(id.String()) + api.BurnAction,
		purpose:   signing.PurposeRead,
		seed:      read,
		challenge: &login.Challenge,
		headers:   map[string]string{api.KE3HeaderName: core.Encode(login.KE3[:])},
	}
	return c.finish(ctx, &request)
}

func (c *Client) Report(ctx context.Context, id core.ID, read *[core.SeedLen]byte, submitted *Report) error {
	contact, err := report.ParseContact(submitted.Contact)
	if err != nil {
		return err
	}
	payload := map[string]any{"id": id.String()}
	if reason := report.RedactReason(submitted.Reason); reason != "" {
		payload["reason"] = reason
	}
	if contact != "" {
		payload["contact"] = contact
	}
	if submitted.GoodFaith {
		payload["good_faith"] = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request := signedRequest{
		method:  http.MethodPost,
		path:    api.AbusePath,
		purpose: signing.PurposeRead,
		seed:    read,
		headers: map[string]string{"Content-Type": "application/json"},
		body:    body,
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return err
	}
	var answer struct {
		Status string `json:"status"`
	}
	if readErr := readJSON(response, &answer); readErr != nil {
		return readErr
	}
	switch answer.Status {
	case "accepted":
		return nil
	case "limit":
		return ErrReportLimit
	default:
		return ErrResponse
	}
}

func (c *Client) finish(ctx context.Context, request *signedRequest) error {
	response, err := c.signed(ctx, request, shortTimeout)
	if err != nil {
		return err
	}
	return drain(response)
}

// signed retries a 404 once with a fresh challenge, which a request that signs the challenge of a handshake cannot.
func (c *Client) signed(ctx context.Context, request *signedRequest, timeout time.Duration) (*http.Response, error) {
	response, err := c.signedOnce(ctx, request, timeout)
	if !IsKind(err, api.ErrorNotFound) || request.challenge != nil {
		return response, err
	}
	return c.signedOnce(ctx, request, timeout)
}

func (c *Client) signedOnce(
	ctx context.Context,
	request *signedRequest,
	timeout time.Duration,
) (*http.Response, error) {
	var challenge [signing.ChallengeLen]byte
	if request.challenge != nil {
		challenge = *request.challenge
	} else {
		fetched, err := c.challenge(ctx)
		if err != nil {
			return nil, err
		}
		challenge = fetched
	}
	headers := map[string]string{}
	maps.Copy(headers, request.headers)
	if request.attemptHeaders != nil {
		maps.Copy(headers, request.attemptHeaders())
	}
	components := signing.Components{
		ShareKey: headers[api.ShareKeyHeaderName],
		Lease:    headers[api.LeaseHeaderName],
		KE1:      headers[api.KE1HeaderName],
		KE3:      headers[api.KE3HeaderName],
	}
	signature := signing.SignRequest(
		request.seed,
		request.purpose,
		request.method,
		request.path,
		challenge,
		request.body, components,
	)
	headers["Authorization"] = signature.Authorization()
	return c.do(ctx, request.method, request.path, headers, request.body, timeout)
}

func (c *Client) challenge(ctx context.Context) ([signing.ChallengeLen]byte, error) {
	response, err := c.do(ctx, http.MethodGet, api.ChallengePath, nil, nil, shortTimeout)
	if err != nil {
		return [signing.ChallengeLen]byte{}, err
	}
	var body api.Challenge
	err = readJSON(response, &body)
	if err != nil {
		return [signing.ChallengeLen]byte{}, err
	}
	decoded, err := core.DecodeExact(body.Challenge, signing.ChallengeLen)
	if err != nil {
		return [signing.ChallengeLen]byte{}, ErrResponse
	}
	return [signing.ChallengeLen]byte(decoded), nil
}
