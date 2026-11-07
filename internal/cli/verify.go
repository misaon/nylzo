package cli

import (
	"bytes"
	"cmp"
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
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	maxManifestBytes = 64 << 10
	maxPageBytes     = 1 << 20
	maxVersionShown  = 64
	identity         = "identity"
	browserEncodings = "gzip, deflate, br, zstd"
)

var (
	errNoLocalClient = errors.New("this nylzo carries no web client to compare with; use a release build")
	transportHeaders = []string{"Date", "Content-Length", "Server", "Via", "Alt-Svc", "Connection", "Keep-Alive"}
	neverAllowed     = []string{
		"Refresh", "Set-Cookie", "Clear-Site-Data", "Link", "Content-Disposition", "Nel", "Report-To",
		"Reporting-Endpoints", "Speculation-Rules", "Service-Worker-Allowed", "Accept-Ch", "Critical-Ch",
	}
	encodings = []string{identity, browserEncodings}
)

// differenceError marks a response that differs from this nylzo, as opposed to one that could not be read.
type differenceError struct {
	message string
}

func (e *differenceError) Error() string {
	return e.message
}

func differs(format string, arguments ...any) error {
	return &differenceError{message: fmt.Sprintf(format, arguments...)}
}

func isDifference(err error) bool {
	_, differing := errors.AsType[*differenceError](err)
	return differing
}

// A server that answers with something other than this nylzo must not look unreachable.
func fetchFailed(where string, err error) error {
	if errors.Is(err, client.ErrRedirect) || errors.Is(err, client.ErrResponse) {
		return differs("%s: %s", where, explain(err))
	}
	return fmt.Errorf("%s: %s", where, explain(err))
}

type verified struct {
	pages int
	files int
}

type verifier struct {
	information buildinfo.Info
	api         *client.Client
	local       webclient.Build
	pages       webclient.Pages
	version     string
	allowed     map[string]bool
	// contactKnown is false when the transparency document could not be read; only the allowlist needs it.
	contactKnown bool
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
	}
	result, err := check.run(ctx)
	if isDifference(err) {
		return command.FailWith(environment, command.ExitMismatch, "%s", err)
	}
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

// comparison keeps searching after an error that is not a difference, so a failing page cannot hide a changed file.
type comparison struct {
	unfinished error
}

func (c *comparison) note(err error) error {
	if isDifference(err) {
		return err
	}
	c.unfinished = cmp.Or(c.unfinished, err)
	return nil
}

func (v *verifier) run(ctx context.Context) (verified, error) {
	served, err := v.release(ctx)
	if err != nil {
		return verified{}, err
	}
	var result verified
	var search comparison
	err = search.note(v.manifest(ctx, &served))
	if err != nil {
		return verified{}, err
	}
	if v.pages != nil {
		if result.pages, err = v.comparePages(ctx, &search); err != nil {
			return verified{}, err
		}
	}
	if result.files, err = v.compareAssets(ctx, &search); err != nil {
		return verified{}, err
	}
	return result, search.unfinished
}

func (v *verifier) comparePages(ctx context.Context, search *comparison) (int, error) {
	for _, page := range webclient.PagePaths {
		path := strings.Replace(page, "{id}", core.NewID().String(), 1)
		for _, encoding := range encodings {
			if err := search.note(v.page(ctx, path, encoding)); err != nil {
				return 0, err
			}
		}
	}
	return len(webclient.PagePaths), nil
}

func (v *verifier) compareAssets(ctx context.Context, search *comparison) (int, error) {
	for _, name := range slices.Sorted(maps.Keys(v.local.Assets)) {
		asset := v.local.Assets[name]
		for _, encoding := range encodings {
			if err := search.note(v.asset(ctx, name, &asset, encoding)); err != nil {
				return 0, err
			}
		}
	}
	return len(v.local.Assets), nil
}

// release reads the served manifest; without the version of this binary no file can be compared.
func (v *verifier) release(ctx context.Context) (webclient.Manifest, error) {
	var served webclient.Manifest
	fetched, err := v.api.Fetch(ctx, webclient.ManifestPath, identity, maxManifestBytes)
	if err != nil {
		return served, fetchFailed(webclient.ManifestPath, err)
	}
	if err = json.Unmarshal(fetched.Body, &served); err != nil {
		return served, differs("the server's client manifest is not valid JSON")
	}
	if served.Server != v.version {
		return served, fmt.Errorf(
			"the server runs nylzo %s and this binary is %s; verify with the nylzo release the server runs",
			shortened(served.Server),
			v.version,
		)
	}
	if served.PublicURL != "" {
		if err = webclient.CheckPublicURL(served.PublicURL); err != nil {
			return served, differs(
				"the server's client manifest names a public URL that this nylzo would refuse: %s",
				err,
			)
		}
	}
	return served, nil
}

func (v *verifier) manifest(ctx context.Context, served *webclient.Manifest) error {
	contact, contactErr := v.contact(ctx)
	if isDifference(contactErr) {
		return contactErr
	}
	v.contactKnown = contactErr == nil
	pages, err := v.local.Render(served.PublicURL, contact)
	if err != nil {
		return err
	}
	v.pages = pages
	servedJSON, _ := json.Marshal(served)
	expectedJSON, _ := json.Marshal(v.local.Manifest(v.version, served.PublicURL, v.pages, v.information))
	if !bytes.Equal(servedJSON, expectedJSON) {
		return differs("the server's client manifest differs from the web client of this nylzo")
	}
	return contactErr
}

// contact reads the operator's contact, which every page's Connection-Allowlist admits.
func (v *verifier) contact(ctx context.Context) (string, error) {
	fetched, err := v.api.Fetch(ctx, server.TransparencyPath, identity, maxManifestBytes)
	if err != nil {
		return "", fetchFailed(server.TransparencyPath, err)
	}
	contact, err := webclient.OperatorContact(fetched.Body)
	if err != nil {
		return "", differs("%s: %s", server.TransparencyPath, err)
	}
	return contact, nil
}

// page checks that the served page and its policy are byte for byte the ones this nylzo renders.
func (v *verifier) page(ctx context.Context, path, encoding string) error {
	where := describe(path, encoding)
	expected := v.pages.For(path)
	fetched, err := v.api.Fetch(ctx, path, encoding, maxPageBytes)
	if err != nil {
		return fetchFailed(where, err)
	}
	headersErr := v.headers(fetched.Header, "text/html; charset=utf-8", path, where)
	if isDifference(headersErr) {
		return headersErr
	}
	if fetched.Header.Get("Content-Security-Policy") != expected.Policy() {
		return differs("%s: the Content-Security-Policy differs from this nylzo", where)
	}
	var allowlistErr error
	if v.contactKnown {
		allowlistErr = allowlist(fetched.Header.Values("Connection-Allowlist"), expected.Allowlist(), where)
	}
	if isDifference(allowlistErr) {
		return allowlistErr
	}
	body, err := decoded(&fetched, encoding, int64(len(expected.Body())), where)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, expected.Body()) {
		return differs("%s: the page differs from the web client of this nylzo", where)
	}
	return cmp.Or(headersErr, allowlistErr)
}

func allowlist(values []string, expected, where string) error {
	switch {
	case len(values) == 1 && values[0] == expected:
		return nil
	case len(values) == 1 && values[0] == webclient.ConnectionAllowlist(""):
		return fmt.Errorf(
			"%s: the Connection-Allowlist leaves out the operator's contact from %s; a Caddy front needs the same NYLZO_CONTACT as the server",
			where,
			server.TransparencyPath,
		)
	default:
		return differs("%s: the Connection-Allowlist differs from this nylzo", where)
	}
}

func (v *verifier) asset(ctx context.Context, name string, asset *webclient.Asset, encoding string) error {
	path := "/assets/" + name
	where := describe(path, encoding)
	limit := int64(max(len(asset.Body), len(asset.Gzip))) + maxPageBytes
	fetched, err := v.api.Fetch(ctx, path, encoding, limit)
	if err != nil {
		return fetchFailed(where, err)
	}
	headersErr := v.headers(fetched.Header, asset.ContentType, path, where)
	if isDifference(headersErr) {
		return headersErr
	}
	if fetched.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP {
		return differs("%s: the headers differ from this nylzo", where)
	}
	body, err := decoded(&fetched, encoding, int64(len(asset.Body)), where)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, asset.Body) {
		return differs("%s: the file differs from the web client of this nylzo", where)
	}
	return headersErr
}

// headers requires every header this version sends exactly once and refuses any other unless it is allowed;
// a difference wins over a header that is merely unexpected.
func (v *verifier) headers(header http.Header, contentType, path, where string) error {
	expected := webclient.SecurityHeaders(path)
	expected["Content-Type"] = contentType
	expected["Vary"] = "Accept-Encoding"
	for name, value := range expected {
		if values := header.Values(name); len(values) != 1 || values[0] != value {
			return differs("%s: the %s header differs from this nylzo", where, name)
		}
	}
	if len(header.Values("Content-Security-Policy")) != 1 {
		return differs("%s: the Content-Security-Policy header differs from this nylzo", where)
	}
	var unexpected error
	for name, values := range header {
		err := v.unknownHeader(name, values, expected, contentType, where)
		if isDifference(err) {
			return err
		}
		unexpected = cmp.Or(unexpected, err)
	}
	return unexpected
}

// unknownHeader refuses a header this version never sends with this response, unless a proxy may add it.
func (v *verifier) unknownHeader(
	name string,
	values []string,
	expected map[string]string,
	contentType, where string,
) error {
	known := expected[name] != "" || name == "Content-Security-Policy" ||
		name == "Connection-Allowlist" && strings.HasPrefix(contentType, "text/html") ||
		slices.Contains(transportHeaders, name) || v.allowed[name]
	if name == "Content-Encoding" {
		known = len(values) == 1
	}
	switch {
	case known:
		return nil
	case name == "Content-Encoding" || name == "Connection-Allowlist" || slices.Contains(neverAllowed, name):
		return differs("%s: the server sent the %s header, which this nylzo never sends here", where, name)
	default:
		return fmt.Errorf(
			"%s: the server sent the %s header, which this nylzo never sends; if a proxy adds it, allow it with --allow-header",
			where,
			shortened(name),
		)
	}
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
			return nil, differs("%s: the server compressed a response that was asked for without compression", where)
		}
		reader, err := gzip.NewReader(bytes.NewReader(fetched.Body))
		if err != nil {
			return nil, differs("%s: the compressed response is damaged", where)
		}
		body, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if err != nil {
			return nil, differs("%s: the compressed response is damaged", where)
		}
		return body, nil
	default:
		return nil, differs("%s: the server used an encoding that this nylzo never sends", where)
	}
}

func describe(path, encoding string) string {
	if encoding == identity {
		return path
	}
	return path + " (as a browser asks for it)"
}
