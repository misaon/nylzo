package client

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/core/report"
	"github.com/misaon/nylzo/internal/core/signing"
)

type Limits struct {
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxTextBytes   int64 `json:"max_text_bytes"`
	MaxObjectBytes int64 `json:"max_object_bytes"`
	MaxTTLSeconds  int64 `json:"max_ttl_s"`
	DefaultTTL     int64 `json:"default_ttl_s"`
	MaxOpens       int   `json:"max_opens"`
}

type PoW struct {
	Required   bool   `json:"required"`
	Difficulty int    `json:"difficulty"`
	Challenge  string `json:"challenge"`
}

type Issued struct {
	ID      core.ID
	IDToken string
}

type Instance struct {
	Limits          Limits
	PoW             PoW
	Create          Issued
	FileCompression string
}

type CreateRequest struct {
	ID         core.ID
	IDToken    string
	ReadKey    [signing.PublicKeyLen]byte
	Verifier   *[signing.PublicKeyLen]byte
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

type Metadata struct {
	State         string `json:"state"`
	CreatedAt     string `json:"created_at"`
	ExpiresAt     string `json:"expires_at"`
	OpensMax      int    `json:"opens_max"`
	TTLSeconds    int64  `json:"ttl_s"`
	KDF           int    `json:"kdf_id"`
	Size          int64  `json:"size"`
	FirstOpenedAt string `json:"first_opened_at"`
	EndedAt       string `json:"ended_at"`
}

type Status struct {
	State                    string `json:"state"`
	OpensUsed                int    `json:"opens_used"`
	OpensMax                 int    `json:"opens_max"`
	CreatedAt                string `json:"created_at"`
	ExpiresAt                string `json:"expires_at"`
	FirstOpenedAt            string `json:"first_opened_at"`
	EndedAt                  string `json:"ended_at"`
	FailedPassphraseAttempts int    `json:"failed_passphrase_attempts"`
}

type signedRequest struct {
	method   string
	path     string
	purpose  string
	seed     *[core.SeedLen]byte
	verifier *[core.SeedLen]byte
	headers  map[string]string
	body     []byte
}

func (c *Client) Instance(ctx context.Context) (Instance, error) {
	response, err := c.do(ctx, http.MethodGet, "/api/v1/instance", nil, nil, shortTimeout)
	if err != nil {
		return Instance{}, err
	}
	var body struct {
		FileCompression string  `json:"file_compression"`
		Limits          *Limits `json:"limits"`
		PoW             *PoW    `json:"pow"`
		Create          *struct {
			ID      string `json:"id"`
			IDToken string `json:"id_token"`
		} `json:"create"`
	}
	err = readJSON(response, &body)
	if err != nil {
		return Instance{}, err
	}
	if body.Limits == nil || body.PoW == nil || body.Create == nil {
		return Instance{}, ErrResponse
	}
	id, err := core.ParseID(body.Create.ID)
	if err != nil || body.Create.IDToken == "" || body.PoW.Difficulty < 0 ||
		body.PoW.Difficulty > proofofwork.MaxPoWDifficulty {
		return Instance{}, ErrResponse
	}
	return Instance{
		Limits:          *body.Limits,
		PoW:             *body.PoW,
		Create:          Issued{ID: id, IDToken: body.Create.IDToken},
		FileCompression: body.FileCompression,
	}, nil
}

func (c *Client) Create(ctx context.Context, request *CreateRequest) (time.Time, error) {
	fields := map[string]any{
		"id":         request.ID.String(),
		"id_token":   request.IDToken,
		"read_key":   core.Encode(request.ReadKey[:]),
		"manage_key": core.Encode(request.ManageKey[:]),
		"kdf_id":     int(request.KDF),
		"opens_max":  int(request.OpensMax),
		"ttl_s":      int(request.TTLSeconds),
		"size":       len(request.Object),
	}
	if request.Verifier != nil {
		fields["verifier_key"] = core.Encode(request.Verifier[:])
	}
	header, err := json.Marshal(fields)
	if err != nil {
		return time.Time{}, err
	}
	headers := map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": core.Encode(header)}
	if request.PoWToken != "" {
		headers["X-Nylzo-Pow"] = request.PoWToken
	}
	timeout := transferTime(int64(len(request.Object)))
	response, err := c.do(ctx, http.MethodPost, "/api/v1/messages", headers, request.Object, timeout)
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

func (c *Client) Metadata(ctx context.Context, id core.ID, read *[core.SeedLen]byte) (Metadata, error) {
	request := signedRequest{method: http.MethodGet, path: messagePath(id), purpose: signing.PurposeRead, seed: read}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	err = readJSON(response, &metadata)
	if err != nil {
		return Metadata{}, err
	}
	if !knownState(metadata.State) ||
		!timesValid(metadata.CreatedAt, metadata.ExpiresAt, metadata.FirstOpenedAt, metadata.EndedAt) {
		return Metadata{}, ErrResponse
	}
	return metadata, nil
}

func (c *Client) Status(ctx context.Context, id core.ID, manage *[core.SeedLen]byte) (Status, error) {
	request := signedRequest{
		method:  http.MethodGet,
		path:    messagePath(id) + "/status",
		purpose: signing.PurposeManage,
		seed:    manage,
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return Status{}, err
	}
	var status Status
	err = readJSON(response, &status)
	if err != nil {
		return Status{}, err
	}
	if !knownState(status.State) ||
		!timesValid(status.CreatedAt, status.ExpiresAt, status.FirstOpenedAt, status.EndedAt) {
		return Status{}, ErrResponse
	}
	return status, nil
}

func (c *Client) Cancel(ctx context.Context, id core.ID, manage *[core.SeedLen]byte) error {
	return c.end(ctx, messagePath(id)+"/cancel", signing.PurposeManage, manage)
}

func (c *Client) Burn(ctx context.Context, id core.ID, read *[core.SeedLen]byte) error {
	return c.end(ctx, messagePath(id)+"/burn", signing.PurposeRead, read)
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
		path:    "/api/v1/abuse",
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
	if !IsKind(err, "not_found") {
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
		headers["X-Nylzo-Verifier"] = verifier.VerifierHeader()
	}
	maps.Copy(headers, request.headers)
	return c.do(ctx, request.method, request.path, headers, request.body, timeout)
}

func (c *Client) challenge(ctx context.Context) ([signing.ChallengeLen]byte, error) {
	response, err := c.do(ctx, http.MethodGet, "/api/v1/challenge", nil, nil, shortTimeout)
	if err != nil {
		return [signing.ChallengeLen]byte{}, err
	}
	var body struct {
		Challenge string `json:"challenge"`
	}
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

func messagePath(id core.ID) string {
	return "/api/v1/messages/" + id.String()
}
