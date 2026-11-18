package messages

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core/api"
)

const (
	manageTokenFileUsage = "read the manage token from this file (or " + command.EnvironmentManageToken + ", or stdin)"
	linkFileUsage        = "read the link from this file (or " + command.EnvironmentLink + ", or stdin)"
)

func RunStatus(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "status")
	file := flags.String("manage-token-file", "", manageTokenFileUsage)
	asJSON := flags.Bool("json", false, "print the status as JSON")
	if code, done := command.ParseFlags(flags, environment, arguments); done {
		return code
	}
	token, server, err := manageClient(environment, *file)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer token.wipe()
	status, err := server.Status(ctx, token.id, &token.seed)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	if *asJSON {
		encoded, _ := json.Marshal(status)
		fmt.Fprintln(environment.Stdout, string(encoded))
		return command.ExitOK
	}
	printStatus(environment, &status)
	return command.ExitOK
}

func printStatus(environment *command.Environment, status *api.Status) {
	fmt.Fprintf(environment.Stdout, "state: %s\n", status.State)
	if status.EndedAt == "" {
		fmt.Fprintf(environment.Stdout, "opens: %d of %d used\n", status.OpensUsed, status.OpensMax)
	}
	for _, row := range []struct{ label, value string }{
		{"created", status.CreatedAt},
		{"expires", status.ExpiresAt},
		{"first opened", status.FirstOpenedAt},
		{"ended", status.EndedAt},
	} {
		if row.value != "" {
			fmt.Fprintf(environment.Stdout, "%s: %s\n", row.label, row.value)
		}
	}
	if status.EndedAt != "" {
		fmt.Fprintln(environment.Stdout, "opens and failed passphrase attempts: no longer kept")
		return
	}
	fmt.Fprintf(environment.Stdout, "failed passphrase attempts: %d\n", status.FailedPassphraseAttempts)
}

func RunCancel(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "cancel")
	file := flags.String("manage-token-file", "", manageTokenFileUsage)
	if code, done := command.ParseFlags(flags, environment, arguments); done {
		return code
	}
	token, server, err := manageClient(environment, *file)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer token.wipe()
	if err = server.Cancel(ctx, token.id, &token.seed); err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	fmt.Fprintln(environment.Stdout, "cancelled")
	return command.ExitOK
}

func manageClient(environment *command.Environment, file string) (*manageToken, *client.Client, error) {
	token, err := readManageToken(environment, file)
	if err != nil {
		return nil, nil, fmt.Errorf("manage token: %w", command.Explained(err))
	}
	server, err := client.New(token.origin)
	if err != nil {
		token.wipe()
		return nil, nil, command.Explained(err)
	}
	return &token, server, nil
}

// RunBurn checks the message before it asks for the passphrase, whose handshake the server needs to delete it (F05).
func RunBurn(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "burn")
	linkFile := flags.String("link-file", "", linkFileUsage)
	var source passphraseSource
	passphraseFlags(flags, &source)
	if code, done := command.ParseFlags(flags, environment, arguments); done {
		return code
	}
	link, server, err := linkClient(environment, *linkFile)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer link.wipe()
	metadata, err := server.Metadata(ctx, link.id, &link.readSeed)
	if err == nil {
		err = checkBurnable(link, &metadata)
	}
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	login, err := logIn(ctx, environment, server, link, source)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	defer login.Wipe()
	if err = server.Burn(ctx, link.id, &link.readSeed, &login); err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	fmt.Fprintln(environment.Stdout, "deleted")
	return command.ExitOK
}

func linkClient(environment *command.Environment, linkFile string) (*openLink, *client.Client, error) {
	link, err := linkFromInput(environment, linkFile)
	if err != nil {
		return nil, nil, fmt.Errorf("link: %w", command.Explained(err))
	}
	server, err := client.New(link.origin)
	if err != nil {
		link.wipe()
		return nil, nil, command.Explained(err)
	}
	return &link, server, nil
}
