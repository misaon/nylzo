package operator

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/passkey"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	enrollmentTokenLength = 24
	// enrollmentLife is how long the operator has to open the link and create the passkey.
	enrollmentLife = 15 * time.Minute
	// enrollmentFragment starts the fragment of an enrollment link; the token after it never reaches a server log.
	enrollmentFragment = "#enroll="
	dateFormat         = "2006-01-02 15:04 UTC"
	// shownIDLength is how much of a credential id tells the passkeys of a list apart.
	shownIDLength = 6
)

var algorithmNames = map[passkey.Algorithm]string{
	passkey.ES256:   "ES256",
	passkey.EdDSA:   "Ed25519",
	passkey.MLDSA44: "ML-DSA-44",
	passkey.MLDSA65: "ML-DSA-65",
	passkey.MLDSA87: "ML-DSA-87",
}

// RunAdminEnroll opens the one enrollment of the admin area and prints its link, which works once.
func RunAdminEnroll(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runAdmin(ctx, environment, arguments, &adminCommand{
		name: "admin-enroll",
		help: "Prints a link that adds one passkey to the admin area of this server. The link works once and for " +
			enrollmentLife.String() + "; a newer link replaces it. Open it in the browser that holds, or will " +
			"hold, the passkey.",
		run: func(ctx context.Context, store *adminstore.Store, settings *config.Server) error {
			token := make([]byte, enrollmentTokenLength)
			rand.Read(token)
			defer clear(token)
			expires := time.Now().Add(enrollmentLife)
			if err := store.OpenEnrollment(ctx, adminstore.HashToken(token), expires); err != nil {
				return err
			}
			fmt.Fprintln(environment.Stdout, adminAddress(settings)+enrollmentFragment+core.Encode(token))
			fmt.Fprintf(environment.Stderr, "the link works once, until %s\n", expires.UTC().Format(dateFormat))
			return nil
		},
	})
}

// RunAdminPasskeys lists the passkeys that can sign in to the admin area.
func RunAdminPasskeys(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runAdmin(ctx, environment, arguments, &adminCommand{
		name: "admin-passkeys",
		help: "Lists the passkeys that can sign in to the admin area of this server.",
		run: func(ctx context.Context, store *adminstore.Store, _ *config.Server) error {
			held, err := store.Passkeys(ctx)
			if err != nil {
				return err
			}
			if len(held) == 0 {
				fmt.Fprintln(environment.Stdout, "no passkey; the admin area is off and the server counts nothing")
			}
			for index := range held {
				fmt.Fprintln(environment.Stdout, describePasskey(&held[index]))
			}
			return nil
		},
	})
}

// RunAdminReset removes every passkey, which switches the admin area and its counting off.
func RunAdminReset(ctx context.Context, environment *command.Environment, arguments []string) int {
	return runAdmin(ctx, environment, arguments, &adminCommand{
		name: "admin-reset",
		help: "Removes every passkey and any open enrollment link, so nobody can sign in to the admin area and " +
			"the server stops counting within a minute. The counts stay.",
		run: func(ctx context.Context, store *adminstore.Store, _ *config.Server) error {
			removed, err := store.RemovePasskeys(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.Stdout, "removed %d passkeys\n", removed)
			return nil
		},
	})
}

type adminCommand struct {
	name string
	help string
	run  func(ctx context.Context, store *adminstore.Store, settings *config.Server) error
}

func runAdmin(
	ctx context.Context,
	environment *command.Environment,
	arguments []string,
	admin *adminCommand,
) int {
	flags := command.NewFlags(environment, admin.name)
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s %s\n\n%s Run it on the server host; it reads NYLZO_DATA_DIR like sweep-once.\n",
			command.Program,
			admin.name,
			admin.help,
		)
	}
	if code, done := command.ParseFlags(flags, environment, arguments); done {
		return code
	}
	if !serverSupported {
		return command.Failf(environment, "%s", errServerUnsupported)
	}
	settings, err := config.FromEnvironment(environment.Getenv, false)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	if err = requireStore(settings.DataDirectory); err != nil {
		return command.Failf(environment, "%s", err)
	}
	// serve creates the file as its own user; a command that created it could leave a file serve may not open.
	if _, statErr := os.Stat(adminStorePath(settings.DataDirectory)); statErr != nil {
		return command.Failf(environment, "%s: %s; start the server once first", errNoStore, adminstore.FileName)
	}
	store, err := adminstore.Open(ctx, adminStorePath(settings.DataDirectory))
	if err != nil {
		return command.Failf(environment, "%s", logging.ErrorText(err))
	}
	defer store.Close()
	if err = admin.run(ctx, store, &settings); err != nil {
		return command.Failf(environment, "%s", logging.ErrorText(err))
	}
	return command.ExitOK
}

func adminStorePath(dataDirectory string) string {
	return filepath.Join(dataDirectory, adminstore.FileName)
}

// adminAddress is the public URL of the instance; a server without one is reached on this host where it listens,
// as localhost when it listens there, which the admin area accepts without NYLZO_PUBLIC_URL.
func adminAddress(settings *config.Server) string {
	if settings.PublicURL != "" {
		return settings.PublicURL + webclient.AdminPath
	}
	host, port, err := net.SplitHostPort(settings.Listen)
	if err != nil {
		return "http://" + settings.Listen + webclient.AdminPath
	}
	address, parseErr := netip.ParseAddr(host)
	switch {
	case host == "", parseErr == nil && address.IsUnspecified(), host == "127.0.0.1", host == "::1":
		host = "localhost"
	case parseErr == nil && address.Zone() != "":
		host = address.WithZone("").String() + "%25" + address.Zone()
	default:
	}
	return "http://" + net.JoinHostPort(host, port) + webclient.AdminPath
}

func describePasskey(held *adminstore.Passkey) string {
	kind := "device-bound"
	if held.BackupEligible {
		kind = "synced"
	}
	used := "never used"
	if !held.LastUsedAt.IsZero() {
		used = "last used " + held.LastUsedAt.UTC().Format(dateFormat)
	}
	return fmt.Sprintf(
		"%s  %s, %s, added %s, %s",
		core.Encode(held.ID[:min(len(held.ID), shownIDLength)]),
		algorithmNames[held.Algorithm],
		kind,
		held.CreatedAt.UTC().Format(dateFormat),
		used,
	)
}
