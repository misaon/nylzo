package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/textproto"
	"regexp"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
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
)

type verified struct {
	pages int
	files int
}

type verifier struct {
	client    *client.Client
	local     server.EmbeddedClient
	version   string
	publicURL string
	allowed   map[string]bool
	nonces    map[string]bool
}

func runVerifyClient(ctx context.Context, env Env, args []string) int {
	fs := newFlags(env, "verify-client")
	serverURL := fs.String("server", "", "server address such as https://nylzo.example (or "+envServer+")")
	allowed := map[string]bool{}
	fs.Func(
		"allow-header",
		"accept this response header, which a proxy in front of the server adds (repeatable)",
		func(name string) error {
			name = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
			if name == "" || slices.Contains(neverAllowed, name) {
				return errors.New("this header changes what a browser does and cannot be allowed")
			}
			allowed[name] = true
			return nil
		},
	)
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	if *serverURL == "" {
		*serverURL = env.Getenv(envServer)
	}
	if *serverURL == "" {
		fmt.Fprintln(env.Stderr, program+" verify-client needs --server or "+envServer)
		return exitUsage
	}
	c, err := client.New(*serverURL)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	local, err := env.webClient()
	if err != nil || !local.Built {
		return failf(env, "%s", errNoLocalClient)
	}
	v := &verifier{
		client:  c,
		local:   local,
		version: buildVersion(env.Version),
		allowed: allowed,
		nonces:  map[string]bool{},
	}
	result, err := v.run(ctx)
	if err != nil {
		return failf(env, "%s", err)
	}
	fmt.Fprintf(
		env.Stdout,
		"the server sends the web client of nylzo %s unchanged: %d pages and %d files checked\n",
		v.version,
		result.pages,
		result.files,
	)
	fmt.Fprintf(
		env.Stderr,
		"%s: this covers what the server sent to this command; a server can recognise it and send browsers something else\n",
		program,
	)
	return exitOK
}

func (v *verifier) run(ctx context.Context) (verified, error) {
	if err := v.manifest(ctx); err != nil {
		return verified{}, err
	}
	var out verified
	for _, page := range server.ClientPages {
		path := strings.Replace(page, "{id}", core.NewID().String(), 1)
		for _, encoding := range []string{identity, browserEncodings} {
			if err := v.page(ctx, path, encoding); err != nil {
				return verified{}, err
			}
		}
		out.pages++
	}
	for _, name := range slices.Sorted(maps.Keys(v.local.Assets)) {
		for _, encoding := range []string{identity, browserEncodings} {
			if err := v.asset(ctx, name, v.local.Assets[name], encoding); err != nil {
				return verified{}, err
			}
		}
		out.files++
	}
	return out, nil
}

func (v *verifier) manifest(ctx context.Context) error {
	fetched, err := v.client.Fetch(ctx, server.ClientManifestPath, identity, maxManifestBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", server.ClientManifestPath, explain(err))
	}
	var got server.ClientManifest
	if err := json.Unmarshal(fetched.Body, &got); err != nil {
		return errors.New("the server's client manifest is not valid JSON")
	}
	if got.Server != v.version {
		version := got.Server
		if len(version) > maxVersionShown {
			version = version[:maxVersionShown] + "…"
		}
		return fmt.Errorf(
			"the server runs nylzo %s and this binary is %s; verify with the nylzo release the server runs",
			version,
			v.version,
		)
	}
	if got.PublicURL != "" {
		if err := server.CheckPublicURL(got.PublicURL); err != nil {
			return fmt.Errorf("the server's client manifest names a public URL that this nylzo would refuse: %v", err)
		}
	}
	v.publicURL = got.PublicURL
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(v.local.Manifest(v.version, v.publicURL))
	if !bytes.Equal(gotJSON, wantJSON) {
		return errors.New("the server's client manifest differs from the web client of this nylzo")
	}
	return nil
}

func (v *verifier) page(ctx context.Context, path, encoding string) error {
	where := describe(path, encoding)
	want, err := v.local.Page(path, v.publicURL)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	fetched, err := v.client.Fetch(ctx, path, encoding, maxPageBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", where, explain(err))
	}
	if err := v.headers(fetched.Header, "text/html; charset=utf-8", path, where); err != nil {
		return err
	}
	csp := fetched.Header.Get("Content-Security-Policy")
	match := pageNonce.FindStringSubmatch(csp)
	if match == nil || len(match[1]) < minNonceLength || csp != server.PageCSP(match[1]) {
		return fmt.Errorf("%s: the Content-Security-Policy differs from this nylzo", where)
	}
	nonce := match[1]
	if v.nonces[nonce] {
		return fmt.Errorf("%s: the server sent the same nonce twice", where)
	}
	v.nonces[nonce] = true
	body, err := decoded(fetched, encoding, int64(len(want))+maxPageBytes, where)
	if err != nil {
		return err
	}
	if strings.Count(string(body), nonce) != strings.Count(want, server.NoncePlaceholder) {
		return fmt.Errorf("%s: the page does not use the nonce of its Content-Security-Policy everywhere", where)
	}
	if strings.ReplaceAll(string(body), nonce, server.NoncePlaceholder) != want {
		return fmt.Errorf("%s: the page differs from the web client of this nylzo", where)
	}
	return nil
}

func (v *verifier) asset(ctx context.Context, name string, asset server.EmbeddedAsset, encoding string) error {
	path := "/assets/" + name
	where := describe(path, encoding)
	fetched, err := v.client.Fetch(ctx, path, encoding, int64(max(len(asset.Body), len(asset.Gzip)))+maxPageBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", where, explain(err))
	}
	if err := v.headers(fetched.Header, asset.ContentType, path, where); err != nil {
		return err
	}
	if fetched.Header.Get("Content-Security-Policy") != server.AssetCSP {
		return fmt.Errorf("%s: the headers differ from this nylzo", where)
	}
	body, err := decoded(fetched, encoding, int64(len(asset.Body)), where)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, asset.Body) {
		return fmt.Errorf("%s: the file differs from the web client of this nylzo", where)
	}
	return nil
}

func (v *verifier) headers(header http.Header, contentType, path, where string) error {
	expected := server.SecurityHeaders(path)
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
		known := expected[name] != "" || name == "Content-Security-Policy" || slices.Contains(transportHeaders, name) ||
			v.allowed[name]
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

func shortened(name string) string {
	if len(name) > maxVersionShown {
		return name[:maxVersionShown] + "…"
	}
	return name
}

func decoded(fetched client.Fetched, encoding string, limit int64, where string) ([]byte, error) {
	switch fetched.Header.Get("Content-Encoding") {
	case "":
		return fetched.Body, nil
	case server.EncodingGzip:
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
	}
	return nil, fmt.Errorf("%s: the server used an encoding that this nylzo never sends", where)
}

func describe(path, encoding string) string {
	if encoding == identity {
		return path
	}
	return path + " (as a browser asks for it)"
}
