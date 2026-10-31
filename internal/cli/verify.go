package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/textproto"
	"regexp"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	maxManifestBytes = 64 << 10
	maxPageBytes     = 1 << 20
	minNonceLength   = 22
	maxVersionShown  = 64
	identity         = "identity"
	browserEncodings = "gzip, deflate, br, zstd"
)

var (
	pageNonce        = regexp.MustCompile(`'nonce-([A-Za-z0-9+/=_-]+)'`)
	errNoLocalClient = errors.New("this nylzo carries no web client to compare with; use a release build")
	transportHeaders = []string{"Date", "Content-Length", "Server", "Via", "Alt-Svc", "Connection", "Keep-Alive"}
	neverAllowed     = []string{
		"Refresh", "Set-Cookie", "Clear-Site-Data", "Link", "Content-Disposition", "Nel", "Report-To",
		"Reporting-Endpoints", "Speculation-Rules", "Service-Worker-Allowed", "Accept-Ch", "Critical-Ch",
	}
	encodings = []string{identity, browserEncodings}
)

type verified struct {
	pages int
	files int
}

type verifier struct {
	information buildinfo.Info
	api         *client.Client
	local       webclient.Build
	version     string
	publicURL   string
	allowed     map[string]bool
	nonces      map[string]bool
}

type verifyOptions struct {
	server  string
	allowed map[string]bool
}

func verifyFlags(environment *command.Environment) (*flag.FlagSet, *verifyOptions) {
	options := &verifyOptions{allowed: map[string]bool{}}
	flags := command.NewFlags(environment, "verify-client")
	flags.StringVar(&options.server, "server", "", "server address such as https://nylzo.example (or "+envServer+")")
	flags.Func(
		"allow-header",
		"accept this response header, which a proxy in front of the server adds (repeatable)",
		func(name string) error {
			name = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
			if name == "" || slices.Contains(neverAllowed, name) {
				return errors.New("this header changes what a browser does and cannot be allowed")
			}
			options.allowed[name] = true
			return nil
		},
	)
	return flags, options
}

func runVerifyClient(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags, options := verifyFlags(environment)
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if options.server == "" {
		options.server = environment.Getenv(envServer)
	}
	if options.server == "" {
		fmt.Fprintln(environment.Stderr, command.Program+" verify-client needs --server or "+envServer)
		return command.ExitUsage
	}
	api, err := client.New(options.server)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	local, err := webClient(environment)
	if err != nil || !local.Built {
		return command.Failf(environment, "%s", errNoLocalClient)
	}
	check := &verifier{
		api:         api,
		information: environment.BuildInformation(),
		local:       local,
		version:     buildVersion(environment.Version),
		allowed:     options.allowed,
		nonces:      map[string]bool{},
	}
	result, err := check.run(ctx)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	fmt.Fprintf(
		environment.Stdout,
		"the server sends the web client of nylzo %s unchanged: %d pages and %d files checked\n",
		check.version,
		result.pages,
		result.files,
	)
	fmt.Fprintf(
		environment.Stderr,
		"%s: this covers what the server sent to this command; a server can recognise it and send browsers something else\n",
		command.Program,
	)
	return command.ExitOK
}

func (v *verifier) run(ctx context.Context) (verified, error) {
	if err := v.manifest(ctx); err != nil {
		return verified{}, err
	}
	var result verified
	for _, page := range webclient.PagePaths {
		path := strings.Replace(page, "{id}", core.NewID().String(), 1)
		for _, encoding := range encodings {
			if err := v.page(ctx, path, encoding); err != nil {
				return verified{}, err
			}
		}
		result.pages++
	}
	for _, name := range slices.Sorted(maps.Keys(v.local.Assets)) {
		asset := v.local.Assets[name]
		for _, encoding := range encodings {
			if err := v.asset(ctx, name, &asset, encoding); err != nil {
				return verified{}, err
			}
		}
		result.files++
	}
	return result, nil
}

func (v *verifier) manifest(ctx context.Context) error {
	fetched, err := v.api.Fetch(ctx, webclient.ManifestPath, identity, maxManifestBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", webclient.ManifestPath, explain(err))
	}
	var served webclient.Manifest
	if err = json.Unmarshal(fetched.Body, &served); err != nil {
		return errors.New("the server's client manifest is not valid JSON")
	}
	if served.Server != v.version {
		return fmt.Errorf(
			"the server runs nylzo %s and this binary is %s; verify with the nylzo release the server runs",
			shortened(served.Server),
			v.version,
		)
	}
	if served.PublicURL != "" {
		if err = webclient.CheckPublicURL(served.PublicURL); err != nil {
			return fmt.Errorf("the server's client manifest names a public URL that this nylzo would refuse: %w", err)
		}
	}
	v.publicURL = served.PublicURL
	servedJSON, _ := json.Marshal(served)
	expectedJSON, _ := json.Marshal(v.local.Manifest(v.version, v.publicURL, v.information))
	if !bytes.Equal(servedJSON, expectedJSON) {
		return errors.New("the server's client manifest differs from the web client of this nylzo")
	}
	return nil
}

// page checks the served page against the local template with a fresh nonce in every place it belongs.
func (v *verifier) page(ctx context.Context, path, encoding string) error {
	where := describe(path, encoding)
	expected, err := v.local.Page(path, v.publicURL)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	fetched, err := v.api.Fetch(ctx, path, encoding, maxPageBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", where, explain(err))
	}
	if headersErr := v.headers(fetched.Header, "text/html; charset=utf-8", path, where); headersErr != nil {
		return headersErr
	}
	nonce, err := v.freshNonce(fetched.Header.Get("Content-Security-Policy"), where)
	if err != nil {
		return err
	}
	body, err := decoded(&fetched, encoding, int64(len(expected))+maxPageBytes, where)
	if err != nil {
		return err
	}
	if strings.Count(string(body), nonce) != strings.Count(expected, webclient.NoncePlaceholder) {
		return fmt.Errorf("%s: the page does not use the nonce of its Content-Security-Policy everywhere", where)
	}
	if strings.ReplaceAll(string(body), nonce, webclient.NoncePlaceholder) != expected {
		return fmt.Errorf("%s: the page differs from the web client of this nylzo", where)
	}
	return nil
}

func (v *verifier) freshNonce(policy, where string) (string, error) {
	match := pageNonce.FindStringSubmatch(policy)
	if len(match) < 2 || len(match[1]) < minNonceLength || policy != webclient.PageCSP(match[1]) {
		return "", fmt.Errorf("%s: the Content-Security-Policy differs from this nylzo", where)
	}
	nonce := match[1]
	if v.nonces[nonce] {
		return "", fmt.Errorf("%s: the server sent the same nonce twice", where)
	}
	v.nonces[nonce] = true
	return nonce, nil
}

func (v *verifier) asset(ctx context.Context, name string, asset *webclient.Asset, encoding string) error {
	path := "/assets/" + name
	where := describe(path, encoding)
	limit := int64(max(len(asset.Body), len(asset.Gzip))) + maxPageBytes
	fetched, err := v.api.Fetch(ctx, path, encoding, limit)
	if err != nil {
		return fmt.Errorf("%s: %s", where, explain(err))
	}
	if headersErr := v.headers(fetched.Header, asset.ContentType, path, where); headersErr != nil {
		return headersErr
	}
	if fetched.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP {
		return fmt.Errorf("%s: the headers differ from this nylzo", where)
	}
	body, err := decoded(&fetched, encoding, int64(len(asset.Body)), where)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, asset.Body) {
		return fmt.Errorf("%s: the file differs from the web client of this nylzo", where)
	}
	return nil
}

// headers requires every header this version sends exactly once and refuses any other unless it is allowed.
func (v *verifier) headers(header http.Header, contentType, path, where string) error {
	expected := webclient.SecurityHeaders(path)
	expected["Content-Type"] = contentType
	expected["Vary"] = "Accept-Encoding"
	for name, value := range expected {
		if values := header.Values(name); len(values) != 1 || values[0] != value {
			return fmt.Errorf("%s: the %s header differs from this nylzo", where, name)
		}
	}
	if len(header.Values("Content-Security-Policy")) != 1 {
		return fmt.Errorf("%s: the Content-Security-Policy header differs from this nylzo", where)
	}
	for name, values := range header {
		known := expected[name] != "" || name == "Content-Security-Policy" ||
			slices.Contains(transportHeaders, name) || v.allowed[name]
		if name == "Content-Encoding" {
			known = len(values) == 1
		}
		if !known {
			return fmt.Errorf(
				"%s: the server sent the %s header, which this nylzo never sends; if a proxy adds it, allow it with --allow-header",
				where,
				shortened(name),
			)
		}
	}
	return nil
}

func shortened(text string) string {
	if len(text) > maxVersionShown {
		return text[:maxVersionShown] + "…"
	}
	return text
}

func decoded(fetched *client.Fetched, encoding string, limit int64, where string) ([]byte, error) {
	switch fetched.Header.Get("Content-Encoding") {
	case "":
		return fetched.Body, nil
	case webclient.EncodingGzip:
		if encoding == identity {
			return nil, fmt.Errorf("%s: the server compressed a response that was asked for without compression", where)
		}
		reader, err := gzip.NewReader(bytes.NewReader(fetched.Body))
		if err != nil {
			return nil, fmt.Errorf("%s: the compressed response is damaged", where)
		}
		body, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return nil, fmt.Errorf("%s: the compressed response is damaged", where)
		}
		return body, nil
	default:
		return nil, fmt.Errorf("%s: the server used an encoding that this nylzo never sends", where)
	}
}

func describe(path, encoding string) string {
	if encoding == identity {
		return path
	}
	return path + " (as a browser asks for it)"
}
