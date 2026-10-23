package main

import (
	"os"

	"github.com/misaon/nylzo/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Getenv:   os.Getenv,
		Terminal: cli.SystemTerminal(os.Stdin),
		Version:  version,
	}))
}
