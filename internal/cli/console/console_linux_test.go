package console

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type pseudoTerminal struct {
	controller *os.File
	devicePath string
}

func openPseudoTerminal(t *testing.T) *pseudoTerminal {
	t.Helper()
	controller, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminals: %v", err)
	}
	t.Cleanup(func() { controller.Close() })
	descriptor := int(controller.Fd())
	if err = unix.IoctlSetPointerInt(descriptor, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(descriptor, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	return &pseudoTerminal{controller: controller, devicePath: "/dev/pts/" + strconv.Itoa(number)}
}

func (p *pseudoTerminal) openDevice(t *testing.T) *os.File {
	t.Helper()
	device, err := os.OpenFile(p.devicePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func (p *pseudoTerminal) system(t *testing.T, end func(os.Signal)) *System {
	t.Helper()
	return &System{open: func() (terminalFiles, error) {
		device := p.openDevice(t)
		return terminalFiles{input: device, output: device}, nil
	}, end: end}
}

func (p *pseudoTerminal) readUntil(t *testing.T, want string) string {
	t.Helper()
	var shown bytes.Buffer
	buffer := make([]byte, 256)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(shown.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the terminal showed %q, not %q", shown.String(), want)
		}
		read, err := p.controller.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		shown.Write(buffer[:read])
	}
	return shown.String()
}

type readResult struct {
	secret []byte
	err    error
}

func TestReadSecretReadsTheTerminalWithoutEcho(t *testing.T) {
	t.Parallel()
	terminal := openPseudoTerminal(t)
	device := terminal.openDevice(t)
	defer device.Close()
	stdio := NewSystem(device, device)
	if !stdio.StdinIsTerminal() || !stdio.StdoutIsTerminal() {
		t.Fatal("a pseudo terminal does not count as a terminal")
	}
	read := make(chan readResult, 1)
	go func() {
		secret, err := terminal.system(t, func(os.Signal) { t.Error("ended the process") }).ReadSecret("heslo: ")
		read <- readResult{secret, err}
	}()
	terminal.readUntil(t, "heslo: ")
	if _, err := terminal.controller.WriteString("tajne heslo\n"); err != nil {
		t.Fatal(err)
	}
	result := <-read
	if result.err != nil || string(result.secret) != "tajne heslo" {
		t.Fatalf("read %q, %v", result.secret, result.err)
	}
	if shown := terminal.readUntil(t, "\n"); strings.Contains(shown, "tajne") {
		t.Fatalf("the terminal echoed the secret: %q", shown)
	}
}

func TestAStopSignalWhileReadingASecretRestoresTheEcho(t *testing.T) {
	terminal := openPseudoTerminal(t)
	ended := make(chan os.Signal, 1)
	read := make(chan readResult, 1)
	go func() {
		secret, err := terminal.system(t, func(received os.Signal) { ended <- received }).ReadSecret("heslo: ")
		read <- readResult{secret, err}
	}()
	terminal.readUntil(t, "heslo: ")
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if received := <-ended; received != syscall.SIGTERM {
		t.Fatalf("ended by %v", received)
	}
	device := terminal.openDevice(t)
	defer device.Close()
	state, err := unix.IoctlGetTermios(int(device.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lflag&unix.ECHO == 0 {
		t.Fatal("the terminal still hides what is typed")
	}
	if _, err = terminal.controller.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	<-read
}
