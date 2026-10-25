package cli

import (
	"bufio"
	"context"
	"crypto/hpke"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
)

const (
	maxReportKeyFile = 4096
	maxReportsInput  = 32 << 20
)

// runReportKey writes the private key to a new file only, so it never passes through a terminal or a log.
func runReportKey(_ context.Context, environment *Environment, arguments []string) int {
	flags := newFlags(environment, "report-key")
	keyFile := flags.String("key-file", "", "write the private key to this new file")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s report-key --key-file PATH\n\nCreates the key pair that seals abuse reports. Keep the private key off the server and set the printed NYLZO_REPORT_KEY on the server.\n",
			program,
		)
	}
	if code, done := flagsDone(parseFlags(flags, environment, arguments)); done {
		return code
	}
	if *keyFile == "" {
		fmt.Fprintln(environment.Stderr, program+" report-key needs --key-file for the private key")
		return exitUsage
	}
	key, err := abuse.NewKeyPair()
	if err != nil {
		return failf(environment, "%s", err)
	}
	private, err := key.Bytes()
	if err != nil {
		return failf(environment, "%s", err)
	}
	if err = writeNewPrivateFile(*keyFile, core.Encode(private)+"\n"); err != nil {
		return failf(environment, "--key-file: the file already exists or cannot be written")
	}
	fmt.Fprintf(environment.Stdout, "NYLZO_REPORT_KEY=%s\n", core.Encode(key.PublicKey().Bytes()))
	return exitOK
}

func writeNewPrivateFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return err
	}
	_, err = file.WriteString(content)
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		os.Remove(path)
	}
	return err
}

func runReports(_ context.Context, environment *Environment, arguments []string) int {
	flags := newFlags(environment, "reports")
	keyFile := flags.String("key-file", "", "the private key file from report-key")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s reports --key-file PATH < DATA_DIR/abuse/*.sealed\n\nOpens the sealed abuse reports on stdin and prints one JSON object per report.\n",
			program,
		)
	}
	if code, done := flagsDone(parseFlags(flags, environment, arguments)); done {
		return code
	}
	if *keyFile == "" || environment.Terminal.StdinIsTerminal() {
		fmt.Fprintln(environment.Stderr, program+" reports needs --key-file and the sealed report files on stdin")
		return exitUsage
	}
	raw, err := readFileLimited(*keyFile, maxReportKeyFile)
	if err != nil {
		return failf(environment, "--key-file: %s", unreadable(err))
	}
	key, err := abuse.ParsePrivateKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return failf(environment, "--key-file: %s", err)
	}
	sealed, err := readLimited(environment.Stdin, maxReportsInput)
	if err != nil {
		return failf(environment, "give at most 32 MiB of sealed reports on stdin")
	}
	return printReports(environment, key, string(sealed))
}

func printReports(environment *Environment, key hpke.PrivateKey, sealed string) int {
	encoder := json.NewEncoder(environment.Stdout)
	failed := 0
	scanner := bufio.NewScanner(strings.NewReader(sealed))
	scanner.Buffer(nil, maxReportsInput)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		report, err := abuse.Open(key, line)
		if err == nil {
			err = encoder.Encode(report)
		}
		if err != nil {
			failed++
		}
	}
	if failed > 0 {
		return failf(environment, "%d reports could not be opened with this key", failed)
	}
	return exitOK
}
