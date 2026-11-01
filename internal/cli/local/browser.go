package local

import (
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

func browser(environment *command.Environment) func(string) error {
	if environment.Browser != nil {
		return environment.Browser
	}
	return openWithSystem
}

func openWithSystem(target string) error {
	name, arguments := systemOpener(target)
	//nolint:gosec // G204: the target is a loopback address or a file that this command wrote.
	opener := exec.Command(name, arguments...)
	if err := opener.Start(); err != nil {
		return err
	}
	go opener.Wait()
	return nil
}

// writeHandoffPage keeps the handoff address out of every process argument, where other accounts could read it.
func writeHandoffPage(address string) (string, func(), error) {
	directory, err := os.MkdirTemp("", "nylzo-open-")
	if err != nil {
		return "", nil, err
	}
	remove := func() { os.RemoveAll(directory) }
	path := filepath.Join(directory, "open.html")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, command.PrivateFileMode)
	if err != nil {
		remove()
		return "", nil, err
	}
	_, writeErr := fmt.Fprintf(file, handoffPage, html.EscapeString(address))
	if closeErr := file.Close(); writeErr != nil || closeErr != nil {
		remove()
		return "", nil, fmt.Errorf("the handoff page could not be written: %w", errors.Join(writeErr, closeErr))
	}
	return path, remove, nil
}
