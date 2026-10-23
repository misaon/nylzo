package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/misaon/nylzo/internal/client"
)

func runStatus(ctx context.Context, env Env, args []string) int {
	fs := newFlags(env, "status")
	file := fs.String("manage-token-file", "", "read the manage token from this file (or "+envManageToken+", or stdin)")
	asJSON := fs.Bool("json", false, "print the status as JSON")
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	token, err := readManageToken(env, *file)
	if err != nil {
		return failf(env, "manage token: %s", explain(err))
	}
	defer token.wipe()
	c, err := client.New(token.origin)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	status, err := c.Status(ctx, token.id, &token.seed)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	if *asJSON {
		out, _ := json.Marshal(status)
		fmt.Fprintln(env.Stdout, string(out))
		return exitOK
	}
	fmt.Fprintf(env.Stdout, "state: %s\nopens: %d of %d used\n", status.State, status.OpensUsed, status.OpensMax)
	for _, row := range []struct{ label, value string }{
		{"created", status.CreatedAt}, {"expires", status.ExpiresAt}, {"first opened", status.FirstOpenedAt}, {"ended", status.EndedAt},
	} {
		if row.value != "" {
			fmt.Fprintf(env.Stdout, "%s: %s\n", row.label, row.value)
		}
	}
	fmt.Fprintf(env.Stdout, "failed passphrase attempts: %d\n", status.FailedPassphraseAttempts)
	return exitOK
}

func runCancel(ctx context.Context, env Env, args []string) int {
	fs := newFlags(env, "cancel")
	file := fs.String("manage-token-file", "", "read the manage token from this file (or "+envManageToken+", or stdin)")
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	token, err := readManageToken(env, *file)
	if err != nil {
		return failf(env, "manage token: %s", explain(err))
	}
	defer token.wipe()
	c, err := client.New(token.origin)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	if err := c.Cancel(ctx, token.id, &token.seed); err != nil {
		return failf(env, "%s", explain(err))
	}
	fmt.Fprintln(env.Stdout, "cancelled")
	return exitOK
}

func runBurn(ctx context.Context, env Env, args []string) int {
	fs := newFlags(env, "burn")
	linkFile := fs.String("link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	link, err := linkFromInput(env, *linkFile)
	if err != nil {
		return failf(env, "link: %s", explain(err))
	}
	defer link.wipe()
	c, err := client.New(link.origin)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	if err := c.Burn(ctx, link.id, &link.keys.R); err != nil {
		return failf(env, "%s", explain(err))
	}
	fmt.Fprintln(env.Stdout, "deleted")
	return exitOK
}
