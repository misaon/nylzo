package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core/report"
)

const manageTokenFileUsage = "read the manage token from this file (or " + envManageToken + ", or stdin)"

func runStatus(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "status")
	file := flags.String("manage-token-file", "", manageTokenFileUsage)
	asJSON := flags.Bool("json", false, "print the status as JSON")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	token, api, err := manageClient(environment, *file)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer token.wipe()
	status, err := api.Status(ctx, token.id, &token.seed)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	if *asJSON {
		encoded, _ := json.Marshal(status)
		fmt.Fprintln(environment.Stdout, string(encoded))
		return command.ExitOK
	}
	printStatus(environment, &status)
	return command.ExitOK
}

func printStatus(environment *command.Environment, status *client.Status) {
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

func runCancel(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "cancel")
	file := flags.String("manage-token-file", "", manageTokenFileUsage)
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	token, api, err := manageClient(environment, *file)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer token.wipe()
	if err = api.Cancel(ctx, token.id, &token.seed); err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	fmt.Fprintln(environment.Stdout, "cancelled")
	return command.ExitOK
}

func manageClient(environment *command.Environment, file string) (*manageToken, *client.Client, error) {
	token, err := readManageToken(environment, file)
	if err != nil {
		return nil, nil, fmt.Errorf("manage token: %s", explain(err))
	}
	api, err := client.New(token.origin)
	if err != nil {
		token.wipe()
		return nil, nil, errors.New(explain(err))
	}
	return &token, api, nil
}

func runBurn(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "burn")
	linkFile := flags.String("link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	link, api, err := linkClient(environment, *linkFile)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer link.wipe()
	if err = api.Burn(ctx, link.id, &link.keys.R); err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	fmt.Fprintln(environment.Stdout, "deleted")
	return command.ExitOK
}

func linkClient(environment *command.Environment, linkFile string) (*openLink, *client.Client, error) {
	link, err := linkFromInput(environment, linkFile)
	if err != nil {
		return nil, nil, fmt.Errorf("link: %s", explain(err))
	}
	api, err := client.New(link.origin)
	if err != nil {
		link.wipe()
		return nil, nil, errors.New(explain(err))
	}
	return &link, api, nil
}

func runReport(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "report")
	linkFile := flags.String("link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	reasonFile := flags.String(
		"reason-file",
		"",
		"read why the message should be removed from this file (optional; links and long codes are removed before sending)",
	)
	contactFlag := flags.String(
		"contact",
		"",
		"your email address, used only to confirm receipt and to tell you the decision (optional)",
	)
	goodFaith := flags.Bool(
		"good-faith",
		false,
		"state that you believe in good faith that the report is accurate and complete",
	)
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	contact, err := report.ParseContact(*contactFlag)
	if err != nil {
		fmt.Fprintln(
			environment.Stderr,
			command.Program+" report: the contact must be an email address such as name@example.com",
		)
		return command.ExitUsage
	}
	reason := ""
	if *reasonFile != "" {
		raw, readErr := command.ReadFileLimited(*reasonFile, maxSecretInput)
		if readErr != nil {
			return command.Failf(environment, "reason: %s", explain(readErr))
		}
		reason = strings.TrimSpace(string(raw))
	}
	if report.ReasonTooLong(reason) {
		fmt.Fprintf(
			environment.Stderr,
			"%s report: the reason is longer than %d bytes after links and long codes are removed\n",
			command.Program,
			report.MaxReasonBytes,
		)
		return command.ExitUsage
	}
	link, api, err := linkClient(environment, *linkFile)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer link.wipe()
	err = api.Report(
		ctx,
		link.id,
		&link.keys.R,
		&client.Report{Reason: reason, Contact: contact, GoodFaith: *goodFaith},
	)
	if errors.Is(err, client.ErrReportLimit) {
		return command.Failf(
			environment,
			"the message already has three reports today, so this one was not stored; a person reviews them, "+
				"and anything new can go to the contact on the /transparency page of the instance",
		)
	}
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	fmt.Fprintln(
		environment.Stdout,
		"the report was sent; a person reviews it, and it is kept only while the message exists",
	)
	return command.ExitOK
}
