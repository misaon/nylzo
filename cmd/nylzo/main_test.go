package main

import (
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandEntrypoint(t *testing.T) {
	if scenario := os.Getenv("NYLZO_TEST_ENTRYPOINT"); scenario != "" {
		os.Args = []string{"nylzo", scenario}
		main()
		return
	}
	for _, test := range []struct {
		argument string
		want     string
		failed   bool
	}{
		{"version", "dev", false},
		{"--help", "nylzo", false},
		{"unknown-command", "unknown", true},
	} {
		t.Run(test.argument, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestCommandEntrypoint$")
			command.Env = append(os.Environ(), "NYLZO_TEST_ENTRYPOINT="+test.argument)
			if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
				command.Env = append(command.Env, "GOCOVERDIR="+directory.Value.String())
			}
			output, err := command.CombinedOutput()
			if (err != nil) != test.failed || !strings.Contains(string(output), test.want) {
				t.Fatalf("entrypoint %s: %v\n%s", test.argument, err, output)
			}
		})
	}
}
