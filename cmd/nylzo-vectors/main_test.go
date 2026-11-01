package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVectorCommandEntrypoint(t *testing.T) {
	if scenario := os.Getenv("NYLZO_TEST_VECTOR_ENTRYPOINT"); scenario != "" {
		os.Args = []string{"nylzo-vectors"}
		switch scenario {
		case "file":
			os.Args = append(os.Args, "-out", os.Getenv("NYLZO_TEST_VECTOR_FILE"))
		case "invalid":
			os.Args = append(os.Args, "unexpected-argument")
		default:
			// With no flags the command writes vectors to stdout.
		}
		flag.CommandLine = flag.NewFlagSet("nylzo-vectors", flag.ExitOnError)
		main()
		os.Exit(0)
	}
	for _, scenario := range []string{"stdout", "file", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "vectors.json")
			command := exec.Command(os.Args[0], "-test.run=^TestVectorCommandEntrypoint$")
			command.Env = append(
				os.Environ(),
				"NYLZO_TEST_VECTOR_ENTRYPOINT="+scenario,
				"NYLZO_TEST_VECTOR_FILE="+destination,
			)
			if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
				command.Env = append(command.Env, "GOCOVERDIR="+directory.Value.String())
			}
			output, err := command.CombinedOutput()
			if scenario == "invalid" {
				if err == nil || !strings.Contains(string(output), "Usage") {
					t.Fatalf("invalid arguments: %v\n%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("generate: %v\n%s", err, output)
			}
			if scenario == "file" {
				output, err = os.ReadFile(destination)
				if err != nil {
					t.Fatal(err)
				}
				info, statErr := os.Stat(destination)
				if statErr != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("vector file must be private: %v %v", info, statErr)
				}
			}
			if !json.Valid(output) {
				t.Fatalf("command did not emit JSON: %.100s", output)
			}
		})
	}
}
