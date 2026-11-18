package localclient

import (
	"bufio"
	"bytes"
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

	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	markedAttachment = "invoice.pdf"
	stepTimeout      = 30 * time.Second
	savePage         = `<!doctype html><title>save</title><button type="button">Save</button><script>
document.querySelector("button").addEventListener("click", () => {
  const url = URL.createObjectURL(new Blob([new TextEncoder().encode("%PDF-1.7")], { type: "application/octet-stream" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = "` + markedAttachment + `";
  link.rel = "noopener";
  link.hidden = true;
  document.body.append(link);
  link.click();
  link.remove();
});
</script>`
)

// The server refuses Windows, so the page saves a file the way web/app/src/shared/files/save-file.ts does.
func TestEdgeMarksAFileSavedOnTheOpenAddressAsFromTheInternet(t *testing.T) {
	edge := findEdge(t)
	listeners, err := Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	page := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for name, value := range webclient.SecurityHeaders("", r.URL.Path) {
				w.Header().Set(name, value)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, savePage)
		}),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() { _ = listeners.Serve(page) }()
	t.Cleanup(func() { _ = page.Close() })
	pagePath, fragment := strings.ToLower(rand.Text()), strings.ToLower(rand.Text())
	address := "http://" + sessionHost(listeners.Port()) + "/m/" + pagePath
	downloads := t.TempDir()
	browser := startEdge(t, edge)
	browser.control.call(t, "Browser.setDownloadBehavior", map[string]any{
		"behavior":     "allow",
		"downloadPath": downloads,
	})
	tab := browser.newPage(t)
	tab.call(t, "Page.navigate", map[string]any{"url": address + "#" + fragment})
	tab.clickTheButton(t)
	mark := waitForMark(t, filepath.Join(downloads, markedAttachment))
	t.Logf("Zone.Identifier of a file saved from %s:\n%s", address, mark)
	if !bytes.Contains(mark, []byte("ZoneId=3")) {
		t.Fatal("the saved file is not marked as from the Internet zone")
	}
	if bytes.Contains(mark, []byte("ReferrerUrl=")) || bytes.Contains(mark, []byte(pagePath)) ||
		bytes.Contains(mark, []byte(fragment)) {
		t.Fatal("the mark names the page beyond its origin")
	}
}

func findEdge(t *testing.T) string {
	t.Helper()
	for _, variable := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		candidate := filepath.Join(os.Getenv(variable), "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatal("Microsoft Edge is not installed")
	return ""
}

// waitForMark polls the stream, since Edge renames the download to its final name before it writes the mark.
func waitForMark(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if mark, err := os.ReadFile(path + ":Zone.Identifier"); err == nil && bytes.Contains(mark, []byte("HostUrl=")) {
			return mark
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Edge left %s without a Zone.Identifier stream", path)
	return nil
}

type edgeBrowser struct {
	port    string
	control *devtools
}

func startEdge(t *testing.T, edge string) *edgeBrowser {
	t.Helper()
	profile := t.TempDir()
	process := exec.Command(edge,
		"--headless=new", "--remote-debugging-port=0", "--user-data-dir="+profile, "--lang=en-US",
		"--no-first-run", "--no-default-browser-check", "about:blank")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = process.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = process.Process.Kill()
		<-exited
	})
	deadline := time.Now().Add(stepTimeout)
	for time.Now().Before(deadline) {
		if active, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); err == nil {
			if port, path, found := strings.Cut(strings.TrimSpace(string(active)), "\n"); found {
				browser := &edgeBrowser{port: port, control: dialDevtools(t, "ws://127.0.0.1:"+port+path)}
				// Runs before the socket closes and before the kill, so Edge lets go of its profile first.
				t.Cleanup(func() { browser.close(exited) })
				return browser
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Edge opened no DevTools port")
	return nil
}

func (b *edgeBrowser) close(exited <-chan struct{}) {
	_ = b.control.connection.SetDeadline(time.Now().Add(stepTimeout))
	b.control.next++
	_ = b.control.send(fmt.Appendf(nil, `{"id":%d,"method":"Browser.close"}`, b.control.next))
	select {
	case <-exited:
	case <-time.After(stepTimeout):
	}
}

func (b *edgeBrowser) newPage(t *testing.T) *devtools {
	t.Helper()
	response, err := (&http.Client{Timeout: stepTimeout}).Do(
		mustRequest(t, http.MethodPut, "http://127.0.0.1:"+b.port+"/json/new"),
	)
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
	if err = connection.SetDeadline(time.Now().Add(stepTimeout)); err != nil {
		t.Fatal(err)
	}
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
	if err = d.connection.SetDeadline(time.Now().Add(stepTimeout)); err != nil {
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

// clickTheButton clicks as a user would, once the page has loaded its only button.
func (d *devtools) clickTheButton(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(stepTimeout)
	for time.Now().Before(deadline) {
		result := d.call(t, "Runtime.evaluate", map[string]any{
			"expression":    `document.querySelector("button")?.click() === undefined && document.querySelector("button") !== null`,
			"returnByValue": true,
			"userGesture":   true,
		})
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
	t.Fatal("the page showed no button")
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
