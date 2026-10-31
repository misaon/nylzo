package operator

import (
	"context"
	"crypto/hpke"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
)

const (
	maxReportKeyFile = 4096
	maxReportsInput  = 32 << 20
)

// RunReportKey writes the private key to a new file only, so it never passes through a terminal or a log.
func RunReportKey(_ context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "report-key")
	keyFile := flags.String("key-file", "", "write the private key to this new file")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s report-key --key-file PATH\n\nCreates the key pair that seals abuse reports. Keep the private key off the server and set the printed NYLZO_REPORT_KEY on the server.\n",
			command.Program,
		)
	}
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if *keyFile == "" {
		fmt.Fprintln(environment.Stderr, command.Program+" report-key needs --key-file for the private key")
		return command.ExitUsage
	}
	key, err := abuse.NewKeyPair()
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	private, err := key.Bytes()
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	if err = writeNewPrivateFile(*keyFile, core.Encode(private)+"\n"); err != nil {
		return command.Failf(environment, "--key-file: the file already exists or cannot be written")
	}
	fmt.Fprintf(environment.Stdout, "NYLZO_REPORT_KEY=%s\n", core.Encode(key.PublicKey().Bytes()))
	return command.ExitOK
}

func writeNewPrivateFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, command.PrivateFileMode)
	if err != nil {
		return err
	}
	_, err = file.WriteString(content)
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		os.Remove(path)
	}
	return err
}

func RunReports(_ context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "reports")
	keyFile := flags.String("key-file", "", "the private key file from report-key")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s reports --key-file PATH < DATA_DIR/abuse/*.sealed\n\nOpens the sealed abuse reports on stdin and prints one JSON object per report.\n",
			command.Program,
		)
	}
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if *keyFile == "" || environment.Terminal.StdinIsTerminal() {
		fmt.Fprintln(
			environment.Stderr,
			command.Program+" reports needs --key-file and the sealed report files on stdin",
		)
		return command.ExitUsage
	}
	raw, err := command.ReadFileLimited(*keyFile, maxReportKeyFile)
	if err != nil {
		return command.Failf(environment, "--key-file: %s", err)
	}
	key, err := abuse.ParsePrivateKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return command.Failf(environment, "--key-file: %s", err)
	}
	sealed, err := command.ReadLimited(environment.Stdin, maxReportsInput)
	if err != nil {
		return command.Failf(environment, "give at most 32 MiB of sealed reports on stdin")
	}
	return printReports(environment, key, sealed)
}

func printReports(environment *command.Environment, key hpke.PrivateKey, sealed []byte) int {
	reports, err := abuse.Open(key, sealed)
	if err != nil {
		return command.Failf(
			environment,
			"the reports could not be opened with this key, or the input holds a partial report",
		)
	}
	encoder := json.NewEncoder(environment.Stdout)
	encoder.SetEscapeHTML(false)
	for i := range reports {
		if err = encoder.Encode(&reports[i]); err != nil {
			return command.Failf(environment, "%s", err)
		}
	}
	return command.ExitOK
}
