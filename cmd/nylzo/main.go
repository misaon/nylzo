package main

import (
	"os"

	"github.com/misaon/nylzo/internal/cli"
	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/console"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
	dirty   = ""
)

func main() {
	os.Exit(cli.Run(os.Args[1:], &command.Environment{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Getenv:   os.Getenv,
		Terminal: console.NewSystem(os.Stdin, os.Stdout),
		Version:  version,
		Commit:   commit,
		Date:     date,
		Dirty:    dirty,
	}))
}
