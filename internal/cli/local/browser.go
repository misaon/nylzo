package local

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/misaon/nylzo/internal/cli/command"
)

const handoffPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="referrer" content="no-referrer">` +
	`<meta http-equiv="refresh" content="0; url=%[1]s"><title>Nylzo</title></head>` +
	`<body><p><a href="%[1]s">Open the message in the verified Nylzo client</a></p></body></html>`

type handoffFile struct {
	path   string
	remove func()
}

func browser(environment *command.Environment) func(string) error {
	if environment.Browser != nil {
		return environment.Browser
	}
	return openWithSystem
}

func openWithSystem(target string) error {
	opener := systemOpener(target)
	//nolint:gosec // G204: the opener is a fixed system program and the target an address or file this command made.
	process := exec.CommandContext(context.Background(), opener[0], opener[1:]...)
	if err := process.Start(); err != nil {
		return err
	}
	go func() { _ = process.Wait() }()
	return nil
}

// writeHandoffFile keeps the handoff address out of every process argument, where other accounts could read it.
func writeHandoffFile(address string) (handoffFile, error) {
	directory, err := os.MkdirTemp("", "nylzo-open-")
	if err != nil {
		return handoffFile{}, err
	}
	written := handoffFile{
		path:   filepath.Join(directory, "open.html"),
		remove: func() { os.RemoveAll(directory) },
	}
	file, err := os.OpenFile(written.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, command.PrivateFileMode)
	if err != nil {
		written.remove()
		return handoffFile{}, err
	}
	_, writeErr := fmt.Fprintf(file, handoffPage, html.EscapeString(address))
	if closeErr := file.Close(); writeErr != nil || closeErr != nil {
		written.remove()
		return handoffFile{}, fmt.Errorf("the handoff page could not be written: %w", errors.Join(writeErr, closeErr))
	}
	return written, nil
}
