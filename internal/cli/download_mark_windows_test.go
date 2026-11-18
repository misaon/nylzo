package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/server/config"
)

const markedAttachment = "invoice.pdf"

// A browser marks what it downloads itself, so only a real Edge shows whether an attachment opened through
// nylzo open carries the Internet zone mark that SmartScreen and Office check.
func TestAnAttachmentDownloadedThroughOpenCarriesTheInternetZoneMark(t *testing.T) {
	edge := findEdge(t)
	instance := newTestServer(t, func(settings *config.Server) { settings.MinFreeBytes = 0 })
	attachment := filepath.Join(t.TempDir(), markedAttachment)
	if err := os.WriteFile(attachment, []byte("%PDF-1.7 not a real invoice"), 0o600); err != nil {
		t.Fatal(err)
	}
	link, _ := createText(t, instance, "", "--from-file", attachment)
	handoff := startOpen(t, instance.url, link)
	downloads := t.TempDir()
	browser := startEdge(t, edge)
	dialDevtools(t, "ws://127.0.0.1:"+browser.port+browser.path).call(t, "Browser.setDownloadBehavior", map[string]any{
		"behavior": "allow", "downloadPath": downloads,
	})
	page := browser.newPage(t)
	page.call(t, "Page.navigate", map[string]any{"url": handoff})
	page.click(t, "Show message")
	page.click(t, "Download attachment")
	page.click(t, "Download anyway")
	saved := waitForFile(t, filepath.Join(downloads, markedAttachment))
	mark, err := os.ReadFile(saved + ":Zone.Identifier")
	if err != nil {
		t.Fatalf("the downloaded attachment has no Zone.Identifier stream: %v", err)
	}
	if !bytes.Contains(mark, []byte("ZoneId=3")) {
		t.Fatalf("the downloaded attachment is not marked as from the Internet zone:\n%s", mark)
	}
	t.Logf("Zone.Identifier:\n%s", mark)
}

func findEdge(t *testing.T) string {
	t.Helper()
	for _, variable := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		candidate := filepath.Join(os.Getenv(variable), "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("Microsoft Edge is not installed")
	return ""
}

func startOpen(t *testing.T, server, link string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	output, input := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer input.Close()
		runOpen(ctx, &command.Environment{
			Stdin: strings.NewReader(link), Stdout: input, Stderr: io.Discard,
			Getenv: func(string) string { return "" }, Terminal: noTerminal{},
		}, []string{"--no-browser", "--server", server})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil {
		t.Fatalf("nylzo open printed no address: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, output) }()
	return strings.TrimSpace(line)
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return path
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Edge saved no %s", path)
	return ""
}

type edgeBrowser struct {
	port, path string
}

func startEdge(t *testing.T, edge string) *edgeBrowser {
	t.Helper()
	profile := t.TempDir()
	process := exec.CommandContext(t.Context(), edge,
		"--headless=new", "--remote-debugging-port=0", "--user-data-dir="+profile, "--lang=en-US",
		"--no-first-run", "--no-default-browser-check", "about:blank")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if active, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); err == nil {
			if port, path, found := strings.Cut(strings.TrimSpace(string(active)), "\n"); found {
				return &edgeBrowser{port: port, path: path}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Edge opened no DevTools port")
	return nil
}

func (b *edgeBrowser) newPage(t *testing.T) *devtools {
	t.Helper()
	response, err := http.DefaultClient.Do(mustRequest(t, http.MethodPut, "http://127.0.0.1:"+b.port+"/json/new"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var target struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err = json.NewDecoder(response.Body).Decode(&target); err != nil {
		t.Fatal(err)
	}
	return dialDevtools(t, target.WebSocketDebuggerURL)
}

func mustRequest(t *testing.T, method, address string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, address, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

// devtools speaks the Chrome DevTools Protocol over a minimal WebSocket client: unfragmented text frames only.
type devtools struct {
	connection net.Conn
	reader     *bufio.Reader
	next       int
}

func dialDevtools(t *testing.T, address string) *devtools {
	t.Helper()
	target, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := new(net.Dialer).DialContext(t.Context(), "tcp", target.Host)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	fmt.Fprintf(connection, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		target.RequestURI(), target.Host, base64.StdEncoding.EncodeToString(nonce))
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("DevTools refused the WebSocket: %s", response.Status)
	}
	return &devtools{connection: connection, reader: reader}
}

func (d *devtools) call(t *testing.T, method string, parameters map[string]any) json.RawMessage {
	t.Helper()
	d.next++
	request, err := json.Marshal(map[string]any{"id": d.next, "method": method, "params": parameters})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.send(request); err != nil {
		t.Fatal(err)
	}
	for {
		message, readErr := d.receive()
		if readErr != nil {
			t.Fatalf("%s: %v", method, readErr)
		}
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(message, &reply) == nil && reply.ID == d.next {
			if reply.Error != nil {
				t.Fatalf("%s: %s", method, reply.Error)
			}
			return reply.Result
		}
	}
}

func (d *devtools) click(t *testing.T, name string) {
	t.Helper()
	script := fmt.Sprintf(`(() => {
		const button = [...document.querySelectorAll('button')].find((candidate) =>
			(candidate.getAttribute('aria-label') ?? candidate.textContent).trim() === %q &&
			!candidate.disabled && !candidate.closest('[inert]'));
		if (!button) return false;
		button.click();
		return true;
	})()`, name)
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		result := d.call(t, "Runtime.evaluate", map[string]any{"expression": script, "returnByValue": true})
		var evaluated struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(result, &evaluated) == nil && evaluated.Result.Value {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no button %q appeared", name)
}

func (d *devtools) send(payload []byte) error {
	header := []byte{0x81}
	switch {
	case len(payload) < 126:
		header = append(header, 0x80|byte(len(payload)))
	case len(payload) <= 0xffff:
		header = append(header, 0x80|126)
		header = binary.BigEndian.AppendUint16(header, uint16(len(payload)))
	default:
		header = append(header, 0x80|127)
		header = binary.BigEndian.AppendUint64(header, uint64(len(payload)))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	masked := make([]byte, len(payload))
	for i, value := range payload {
		masked[i] = value ^ mask[i%4]
	}
	_, err := d.connection.Write(append(append(header, mask...), masked...))
	return err
}

var errUnsupportedFrame = errors.New("unsupported WebSocket frame")

func (d *devtools) receive() ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(d.reader, header); err != nil {
		return nil, err
	}
	if header[0] != 0x81 || header[1]&0x80 != 0 {
		return nil, fmt.Errorf("%w: %x", errUnsupportedFrame, header)
	}
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(d.reader, extended); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(d.reader, extended); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(extended)
	default:
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(d.reader, payload)
	return payload, err
}
