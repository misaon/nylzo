package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

const (
	shortTimeout   = 30 * time.Second
	transferMargin = 30 * time.Second
	minTransfer    = 120 * time.Second
	minTransferBPS = 16000
	maxJSONBytes   = 64 << 10
	MaxFetchBytes  = 8 << 20
	userAgent      = "nylzo-cli"
)

var (
	ErrInsecureServer = errors.New("client: the server must use https, except localhost")
	ErrServerURL      = errors.New("client: the server address must be scheme://host[:port] without a path, query or fragment")
	ErrResponse       = errors.New("client: the server sent a malformed response")
	ErrRedirect       = errors.New("client: the server answered with a redirect, which nylzo never follows")
)

type Client struct {
	base string
	http *http.Client
}

func New(server string) (*Client, error) {
	base, err := Origin(server)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true, ResponseHeaderTimeout: minTransfer + transferMargin}
	if strings.HasPrefix(base, "http://") {
		transport.Proxy = nil
		dialer := &net.Dialer{Timeout: shortTimeout, Control: loopbackOnly}
		transport.DialContext = dialer.DialContext
	}
	return &Client{base: base, http: &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrRedirect
		},
	}}, nil
}

func loopbackOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip, err := netip.ParseAddr(host); err != nil || !ip.Unmap().IsLoopback() {
		return ErrInsecureServer
	}
	return nil
}

func (c *Client) Origin() string {
	return c.base
}

func Origin(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.Contains(server, "#") || (u.Path != "" && u.Path != "/") {
		return "", ErrServerURL
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == "https":
	case scheme == "http" && loopback(u.Hostname()):
	case scheme == "http":
		return "", ErrInsecureServer
	default:
		return "", ErrServerURL
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

type APIError struct {
	Status       int
	Kind         string
	RetryAfter   time.Duration
	AttemptsLeft int
	State        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("client: server answered %d %s", e.Status, e.Kind)
}

func IsKind(err error, kind string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Kind == kind
}

type Limits struct {
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxTextBytes   int64 `json:"max_text_bytes"`
	MaxObjectBytes int64 `json:"max_object_bytes"`
	MaxTTLSeconds  int64 `json:"max_ttl_s"`
	DefaultTTL     int64 `json:"default_ttl_s"`
	MaxOpens       int   `json:"max_opens"`
	DefaultKDF     int   `json:"default_kdf_id"`
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
	Limits Limits
	PoW    PoW
	Create Issued
}

func (c *Client) Instance(ctx context.Context) (Instance, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/instance", nil, nil, shortTimeout)
	if err != nil {
		return Instance{}, err
	}
	var raw struct {
		Limits *Limits `json:"limits"`
		PoW    *PoW    `json:"pow"`
		Create *struct {
			ID      string `json:"id"`
			IDToken string `json:"id_token"`
		} `json:"create"`
	}
	if err := readJSON(resp, &raw); err != nil {
		return Instance{}, err
	}
	if raw.Limits == nil || raw.PoW == nil || raw.Create == nil {
		return Instance{}, ErrResponse
	}
	id, err := core.ParseID(raw.Create.ID)
	if err != nil || raw.Create.IDToken == "" || !core.KDF(raw.Limits.DefaultKDF).Valid() || raw.PoW.Difficulty < 0 || raw.PoW.Difficulty > core.MaxPoWDifficulty {
		return Instance{}, ErrResponse
	}
	return Instance{Limits: *raw.Limits, PoW: *raw.PoW, Create: Issued{ID: id, IDToken: raw.Create.IDToken}}, nil
}

func (c *Client) challenge(ctx context.Context) ([core.ChallengeLen]byte, error) {
	var out [core.ChallengeLen]byte
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/challenge", nil, nil, shortTimeout)
	if err != nil {
		return out, err
	}
	var raw struct {
		Challenge string `json:"challenge"`
	}
	if err := readJSON(resp, &raw); err != nil {
		return out, err
	}
	b, err := core.DecodeExact(raw.Challenge, core.ChallengeLen)
	if err != nil {
		return out, ErrResponse
	}
	copy(out[:], b)
	return out, nil
}

type CreateRequest struct {
	ID         core.ID
	IDToken    string
	ReadKey    [core.PublicKeyLen]byte
	Verifier   *[core.PublicKeyLen]byte
	ManageKey  [core.PublicKeyLen]byte
	KDF        core.KDF
	OpensMax   uint16
	TTLSeconds uint32
	Object     []byte
	PoWToken   string
}

func (c *Client) Create(ctx context.Context, r CreateRequest) (time.Time, error) {
	fields := map[string]any{
		"id": r.ID.String(), "id_token": r.IDToken, "read_key": core.Encode(r.ReadKey[:]), "manage_key": core.Encode(r.ManageKey[:]),
		"kdf_id": int(r.KDF), "opens_max": int(r.OpensMax), "ttl_s": int(r.TTLSeconds), "size": len(r.Object),
	}
	if r.Verifier != nil {
		fields["verifier_key"] = core.Encode(r.Verifier[:])
	}
	header, err := json.Marshal(fields)
	if err != nil {
		return time.Time{}, err
	}
	headers := map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": core.Encode(header)}
	if r.PoWToken != "" {
		headers["X-Nylzo-Pow"] = r.PoWToken
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/messages", headers, r.Object, transferTime(int64(len(r.Object))))
	if err != nil {
		return time.Time{}, err
	}
	var raw struct {
		ID        string `json:"id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := readJSON(resp, &raw); err != nil {
		return time.Time{}, err
	}
	expires, err := time.Parse(time.RFC3339, raw.ExpiresAt)
	if err != nil || raw.ID != r.ID.String() {
		return time.Time{}, ErrResponse
	}
	return expires, nil
}

type Metadata struct {
	State              string `json:"state"`
	CreatedAt          string `json:"created_at"`
	ExpiresAt          string `json:"expires_at"`
	OpensMax           int    `json:"opens_max"`
	OpensRemaining     int    `json:"opens_remaining"`
	TTLSeconds         int64  `json:"ttl_s"`
	PassphraseRequired bool   `json:"passphrase_required"`
	KDF                int    `json:"kdf_id"`
	Size               int64  `json:"size"`
	FirstOpenedAt      string `json:"first_opened_at"`
	EndedAt            string `json:"ended_at"`
}

var states = []string{"available", "opened", "expired", "cancelled", "deleted"}

func knownState(state string) bool {
	return slices.Contains(states, state)
}

func timesValid(times ...string) bool {
	for _, t := range times {
		if _, err := time.Parse(time.RFC3339, t); t != "" && err != nil {
			return false
		}
	}
	return true
}

func messagePath(id core.ID) string {
	return "/api/v1/messages/" + id.String()
}

func (c *Client) Metadata(ctx context.Context, id core.ID, read *[core.TokenLen]byte) (Metadata, error) {
	resp, err := c.signed(ctx, signedRequest{method: http.MethodGet, path: messagePath(id), purpose: core.PurposeRead, seed: read}, shortTimeout)
	if err != nil {
		return Metadata{}, err
	}
	var m Metadata
	if err := readJSON(resp, &m); err != nil {
		return Metadata{}, err
	}
	if !knownState(m.State) || !timesValid(m.CreatedAt, m.ExpiresAt, m.FirstOpenedAt, m.EndedAt) {
		return Metadata{}, ErrResponse
	}
	return m, nil
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

func (c *Client) Status(ctx context.Context, id core.ID, manage *[core.TokenLen]byte) (Status, error) {
	resp, err := c.signed(ctx, signedRequest{method: http.MethodGet, path: messagePath(id) + "/status", purpose: core.PurposeManage, seed: manage}, shortTimeout)
	if err != nil {
		return Status{}, err
	}
	var s Status
	if err := readJSON(resp, &s); err != nil {
		return Status{}, err
	}
	if !knownState(s.State) || !timesValid(s.CreatedAt, s.ExpiresAt, s.FirstOpenedAt, s.EndedAt) {
		return Status{}, ErrResponse
	}
	return s, nil
}

func (c *Client) Cancel(ctx context.Context, id core.ID, manage *[core.TokenLen]byte) error {
	return c.end(ctx, messagePath(id)+"/cancel", core.PurposeManage, manage)
}

func (c *Client) Burn(ctx context.Context, id core.ID, read *[core.TokenLen]byte) error {
	return c.end(ctx, messagePath(id)+"/burn", core.PurposeRead, read)
}

func (c *Client) end(ctx context.Context, path, purpose string, seed *[core.TokenLen]byte) error {
	resp, err := c.signed(ctx, signedRequest{method: http.MethodPost, path: path, purpose: purpose, seed: seed}, shortTimeout)
	if err != nil {
		return err
	}
	return drain(resp)
}

func (c *Client) Report(ctx context.Context, id core.ID, read *[core.TokenLen]byte, reason string) error {
	payload := map[string]string{"id": id.String()}
	if reason = core.RedactReason(reason); reason != "" {
		payload["reason"] = reason
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.signed(ctx, signedRequest{
		method: http.MethodPost, path: "/api/v1/abuse", purpose: core.PurposeRead, seed: read,
		headers: map[string]string{"Content-Type": "application/json"}, body: body,
	}, shortTimeout)
	if err != nil {
		return err
	}
	return drain(resp)
}

type Envelope struct {
	Policy core.Policy
	KDF    core.KDF
	Flags  int
	Object []byte
}

type Claimed struct {
	Lease string
	Envelope
}

func (c *Client) Claim(ctx context.Context, id core.ID, read *[core.TokenLen]byte, verifier *[core.TokenLen]byte, size int64, onLease func(string)) (Claimed, error) {
	resp, err := c.signed(ctx, signedRequest{method: http.MethodPost, path: messagePath(id) + "/claim", purpose: core.PurposeRead, seed: read, verifier: verifier}, transferTime(size))
	if err != nil {
		return Claimed{}, err
	}
	defer resp.Body.Close()
	lease := resp.Header.Get("X-Nylzo-Lease")
	if lease == "" {
		return Claimed{}, ErrResponse
	}
	if onLease != nil {
		onLease(lease)
	}
	env, err := readEnvelope(resp)
	if err != nil {
		return Claimed{Lease: lease}, err
	}
	return Claimed{Lease: lease, Envelope: env}, nil
}

func (c *Client) Blob(ctx context.Context, id core.ID, read *[core.TokenLen]byte, lease string, size int64) (Envelope, error) {
	resp, err := c.signed(ctx, signedRequest{method: http.MethodGet, path: messagePath(id) + "/blob", purpose: core.PurposeRead, seed: read, headers: map[string]string{"X-Nylzo-Lease": lease}}, transferTime(size))
	if err != nil {
		return Envelope{}, err
	}
	defer resp.Body.Close()
	return readEnvelope(resp)
}

func (c *Client) Ack(ctx context.Context, id core.ID, read *[core.TokenLen]byte, lease string) error {
	resp, err := c.signed(ctx, signedRequest{method: http.MethodPost, path: messagePath(id) + "/ack", purpose: core.PurposeRead, seed: read, headers: map[string]string{"X-Nylzo-Lease": lease}}, shortTimeout)
	if err != nil {
		return err
	}
	return drain(resp)
}

func readEnvelope(resp *http.Response) (Envelope, error) {
	env, err := decodePolicy(resp.Header.Get("X-Nylzo-Policy"))
	if err != nil {
		return Envelope{}, err
	}
	limit := core.ObjectLimit(core.MaxFileBytes)
	if resp.ContentLength > limit {
		return Envelope{}, ErrResponse
	}
	object, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Envelope{}, err
	}
	if int64(len(object)) > limit || (resp.ContentLength >= 0 && int64(len(object)) != resp.ContentLength) {
		return Envelope{}, ErrResponse
	}
	env.Object = object
	return env, nil
}

func decodePolicy(header string) (Envelope, error) {
	data, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil || header == "" {
		return Envelope{}, ErrResponse
	}
	var raw struct {
		OpensMax *int `json:"opens_max"`
		TTL      *int `json:"ttl_s"`
		KDF      *int `json:"kdf_id"`
		Flags    *int `json:"flags"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || raw.OpensMax == nil || raw.TTL == nil || raw.KDF == nil || raw.Flags == nil {
		return Envelope{}, ErrResponse
	}
	if *raw.OpensMax < 1 || *raw.OpensMax > 0xffff || *raw.TTL < 1 || int64(*raw.TTL) > 0xffffffff || !core.KDF(*raw.KDF).Valid() {
		return Envelope{}, ErrResponse
	}
	return Envelope{Policy: core.Policy{OpensMax: uint16(*raw.OpensMax), TTLSeconds: uint32(*raw.TTL)}, KDF: core.KDF(*raw.KDF), Flags: *raw.Flags}, nil
}

type signedRequest struct {
	method   string
	path     string
	purpose  string
	seed     *[core.TokenLen]byte
	verifier *[core.TokenLen]byte
	headers  map[string]string
	body     []byte
}

func (c *Client) signed(ctx context.Context, r signedRequest, timeout time.Duration) (*http.Response, error) {
	resp, err := c.signedOnce(ctx, r, timeout)
	if !IsKind(err, "not_found") {
		return resp, err
	}
	return c.signedOnce(ctx, r, timeout)
}

func (c *Client) signedOnce(ctx context.Context, r signedRequest, timeout time.Duration) (*http.Response, error) {
	challenge, err := c.challenge(ctx)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Authorization": core.SignRequest(r.seed, r.purpose, r.method, r.path, challenge, r.body).Authorization()}
	if r.verifier != nil {
		headers["X-Nylzo-Verifier"] = core.SignRequest(r.verifier, core.PurposeVerifier, r.method, r.path, challenge, nil).VerifierHeader()
	}
	maps.Copy(headers, r.headers)
	return c.do(ctx, r.method, r.path, headers, r.body, timeout)
}

type Fetched struct {
	Header http.Header
	Body   []byte
}

func (c *Client) Fetch(ctx context.Context, path, acceptEncoding string, limit int64) (Fetched, error) {
	resp, err := c.do(ctx, http.MethodGet, path, map[string]string{"Accept-Encoding": acceptEncoding}, nil, shortTimeout)
	if err != nil {
		return Fetched{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Fetched{}, &APIError{Status: resp.StatusCode, Kind: "unexpected"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, min(limit, MaxFetchBytes)+1))
	if err != nil {
		return Fetched{}, err
	}
	if int64(len(body)) > min(limit, MaxFetchBytes) {
		return Fetched{}, ErrResponse
	}
	return Fetched{Header: resp.Header, Body: body}, nil
}

func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, body []byte, timeout time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		if errors.Is(err, ErrRedirect) {
			return nil, ErrRedirect
		}
		return nil, err
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, apiError(resp)
	}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func apiError(resp *http.Response) error {
	out := &APIError{Status: resp.StatusCode, Kind: "unexpected"}
	var raw struct {
		Error        string `json:"error"`
		RetryAfter   *int64 `json:"retry_after"`
		AttemptsLeft *int   `json:"attempts_left"`
		State        string `json:"state"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes))
	if json.Unmarshal(data, &raw) == nil && raw.Error != "" {
		out.Kind = raw.Error
		if knownState(raw.State) {
			out.State = raw.State
		}
		if raw.AttemptsLeft != nil {
			out.AttemptsLeft = *raw.AttemptsLeft
		}
		if raw.RetryAfter != nil && *raw.RetryAfter >= 0 {
			out.RetryAfter = time.Duration(*raw.RetryAfter) * time.Second
		}
	}
	if seconds, err := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64); err == nil && seconds >= 0 && out.RetryAfter == 0 {
		out.RetryAfter = time.Duration(seconds) * time.Second
	}
	return out
}

func readJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxJSONBytes || json.Unmarshal(data, v) != nil {
		return ErrResponse
	}
	return nil
}

func drain(resp *http.Response) error {
	defer resp.Body.Close()
	_, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxJSONBytes))
	return err
}

func transferTime(size int64) time.Duration {
	return max(minTransfer, time.Duration(size)*time.Second/minTransferBPS) + transferMargin
}
