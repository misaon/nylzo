package console

import (
	"errors"
	"os"
	"testing"
)

func TestReadSecretRefusesWhatIsNotATerminal(t *testing.T) {
	t.Parallel()
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	for name, open := range map[string]func() (terminalFiles, error){
		"no terminal device": func() (terminalFiles, error) { return terminalFiles{}, os.ErrNotExist },
		"a pipe":             func() (terminalFiles, error) { return terminalFiles{input: input, output: output}, nil },
	} {
		system := &System{open: open, end: func(os.Signal) { t.Error("ended the process") }}
		if secret, readErr := system.ReadSecret("heslo: "); !errors.Is(readErr, ErrNoTerminal) || secret != nil {
			t.Errorf("%s: read %q, %v", name, secret, readErr)
		}
	}
}

func TestPipesAreNoTerminal(t *testing.T) {
	t.Parallel()
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	system := NewSystem(input, output)
	if system.StdinIsTerminal() || system.StdoutIsTerminal() {
		t.Fatal("a pipe counts as a terminal")
	}
}
