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

var pageNonce = regexp.MustCompile(`'nonce-([A-Za-z0-9+/=_-]+)'`)

var errNoLocalClient = errors.New("this nylzo carries no web client to compare with; use a release build")

type verified struct {
	pages int
	files int
}

func runVerifyClient(ctx context.Context, env Env, args []string) int {
	fs := newFlags(env, "verify-client")
	serverURL := fs.String("server", "", "server address such as https://nylzo.example (or "+envServer+")")
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
	version := buildVersion(env.Version)
	result, err := verifyClient(ctx, c, local, version)
	if err != nil {
		return failf(env, "%s", err)
	}
	fmt.Fprintf(env.Stdout, "the server sends the web client of nylzo %s unchanged: %d pages and %d files checked\n", version, result.pages, result.files)
	fmt.Fprintf(env.Stderr, "%s: this covers what the server sent to this computer; a targeted attack can still send other code to someone else\n", program)
	return exitOK
}

func verifyClient(ctx context.Context, c *client.Client, local server.EmbeddedClient, version string) (verified, error) {
	if err := verifyManifest(ctx, c, local.Manifest(version)); err != nil {
		return verified{}, err
	}
	var out verified
	for _, page := range server.ClientPages {
		path := strings.Replace(page, "{id}", core.NewID().String(), 1)
		for _, encoding := range []string{identity, browserEncodings} {
			if err := verifyPage(ctx, c, local.Index, path, encoding); err != nil {
				return verified{}, err
			}
		}
		out.pages++
	}
	for _, name := range slices.Sorted(maps.Keys(local.Assets)) {
		for _, encoding := range []string{identity, browserEncodings} {
			if err := verifyAsset(ctx, c, name, local.Assets[name], encoding); err != nil {
				return verified{}, err
			}
		}
		out.files++
	}
	return out, nil
}

func verifyManifest(ctx context.Context, c *client.Client, want server.ClientManifest) error {
	fetched, err := c.Fetch(ctx, server.ClientManifestPath, identity, maxManifestBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", server.ClientManifestPath, explain(err))
	}
	var got server.ClientManifest
	if err := json.Unmarshal(fetched.Body, &got); err != nil {
		return errors.New("the server's client manifest is not valid JSON")
	}
	if got.Server != want.Server {
		version := got.Server
		if len(version) > maxVersionShown {
			version = version[:maxVersionShown] + "…"
		}
		return fmt.Errorf("the server runs nylzo %s and this binary is %s; verify with the nylzo release the server runs", version, want.Server)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		return errors.New("the server's client manifest differs from the web client of this nylzo")
	}
	return nil
}

func verifyPage(ctx context.Context, c *client.Client, template, path, encoding string) error {
	where := describe(path, encoding)
	fetched, err := c.Fetch(ctx, path, encoding, maxPageBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", where, explain(err))
	}
	if err := sameHeaders(fetched, "text/html; charset=utf-8", where); err != nil {
		return err
	}
	csp := fetched.Header.Get("Content-Security-Policy")
	match := pageNonce.FindStringSubmatch(csp)
	if match == nil || len(match[1]) < minNonceLength || csp != server.PageCSP(match[1]) || len(fetched.Header.Values("Content-Security-Policy")) != 1 {
		return fmt.Errorf("%s: the Content-Security-Policy differs from this nylzo", where)
	}
	body, err := decoded(fetched, encoding, int64(len(template))+maxPageBytes, where)
	if err != nil {
		return err
	}
	nonce := match[1]
	if strings.Contains(template, server.NoncePlaceholder) && !bytes.Contains(body, []byte(nonce)) {
		return fmt.Errorf("%s: the page does not use the nonce of its Content-Security-Policy", where)
	}
	if strings.ReplaceAll(string(body), nonce, server.NoncePlaceholder) != template {
		return fmt.Errorf("%s: the page differs from the web client of this nylzo", where)
	}
	return nil
}

func verifyAsset(ctx context.Context, c *client.Client, name string, asset server.EmbeddedAsset, encoding string) error {
	path := "/assets/" + name
	where := describe(path, encoding)
	fetched, err := c.Fetch(ctx, path, encoding, int64(max(len(asset.Body), len(asset.Gzip)))+maxPageBytes)
	if err != nil {
		return fmt.Errorf("%s: %s", where, explain(err))
	}
	if err := sameHeaders(fetched, asset.ContentType, where); err != nil {
		return err
	}
	if fetched.Header.Get("Content-Security-Policy") != server.AssetCSP || fetched.Header.Get("Vary") != "Accept-Encoding" {
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

func sameHeaders(fetched client.Fetched, contentType, where string) error {
	if fetched.Header.Get("Content-Type") != contentType {
		return fmt.Errorf("%s: the Content-Type differs from this nylzo", where)
	}
	for name, value := range server.SecurityHeaders() {
		if values := fetched.Header.Values(name); len(values) != 1 || values[0] != value {
			return fmt.Errorf("%s: the %s header differs from this nylzo", where, name)
		}
	}
	return nil
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
