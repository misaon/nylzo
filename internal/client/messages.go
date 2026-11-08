package client

import (
	"context"
	"crypto/ecdh"
	"encoding/json"
	"maps"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/core/report"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

// Issued is an id with its token and the expiry share, still wrapped to the client's one-time key.
type Issued struct {
	ID      core.ID
	IDToken string
	Epoch   uint32
	Share   [share.WrappedLen]byte
}

type Instance struct {
	Limits api.InstanceLimits
	PoW    api.Work
	Create *Issued
}

// Creation asks the instance for an id, for a message of this TTL, with its share wrapped to ShareKey.
type Creation struct {
	TTLSeconds uint32
	ShareKey   *ecdh.PublicKey
}

type CreateRequest struct {
	ID         core.ID
	IDToken    string
	Epoch      uint32
	ReadKey    [signing.PublicKeyLen]byte
	Verifier   [signing.PublicKeyLen]byte
	ManageKey  [signing.PublicKeyLen]byte
	KDF        core.KDF
	OpensMax   uint16
	TTLSeconds uint32
	Object     []byte
	PoWToken   string
}

type Report struct {
	Reason    string
	Contact   string
	GoodFaith bool
}

type signedRequest struct {
	method   string
	path     string
	purpose  string
	seed     *[core.SeedLen]byte
	verifier *[core.SeedLen]byte
	headers  map[string]string
	// attemptHeaders runs before every attempt, so a retry never repeats a share key (Protocol §5.6).
	attemptHeaders func() map[string]string
	body           []byte
}

// Instance reads the limits, and with a creation also issues an id; it refuses a difficulty above the spec maximum.
func (c *Client) Instance(ctx context.Context, creation *Creation) (Instance, error) {
	var headers map[string]string
	if creation != nil {
		headers = map[string]string{
			api.TTLHeaderName:      strconv.FormatUint(uint64(creation.TTLSeconds), 10),
			api.ShareKeyHeaderName: core.Encode(creation.ShareKey.Bytes()),
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
	id, err := core.ParseID(body.Create.ID)
	wrapped, shareErr := core.DecodeExact(body.Create.Share, share.WrappedLen)
	if err != nil || shareErr != nil || body.Create.IDToken == "" || body.Create.Epoch == nil {
		return Instance{}, ErrResponse
	}
	instance.Create = &Issued{
		ID:      id,
		IDToken: body.Create.IDToken,
		Epoch:   *body.Create.Epoch,
		Share:   [share.WrappedLen]byte(wrapped),
	}
	return instance, nil
}

func (c *Client) Create(ctx context.Context, request *CreateRequest) (time.Time, error) {
	header, _ := json.Marshal(api.CreateHeader{
		ID:          request.ID.String(),
		IDToken:     request.IDToken,
		Epoch:       &request.Epoch,
		ReadKey:     core.Encode(request.ReadKey[:]),
		VerifierKey: new(core.Encode(request.Verifier[:])),
		ManageKey:   core.Encode(request.ManageKey[:]),
		KDFID:       int(request.KDF),
		OpensMax:    int(request.OpensMax),
		TTLSeconds:  int64(request.TTLSeconds),
		Size:        int64(len(request.Object)),
	})
	headers := map[string]string{"Content-Type": "application/octet-stream", api.CreateHeaderName: core.Encode(header)}
	if request.PoWToken != "" {
		headers[api.WorkHeaderName] = request.PoWToken
	}
	timeout := transferTime(int64(len(request.Object)))
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
	if err != nil || body.ID != request.ID.String() {
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
	if !api.KnownState(metadata.State) ||
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
	return c.end(ctx, api.MessagePath(id.String())+api.CancelAction, signing.PurposeManage, manage)
}

func (c *Client) Burn(ctx context.Context, id core.ID, read *[core.SeedLen]byte) error {
	return c.end(ctx, api.MessagePath(id.String())+api.BurnAction, signing.PurposeRead, read)
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

func (c *Client) end(ctx context.Context, path, purpose string, seed *[core.SeedLen]byte) error {
	request := signedRequest{method: http.MethodPost, path: path, purpose: purpose, seed: seed}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return err
	}
	return drain(response)
}

func (c *Client) signed(ctx context.Context, request *signedRequest, timeout time.Duration) (*http.Response, error) {
	response, err := c.signedOnce(ctx, request, timeout)
	if !IsKind(err, api.ErrorNotFound) {
		return response, err
	}
	return c.signedOnce(ctx, request, timeout)
}

func (c *Client) signedOnce(
	ctx context.Context,
	request *signedRequest,
	timeout time.Duration,
) (*http.Response, error) {
	challenge, err := c.challenge(ctx)
	if err != nil {
		return nil, err
	}
	signature := signing.SignRequest(
		request.seed,
		request.purpose,
		request.method,
		request.path,
		challenge,
		request.body,
	)
	headers := map[string]string{"Authorization": signature.Authorization()}
	if request.verifier != nil {
		verifier := signing.SignRequest(
			request.verifier,
			signing.PurposeVerifier,
			request.method,
			request.path,
			challenge,
			nil,
		)
		headers[api.VerifierHeaderName] = verifier.VerifierHeader()
	}
	maps.Copy(headers, request.headers)
	if request.attemptHeaders != nil {
		maps.Copy(headers, request.attemptHeaders())
	}
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
