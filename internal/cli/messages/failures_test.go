package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/share"
)

type serverAnswer struct {
	status int
	header http.Header
	body   []byte
}

func rewriting(match func(*http.Request) bool, change func(*serverAnswer)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !match(request) {
				next.ServeHTTP(writer, request)
				return
			}
			recorder := httptest.NewRecorder()
			next.ServeHTTP(recorder, request)
			answer := &serverAnswer{
				status: recorder.Code,
				header: recorder.Header().Clone(),
				body:   recorder.Body.Bytes(),
			}
			change(answer)
			maps.Copy(writer.Header(), answer.header)
			writer.Header().Del("Content-Length")
			writer.WriteHeader(answer.status)
			writer.Write(answer.body)
		})
	}
}

type interceptedWriter struct {
	http.ResponseWriter

	header      func(http.Header)
	body        func(writer http.ResponseWriter, data []byte) (int, error)
	wroteHeader bool
}

// Unwrap keeps the server's write deadlines working.
func (w *interceptedWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *interceptedWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.header(w.Header())
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *interceptedWriter) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body(w.ResponseWriter, data)
}

func intercepting(
	match func(*http.Request) bool,
	header func(http.Header),
	body func(http.ResponseWriter, []byte) (int, error),
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !match(request) {
				next.ServeHTTP(writer, request)
				return
			}
			next.ServeHTTP(&interceptedWriter{ResponseWriter: writer, header: header, body: body}, request)
		})
	}
}

func unchangedHeader(http.Header) {}

func unchangedBody(writer http.ResponseWriter, data []byte) (int, error) {
	return writer.Write(data)
}

func flippedFirstByte(writer http.ResponseWriter, data []byte) (int, error) {
	changed := bytes.Clone(data)
	changed[0] ^= 1
	return writer.Write(changed)
}

func droppedMidway(writer http.ResponseWriter, data []byte) (int, error) {
	controller := http.NewResponseController(writer)
	if _, err := writer.Write(data[:len(data)/2]); err != nil {
		return 0, err
	}
	if err := controller.Flush(); err != nil {
		return 0, err
	}
	connection, _, err := controller.Hijack()
	if err != nil {
		return 0, err
	}
	connection.Close()
	return 0, http.ErrHijacked
}

func refusing(match func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !match(request) {
				next.ServeHTTP(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte(`{"error":"internal"}`))
		})
	}
}

func failed(answer *serverAnswer) {
	answer.status = http.StatusInternalServerError
	answer.header = http.Header{"Content-Type": {"application/json"}}
	answer.body = []byte(`{"error":"internal"}`)
}

func changedJSON(change func(map[string]any)) func(*serverAnswer) {
	return func(answer *serverAnswer) {
		var document map[string]any
		if err := json.Unmarshal(answer.body, &document); err != nil {
			panic(err)
		}
		change(document)
		body, err := json.Marshal(document)
		if err != nil {
			panic(err)
		}
		answer.body = body
	}
}

func object(document map[string]any, name string) map[string]any {
	nested, ok := document[name].(map[string]any)
	if !ok {
		panic("the answer has no " + name)
	}
	return nested
}

func isPath(path string) func(*http.Request) bool {
	return func(request *http.Request) bool { return request.URL.Path == path }
}

func isAction(action string) func(*http.Request) bool {
	return func(request *http.Request) bool { return strings.HasSuffix(request.URL.Path, action) }
}

func asksForAnID(request *http.Request) bool {
	return request.URL.Path == api.InstancePath && request.Header.Get(api.ShareKeyHeaderName) != ""
}

func secondPlainInstance() func(*http.Request) bool {
	var seen atomic.Int32
	return func(request *http.Request) bool {
		return request.URL.Path == api.InstancePath && request.Header.Get(api.ShareKeyHeaderName) == "" &&
			seen.Add(1) > 1
	}
}

type secretTerminal struct {
	secret []byte
	err    error
}

func (secretTerminal) StdinIsTerminal() bool { return true }

func (secretTerminal) StdoutIsTerminal() bool { return true }

func (s secretTerminal) ReadSecret(string) ([]byte, error) { return bytes.Clone(s.secret), s.err }

type messageRun struct {
	stdin     string
	terminal  console.Terminal
	variables map[string]string
}

func runMessage(
	t *testing.T,
	run func(context.Context, *command.Environment, []string) int,
	input messageRun,
	arguments ...string,
) commandResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	terminal := input.terminal
	if terminal == nil {
		terminal = noTerminal{}
	}
	code := run(t.Context(), &command.Environment{
		Stdin:    strings.NewReader(input.stdin),
		Stdout:   &stdout,
		Stderr:   &stderr,
		Getenv:   func(name string) string { return input.variables[name] },
		Terminal: terminal,
		Version:  "test",
	}, arguments)
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestCreateRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	directory := t.TempDir()
	existing := filepath.Join(directory, "existing")
	if err := os.WriteFile(existing, []byte("taken"), 0o600); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(directory, "generated")
	for name, test := range map[string]struct {
		input     messageRun
		arguments []string
		code      int
		want      string
	}{
		"no server":             {messageRun{stdin: "text"}, nil, command.ExitUsage, "needs --server"},
		"an insecure server":    {messageRun{stdin: "text"}, []string{"--server", "http://nylzo.example"}, command.ExitError, "https"},
		"nothing to send":       {messageRun{}, []string{"--server", instance.url}, command.ExitError, errNothingToSend.Error()},
		"text that is no UTF-8": {messageRun{stdin: "\xff"}, []string{"--server", instance.url}, command.ExitError, errTextNotUTF8.Error()},
		"too much text": {
			messageRun{stdin: strings.Repeat("a", 100*1024+1)},
			[]string{"--server", instance.url},
			command.ExitError,
			"longer than nylzo accepts",
		},
		"a terminal that cannot be read": {
			messageRun{terminal: secretTerminal{err: console.ErrNoTerminal}},
			[]string{"--server", instance.url},
			command.ExitError,
			console.ErrNoTerminal.Error(),
		},
		"a missing file": {
			messageRun{},
			[]string{"--server", instance.url, "--from-file", filepath.Join(directory, "absent")},
			command.ExitError,
			"--from-file: the file does not exist",
		},
		"a directory": {
			messageRun{},
			[]string{"--server", instance.url, "--from-file", directory},
			command.ExitError,
			errFromFileTooLarge.Error(),
		},
		"a language without a generated passphrase": {
			messageRun{stdin: "text"},
			[]string{"--server", instance.url, "--passphrase-language", "cs"},
			command.ExitError,
			errLanguageNotGenerated.Error(),
		},
		"a generated passphrase beside a passphrase file": {
			messageRun{stdin: "text"},
			[]string{"--server", instance.url, "--generate-passphrase-file", generated, "--passphrase-file", existing},
			command.ExitError,
			errPassphraseSource.Error(),
		},
		"an unknown language": {
			messageRun{stdin: "text"},
			[]string{"--server", instance.url, "--generate-passphrase-file", generated, "--passphrase-language", "xx"},
			command.ExitError,
			"--passphrase-language or",
		},
		"too many opens": {
			messageRun{stdin: "text"},
			[]string{"--server", instance.url, "--opens", "0"},
			command.ExitError,
			"--opens must be between 1",
		},
		"a TTL of a fraction of a second": {
			messageRun{stdin: "text"},
			[]string{"--server", instance.url, "--ttl", "90500ms"},
			command.ExitError,
			"--ttl must be whole seconds",
		},
	} {
		result := runMessage(t, RunCreate, test.input, test.arguments...)
		if result.code != test.code || !strings.Contains(result.stderr, test.want) || result.stdout != "" {
			t.Errorf("%s: %+v", name, result)
		}
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("a refused create left the generated passphrase: %v", err)
	}
}

func TestCreateDiscardsTheGeneratedPassphraseWhenTheManageFileIsTaken(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	directory := t.TempDir()
	existing := filepath.Join(directory, "existing")
	if err := os.WriteFile(existing, []byte("taken"), 0o600); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(directory, "generated")
	result := runMessage(t, RunCreate, messageRun{stdin: "text"},
		"--server", instance.url, "--generate-passphrase-file", generated, "--manage-token-file", existing)
	if result.code != command.ExitError || !strings.Contains(result.stderr, "--manage-token-file") {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("the generated passphrase of an uncreated message stays: %v", err)
	}
}

func TestCreateSendsTypedTextAndAFileOfAnUnknownType(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	typed := runMessage(
		t,
		RunCreate,
		messageRun{terminal: secretTerminal{secret: []byte("typed text")}},
		"--server",
		instance.url,
	)
	if typed.code != command.ExitOK {
		t.Fatalf("typed text: %+v", typed)
	}
	link, _, _ := strings.Cut(typed.stdout, "\n")
	if read := runWith(t, link, "read", "--stdout"); read.code != command.ExitOK || read.stdout != "typed text\n" {
		t.Fatalf("read typed text: %+v", read)
	}
	path := filepath.Join(t.TempDir(), "data.nylzo-unknown")
	if err := os.WriteFile(path, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	attached := runMessage(t, RunCreate, messageRun{}, "--server", instance.url, "--from-file", path)
	if attached.code != command.ExitOK {
		t.Fatalf("a file of an unknown type: %+v", attached)
	}
	link, _, _ = strings.Cut(attached.stdout, "\n")
	paths := readPaths(t, runWith(t, link, "read", "--out", t.TempDir()))
	if len(paths) != 1 || mustRead(t, paths[0]) != "contents" {
		t.Fatalf("read the file: %v", paths)
	}
}

func TestCreateFailsWhenTheServerAnswersWrong(t *testing.T) {
	t.Parallel()
	unusable := core.Encode(bytes.Repeat([]byte{0xff}, opaque.RegistrationResponseLen))
	otherShare := core.Encode(bytes.Repeat([]byte{1}, share.WrappedLen))
	limit := func(name string, value int) func(*serverAnswer) {
		return changedJSON(func(document map[string]any) { object(document, "limits")[name] = value })
	}
	work := func(challenge string) func(*serverAnswer) {
		return changedJSON(func(document map[string]any) {
			document["pow"] = map[string]any{"required": true, "difficulty": 1, "challenge": challenge}
		})
	}
	for name, test := range map[string]struct {
		wrap      func(http.Handler) http.Handler
		arguments []string
		want      string
	}{
		"no id": {rewriting(asksForAnID, failed), nil, "the server failed"},
		"a share for another key": {
			rewriting(asksForAnID, changedJSON(func(document map[string]any) { object(document, "create")["share"] = otherShare })),
			nil,
			"",
		},
		"an unusable registration": {
			rewriting(asksForAnID, changedJSON(func(document map[string]any) {
				object(document, "create")["registration"] = unusable
			})),
			nil,
			"",
		},
		"a refused message":             {rewriting(isPath(api.MessagesPath), failed), nil, "the server failed"},
		"a smaller object limit":        {rewriting(isPath(api.InstancePath), limit("max_object_bytes", 10)), nil, errTooLargeForInstance.Error()},
		"a smaller text limit":          {rewriting(isPath(api.InstancePath), limit("max_text_bytes", 2)), nil, "the text is longer than 2 bytes"},
		"a smaller file limit":          {rewriting(isPath(api.InstancePath), limit("max_file_bytes", 2)), []string{"--from-file"}, "the file is larger than 2 bytes"},
		"work with a damaged challenge": {rewriting(isPath(api.InstancePath), work("!")), nil, "proof of work"},
		"work and then no instance": {func(next http.Handler) http.Handler {
			return rewriting(secondPlainInstance(), failed)(rewriting(asksForAnID, work("AAAA"))(next))
		}, nil, "the server failed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instance := serveThrough(t, test.wrap)
			arguments := []string{"--server", instance.url}
			if len(test.arguments) > 0 {
				path := filepath.Join(t.TempDir(), "attachment.bin")
				if err := os.WriteFile(path, []byte("three"), 0o600); err != nil {
					t.Fatal(err)
				}
				arguments = append(arguments, "--from-file", path)
			}
			result := runMessage(t, RunCreate, messageRun{stdin: "text"}, arguments...)
			if result.code != command.ExitError || !strings.Contains(result.stderr, test.want) || result.stdout != "" {
				t.Errorf("%+v", result)
			}
		})
	}
}

func TestCreateSendsWithoutWorkTheInstanceNoLongerAsksFor(t *testing.T) {
	t.Parallel()
	instance := serveThrough(t, rewriting(asksForAnID, changedJSON(func(document map[string]any) {
		document["pow"] = map[string]any{"required": true, "difficulty": 1, "challenge": "AAAA"}
	})))
	if result := runMessage(
		t,
		RunCreate,
		messageRun{stdin: "text"},
		"--server",
		instance.url,
	); result.code != command.ExitOK {
		t.Fatalf("%+v", result)
	}
}

func TestSealingRefusesWhatTheProtocolForbids(t *testing.T) {
	t.Parallel()
	serverKeys, err := opaque.DeriveServerKeys(bytes.Repeat([]byte{1}, opaque.SeedLen), new([opaque.OPRFSeedLen]byte))
	if err != nil {
		t.Fatal(err)
	}
	text := "sealed"
	valid := envelope.Policy{OpensMax: 1, TTLSeconds: 3600, Deadline: 1797000000}
	for name, test := range map[string]struct {
		kdf    core.KDF
		share  *[core.ShareLen]byte
		policy envelope.Policy
		want   error
	}{
		"an unknown profile": {core.KDF(9), new([core.ShareLen]byte), valid, core.ErrKDF},
		"no expiry share":    {core.KDFNone, nil, valid, keys.ErrShare},
		"no opens":           {core.KDFNone, new([core.ShareLen]byte), envelope.Policy{TTLSeconds: 3600, Deadline: 1797000000}, nil},
	} {
		secret := core.NewSecret()
		started, startErr := startRegistration(secret, core.KDFNone, "")
		if startErr != nil {
			t.Fatal(startErr)
		}
		response, responseErr := opaque.CreateRegistrationResponse(started.request[:], &serverKeys, []byte("message"))
		if responseErr != nil {
			t.Fatal(responseErr)
		}
		_, err = sealWithDerivedKeys(&sealInput{
			id:           core.NewID(),
			secret:       secret,
			manage:       core.NewSecret(),
			share:        test.share,
			kdf:          test.kdf,
			registration: started,
			response:     &response,
			policy:       test.policy,
			message:      &envelope.Payload{Meta: envelope.Meta{Text: &text}},
		})
		if err == nil || test.want != nil && !errors.Is(err, test.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAGeneratedPassphraseNeedsAWordList(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "passphrase")
	if generated, err := writeGeneratedPassphrase(path, "xx"); !errors.Is(err, keys.ErrUnknownPassphraseLanguage) ||
		generated != nil {
		t.Fatalf("%+v, %v", generated, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a refused passphrase was written: %v", err)
	}
}

func TestCreateRemovesTheManageFileOfAMessageTheServerRefused(t *testing.T) {
	t.Parallel()
	instance := serveThrough(t, rewriting(isPath(api.MessagesPath), failed))
	manage := filepath.Join(t.TempDir(), "manage")
	result := runMessage(
		t,
		RunCreate,
		messageRun{stdin: "text"},
		"--server",
		instance.url,
		"--manage-token-file",
		manage,
	)
	if result.code != command.ExitError {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(manage); !os.IsNotExist(err) {
		t.Fatalf("the manage file of a refused message stays: %v", err)
	}
}

func TestCreateRefusesWhatOPAQUEOrTheMetaCannotCarry(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	long := make([]byte, keys.MaxPassphraseBytes)
	_, _ = rand.NewChaCha8([32]byte{}).Read(long)
	typed := secretTerminal{secret: []byte(core.Encode(long))}
	for name, test := range map[string]struct {
		input     messageRun
		arguments []string
		want      string
	}{
		"a typed passphrase longer than OPAQUE takes": {
			messageRun{terminal: typed},
			[]string{"--ask-passphrase"},
			"the passphrase is longer than",
		},
		"text whose escapes outgrow the meta": {messageRun{stdin: strings.Repeat(`"`, envelope.MaxTextBytes)}, nil, ""},
	} {
		result := runMessage(t, RunCreate, test.input, append([]string{"--server", instance.url}, test.arguments...)...)
		if result.code != command.ExitError || !strings.Contains(result.stderr, test.want) {
			t.Errorf("%s: %+v", name, result)
		}
	}
}

func TestReadRefusesALinkItCannotUse(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t)
	link := createText(t, instance, "never read")
	_, fragment, _ := strings.Cut(link, "#")
	for name, test := range map[string]struct {
		input     messageRun
		arguments []string
		want      string
	}{
		"a link of a plain HTTP server":       {messageRun{stdin: "http://nylzo.example/r#" + fragment}, nil, "https"},
		"a link whose address does not parse": {messageRun{stdin: "https://[nylzo.example/r#" + fragment}, nil, "missing ']'"},
		"a link typed on the terminal of another server": {
			messageRun{terminal: secretTerminal{secret: []byte("http://nylzo.example/r#" + fragment)}},
			nil,
			"https",
		},
		"a missing link file": {
			messageRun{},
			[]string{"--link-file", filepath.Join(t.TempDir(), "absent")},
			"the file does not exist",
		},
	} {
		result := runMessage(t, RunRead, test.input, append([]string{"--stdout"}, test.arguments...)...)
		if result.code != command.ExitError || !strings.Contains(result.stderr, test.want) || result.stdout != "" {
			t.Errorf("%s: %+v", name, result)
		}
	}
}

func TestReadFailsWhenTheServerAnswersWrong(t *testing.T) {
	t.Parallel()
	otherShare := core.Encode(bytes.Repeat([]byte{1}, share.WrappedLen))
	for name, test := range map[string]struct {
		wrap func(http.Handler) http.Handler
		want string
	}{
		"no metadata": {rewriting(func(request *http.Request) bool {
			return request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, api.MessagesPath+"/")
		}, failed), "the server failed"},
		"no handshake": {rewriting(isAction(api.HandshakeAction), failed), "the server failed"},
		"no claim":     {rewriting(isAction(api.ClaimAction), failed), "the server failed"},
		"a claim without its share": {intercepting(isAction(api.ClaimAction), func(header http.Header) {
			header.Del(api.ShareHeaderName)
		}, unchangedBody), "changed"},
		"a share for another key": {intercepting(isAction(api.ClaimAction), func(header http.Header) {
			header.Set(api.ShareHeaderName, otherShare)
		}, unchangedBody), "changed"},
		"a changed object": {intercepting(isAction(api.ClaimAction), unchangedHeader, flippedFirstByte), "changed"},
		"a broken claim and failing downloads": {func(next http.Handler) http.Handler {
			broken := intercepting(isAction(api.ClaimAction), unchangedHeader, droppedMidway)
			return broken(rewriting(isAction(api.BlobAction), failed)(next))
		}, "the server failed"},
		"a broken claim of a message that is gone": {func(next http.Handler) http.Handler {
			broken := intercepting(isAction(api.ClaimAction), unchangedHeader, droppedMidway)
			return broken(rewriting(isAction(api.BlobAction), func(answer *serverAnswer) {
				answer.status = http.StatusGone
				answer.header = http.Header{"Content-Type": {"application/json"}}
				answer.body = []byte(`{"error":"gone","state":"opened"}`)
			})(next))
		}, "already opened"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instance := serveThrough(t, test.wrap)
			link := createText(t, instance, "never shown")
			result := runMessage(t, RunRead, messageRun{stdin: link}, "--stdout")
			if result.code != command.ExitError || !strings.Contains(result.stderr, test.want) || result.stdout != "" {
				t.Errorf("%+v", result)
			}
		})
	}
}

func TestReadWarnsWhenTheServerDoesNotConfirmTheRead(t *testing.T) {
	t.Parallel()
	instance := serveThrough(t, rewriting(isAction(api.AckAction), failed))
	link := createText(t, instance, "read but not confirmed")
	result := runMessage(t, RunRead, messageRun{stdin: link}, "--stdout")
	if result.code != command.ExitOK || result.stdout != "read but not confirmed\n" ||
		!strings.Contains(result.stderr, "the server did not confirm the read") {
		t.Fatalf("%+v", result)
	}
}

func TestReadFailsWhenNoneOfThePrintedTextArrives(t *testing.T) {
	t.Parallel()
	link := createText(t, newTestServer(t), "printed into a closed pipe")
	var stderr bytes.Buffer
	code := readWith(t, link, closedWriter{}, &stderr, "--stdout")
	if code != command.ExitError || !strings.Contains(stderr.String(), "part of the output could not be written") {
		t.Fatalf("exit %d, %s", code, stderr.String())
	}
}

func TestAClaimThatRanOutOfTimeAfterAStopSaysSo(t *testing.T) {
	t.Parallel()
	refused := errors.New("refused")
	timedOut, cancel := context.WithCancelCause(t.Context())
	cancel(errStopTimedOut)
	if err := claimError(timedOut, refused); !errors.Is(err, errStopTimedOut) {
		t.Fatalf("a claim stopped after its time: %v", err)
	}
	if err := claimError(t.Context(), refused); !errors.Is(err, refused) {
		t.Fatalf("a refused claim: %v", err)
	}
}

func TestASecondCtrlCAfterTheClaimEndsTheProcessByIt(t *testing.T) {
	t.Parallel()
	ended := make(chan os.Signal, 1)
	var stderr lockedBuffer
	out := t.TempDir()
	guard := &claimGuard{
		environment: &command.Environment{Stderr: &stderr},
		end:         func(received os.Signal) { ended <- received },
		out:         out,
	}
	if _, err := guard.hold(t.Context(), time.Hour); err != nil {
		t.Fatal(err)
	}
	guard.interrupt <- os.Interrupt
	guard.interrupt <- os.Interrupt
	select {
	case received := <-ended:
		if received != os.Interrupt {
			t.Fatalf("ended by %v", received)
		}
	case <-time.After(time.Minute):
		t.Fatal("a second Ctrl-C did not end read")
	}
	guard.release()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stderr.String(), out) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(stderr.String(), out) {
		t.Fatalf("the warning does not name where part of the message may stay: %q", stderr.String())
	}
}

type answeringTerminal struct {
	noTerminal

	mutex   *sync.Mutex
	answers []terminalAnswer
}

type terminalAnswer struct {
	text string
	err  error
}

func (a *answeringTerminal) ReadSecret(string) ([]byte, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	answer := a.answers[0]
	a.answers = a.answers[1:]
	return []byte(answer.text), answer.err
}

func answering(answers ...terminalAnswer) *answeringTerminal {
	return &answeringTerminal{mutex: new(sync.Mutex), answers: answers}
}

func TestAPassphraseComesFromOneSourceAndIsNeverEmpty(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	empty := filepath.Join(directory, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	strong := "correct horse battery staple"
	for name, test := range map[string]struct {
		terminal console.Terminal
		source   passphraseSource
		want     error
	}{
		"a file and the terminal": {noTerminal{}, passphraseSource{file: empty, ask: true}, errPassphraseSource},
		"an empty file":           {noTerminal{}, passphraseSource{file: empty}, errEmpty},
		"nothing typed":           {answering(terminalAnswer{}), passphraseSource{ask: true}, errEmpty},
		"no second typing": {
			answering(terminalAnswer{text: strong}, terminalAnswer{err: console.ErrNoTerminal}),
			passphraseSource{ask: true},
			console.ErrNoTerminal,
		},
	} {
		passphrase, err := createPassphrase(&command.Environment{
			Stderr:   io.Discard,
			Getenv:   func(string) string { return "" },
			Terminal: test.terminal,
		}, test.source)
		if !errors.Is(err, test.want) || passphrase != "" {
			t.Errorf("%s: %q, %v", name, passphrase, err)
		}
	}
}

func TestManagingFailsWithoutATokenOrAnAnswer(t *testing.T) {
	t.Parallel()
	instance := serveThrough(t, func(next http.Handler) http.Handler {
		return refusing(
			isAction(api.StatusAction),
		)(
			refusing(isAction(api.CancelAction))(refusing(isAction(api.BurnAction))(next)),
		)
	})
	var stdout bytes.Buffer
	created := RunCreate(t.Context(), &command.Environment{
		Stdin:    strings.NewReader("managed"),
		Stdout:   &stdout,
		Stderr:   io.Discard,
		Getenv:   func(string) string { return "" },
		Terminal: noTerminal{},
	}, []string{"--server", instance.url, "--print-manage-token"})
	lines := strings.SplitN(stdout.String(), "\n", 2)
	if created != command.ExitOK || len(lines) != 2 {
		t.Fatalf("create: %d %q", created, stdout.String())
	}
	link, token := lines[0], lines[1]
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphraseFile, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	protected := createText(t, instance, "protected", "--passphrase-file", passphraseFile)
	head, fragment, _ := strings.Cut(link, "#")
	parts := strings.Split(fragment, ".")
	parts[2] = strings.Map(func(character rune) rune {
		if character == 'A' {
			return 'B'
		}
		return 'A'
	}, parts[2][:1]) + parts[2][1:]
	unknown := head + "#" + strings.Join(parts, ".")
	unusableOrigin := formatManageToken("https://a%252541.example", core.NewID(), core.NewSecret())
	for name, test := range map[string]struct {
		run   func(context.Context, *command.Environment, []string) int
		input messageRun
		want  string
	}{
		"status from a terminal":      {RunStatus, messageRun{terminal: secretTerminal{}}, "--manage-token-file"},
		"status the server refused":   {RunStatus, messageRun{stdin: token}, "the server failed"},
		"cancel without a token":      {RunCancel, messageRun{}, "manage token"},
		"cancel the server refused":   {RunCancel, messageRun{stdin: token}, "the server failed"},
		"cancel an unusable origin":   {RunCancel, messageRun{stdin: unusableOrigin}, "server address"},
		"burn without a link":         {RunBurn, messageRun{}, "link"},
		"burn the server refused":     {RunBurn, messageRun{stdin: link}, "the server failed"},
		"burn without the passphrase": {RunBurn, messageRun{stdin: protected}, errPassphraseNeeded.Error()},
		"burn of an unknown message":  {RunBurn, messageRun{stdin: unknown}, "no such message"},
	} {
		result := runMessage(t, test.run, test.input)
		if result.code != command.ExitError || !strings.Contains(result.stderr, test.want) || result.stdout != "" {
			t.Errorf("%s: %+v", name, result)
		}
	}
}

func TestCreateRefusesAFileItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	t.Parallel()
	path := filepath.Join(t.TempDir(), "unreadable")
	if err := os.WriteFile(path, []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	result := runMessage(t, RunCreate, messageRun{}, "--server", newTestServer(t).url, "--from-file", path)
	if result.code != command.ExitError || !strings.Contains(result.stderr, "--from-file: the file is not readable") {
		t.Fatalf("%+v", result)
	}
}
