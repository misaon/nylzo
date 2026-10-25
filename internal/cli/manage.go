package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const manageTokenFileUsage = "read the manage token from this file (or " + envManageToken + ", or stdin)"

func runStatus(ctx context.Context, env *Env, args []string) int {
	flags := newFlags(env, "status")
	file := flags.String("manage-token-file", "", manageTokenFileUsage)
	asJSON := flags.Bool("json", false, "print the status as JSON")
	if code, done := flagsDone(parseFlags(flags, env, args)); done {
		return code
	}
	token, api, err := manageClient(env, *file)
	if err != nil {
		return failf(env, "%s", err)
	}
	defer token.wipe()
	status, err := api.Status(ctx, token.id, &token.seed)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	if *asJSON {
		encoded, _ := json.Marshal(status)
		fmt.Fprintln(env.Stdout, string(encoded))
		return exitOK
	}
	printStatus(env, &status)
	return exitOK
}

func printStatus(env *Env, status *client.Status) {
	fmt.Fprintf(env.Stdout, "state: %s\n", status.State)
	if status.EndedAt == "" {
		fmt.Fprintf(env.Stdout, "opens: %d of %d used\n", status.OpensUsed, status.OpensMax)
	}
	for _, row := range []struct{ label, value string }{
		{"created", status.CreatedAt},
		{"expires", status.ExpiresAt},
		{"first opened", status.FirstOpenedAt},
		{"ended", status.EndedAt},
	} {
		if row.value != "" {
			fmt.Fprintf(env.Stdout, "%s: %s\n", row.label, row.value)
		}
	}
	if status.EndedAt != "" {
		fmt.Fprintln(env.Stdout, "opens and failed passphrase attempts: no longer kept")
		return
	}
	fmt.Fprintf(env.Stdout, "failed passphrase attempts: %d\n", status.FailedPassphraseAttempts)
}

func runCancel(ctx context.Context, env *Env, args []string) int {
	flags := newFlags(env, "cancel")
	file := flags.String("manage-token-file", "", manageTokenFileUsage)
	if code, done := flagsDone(parseFlags(flags, env, args)); done {
		return code
	}
	token, api, err := manageClient(env, *file)
	if err != nil {
		return failf(env, "%s", err)
	}
	defer token.wipe()
	if err = api.Cancel(ctx, token.id, &token.seed); err != nil {
		return failf(env, "%s", explain(err))
	}
	fmt.Fprintln(env.Stdout, "cancelled")
	return exitOK
}

func manageClient(env *Env, file string) (*manageToken, *client.Client, error) {
	token, err := readManageToken(env, file)
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

func runBurn(ctx context.Context, env *Env, args []string) int {
	flags := newFlags(env, "burn")
	linkFile := flags.String("link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	if code, done := flagsDone(parseFlags(flags, env, args)); done {
		return code
	}
	link, api, err := linkClient(env, *linkFile)
	if err != nil {
		return failf(env, "%s", err)
	}
	defer link.wipe()
	if err = api.Burn(ctx, link.id, &link.keys.R); err != nil {
		return failf(env, "%s", explain(err))
	}
	fmt.Fprintln(env.Stdout, "deleted")
	return exitOK
}

func linkClient(env *Env, linkFile string) (*openLink, *client.Client, error) {
	link, err := linkFromInput(env, linkFile)
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

func runReport(ctx context.Context, env *Env, args []string) int {
	flags := newFlags(env, "report")
	linkFile := flags.String("link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	reasonFile := flags.String(
		"reason-file",
		"",
		"read why the message should be removed from this file (optional; links and long codes are removed before sending)",
	)
	if code, done := flagsDone(parseFlags(flags, env, args)); done {
		return code
	}
	reason := ""
	if *reasonFile != "" {
		raw, err := readFileLimited(*reasonFile, maxSecretInput)
		if err != nil {
			return failf(env, "reason: %s", explain(err))
		}
		reason = strings.TrimSpace(string(raw))
	}
	if core.ReasonTooLong(reason) {
		fmt.Fprintf(
			env.Stderr,
			"%s report: the reason is longer than %d bytes after links and long codes are removed\n",
			program,
			core.MaxReasonBytes,
		)
		return exitUsage
	}
	link, api, err := linkClient(env, *linkFile)
	if err != nil {
		return failf(env, "%s", err)
	}
	defer link.wipe()
	if err = api.Report(ctx, link.id, &link.keys.R, reason); err != nil {
		return failf(env, "%s", explain(err))
	}
	fmt.Fprintln(env.Stdout, "the report was sent; a person reviews it, and it is kept only while the message exists")
	return exitOK
}
