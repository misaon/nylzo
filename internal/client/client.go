package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/misaon/nylzo/internal/loopback"
)

const (
	shortTimeout              = 30 * time.Second
	transferMargin            = 30 * time.Second
	minTransfer               = 120 * time.Second
	minTransferBytesPerSecond = 16000
	maxJSONBytes              = 64 << 10
	MaxFetchBytes             = 8 << 20
	userAgent                 = "nylzo-cli"
)

var (
	ErrInsecureServer = errors.New("client: the server must use https, except localhost")
	ErrServerURL      = errors.New(
		"client: the server address must be scheme://host[:port] without a path, query or fragment",
	)
	ErrResponse    = errors.New("client: the server sent a malformed response")
	ErrRedirect    = errors.New("client: the server answered with a redirect, which nylzo never follows")
	ErrReportLimit = errors.New(
		"client: the message already has its reports for the day, so the server did not store this one",
	)
)

var states = []string{"available", "opened", "expired", "cancelled", "deleted"}

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
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.Kind == kind
}

type Client struct {
	base string
	http *http.Client
}

func New(server string) (*Client, error) {
	base, err := Origin(server)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: minTransfer + transferMargin,
	}
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

func Origin(server string) (string, error) {
	parsed, err := url.Parse(server)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.ForceQuery || strings.Contains(server, "#") || (parsed.Path != "" && parsed.Path != "/") {
		return "", ErrServerURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch {
	case scheme == "https":
	case scheme == "http" && loopback.Host(parsed.Hostname()):
	case scheme == "http":
		return "", ErrInsecureServer
	default:
		return "", ErrServerURL
	}
	return scheme + "://" + strings.ToLower(parsed.Host), nil
}

func (c *Client) Origin() string {
	return c.base
}

type Fetched struct {
	Header http.Header
	Body   []byte
}

func (c *Client) Fetch(ctx context.Context, path, acceptEncoding string, limit int64) (Fetched, error) {
	headers := map[string]string{"Accept-Encoding": acceptEncoding}
	response, err := c.do(ctx, http.MethodGet, path, headers, nil, shortTimeout)
	if err != nil {
		return Fetched{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Fetched{}, &APIError{Status: response.StatusCode, Kind: "unexpected"}
	}
	limit = min(limit, MaxFetchBytes)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return Fetched{}, err
	}
	if int64(len(body)) > limit {
		return Fetched{}, ErrResponse
	}
	return Fetched{Header: response.Header, Body: body}, nil
}

func (c *Client) do(
	ctx context.Context,
	method, path string,
	headers map[string]string,
	body []byte,
	timeout time.Duration,
) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	var content io.Reader
	if body != nil {
		content = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, content)
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	response, err := c.http.Do(request)
	if err != nil {
		cancel()
		if errors.Is(err, ErrRedirect) {
			return nil, ErrRedirect
		}
		return nil, err
	}
	response.Body = cancelOnClose{ReadCloser: response.Body, cancel: cancel}
	if response.StatusCode >= http.StatusMultipleChoices {
		defer response.Body.Close()
		return nil, apiError(response)
	}
	return response, nil
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

func loopbackOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if address, parseErr := netip.ParseAddr(host); parseErr != nil || !loopback.Address(address) {
		return ErrInsecureServer
	}
	return nil
}

func apiError(response *http.Response) error {
	apiErr := &APIError{Status: response.StatusCode, Kind: "unexpected"}
	var body struct {
		Error        string `json:"error"`
		RetryAfter   *int64 `json:"retry_after"`
		AttemptsLeft *int   `json:"attempts_left"`
		State        string `json:"state"`
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes))
	if json.Unmarshal(data, &body) == nil && body.Error != "" {
		apiErr.Kind = body.Error
		if knownState(body.State) {
			apiErr.State = body.State
		}
		if body.AttemptsLeft != nil {
			apiErr.AttemptsLeft = *body.AttemptsLeft
		}
		if body.RetryAfter != nil && *body.RetryAfter >= 0 {
			apiErr.RetryAfter = time.Duration(*body.RetryAfter) * time.Second
		}
	}
	seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64)
	if err == nil && seconds >= 0 && apiErr.RetryAfter == 0 {
		apiErr.RetryAfter = time.Duration(seconds) * time.Second
	}
	return apiErr
}

func readJSON(response *http.Response, target any) error {
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxJSONBytes || json.Unmarshal(data, target) != nil {
		return ErrResponse
	}
	return nil
}

func drain(response *http.Response) error {
	defer response.Body.Close()
	_, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxJSONBytes))
	return err
}

func knownState(state string) bool {
	return slices.Contains(states, state)
}

func timesValid(times ...string) bool {
	for _, moment := range times {
		if _, err := time.Parse(time.RFC3339, moment); moment != "" && err != nil {
			return false
		}
	}
	return true
}

func transferTime(size int64) time.Duration {
	return max(minTransfer, time.Duration(size)*time.Second/minTransferBytesPerSecond) + transferMargin
}
