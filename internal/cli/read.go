package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const (
	textFileName    = "message.txt"
	blobRetries     = 3
	privateDirMode  = 0o700
	outputDirPrefix = "nylzo-"
)

var (
	errTampered      = errors.New("the message could not be decrypted: it was changed, or the link or passphrase is wrong")
	errPolicyChanged = errors.New("the server sent the message with a different policy than its metadata, so nylzo refused it")
)

type readOptions struct {
	linkFile   string
	passphrase passphraseSource
	out        string
	stdout     bool
}

type openLink struct {
	origin string
	id     core.ID
	kdf    core.KDF
	secret [core.SecretLen]byte
	keys   core.LinkKeys
}

func (l *openLink) wipe() {
	clear(l.secret[:])
	clear(l.keys.R[:])
	clear(l.keys.SaltPW[:])
}

func parseOpenLink(raw string) (openLink, error) {
	parsed, err := core.ParseLink(raw)
	if err != nil {
		return openLink{}, errors.New("the link is incomplete or damaged")
	}
	origin, err := linkOrigin(raw)
	if err != nil {
		clear(parsed.Secret[:])
		return openLink{}, err
	}
	out := openLink{origin: origin, id: parsed.ID, kdf: parsed.KDF, secret: parsed.Secret}
	clear(parsed.Secret[:])
	if out.keys, err = core.DeriveLink(out.id, &out.secret); err != nil {
		out.wipe()
		return openLink{}, err
	}
	return out, nil
}

func linkFromInput(env Env, linkFile string) (openLink, error) {
	raw, err := readLink(env, linkFile)
	if err != nil {
		return openLink{}, err
	}
	return parseOpenLink(raw)
}

func runRead(ctx context.Context, env Env, args []string) int {
	var o readOptions
	fs := newFlags(env, "read")
	fs.StringVar(&o.linkFile, "link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	fs.StringVar(&o.passphrase.file, "passphrase-file", "", "read the passphrase from the first line of this file (or "+envPassphrase+")")
	fs.BoolVar(&o.passphrase.ask, "ask-passphrase", false, "ask for the passphrase on the terminal without echo")
	fs.StringVar(&o.out, "out", "", "write the files into this existing directory instead of a new private one")
	fs.BoolVar(&o.stdout, "stdout", false, "print the text to stdout instead of writing it to a file")
	if parseFlags(fs, env, args) != nil {
		return exitUsage
	}
	link, err := linkFromInput(env, o.linkFile)
	if err != nil {
		return failf(env, "link: %s", explain(err))
	}
	defer link.wipe()
	c, err := client.New(link.origin)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	opened, err := readMessage(ctx, env, c, &link, o.passphrase)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	defer clear(opened.payload.File)
	paths, err := saveMessage(env, o, opened.payload)
	if err != nil {
		return failf(env, "the message was opened but could not be saved: %s", err)
	}
	if err := c.Ack(ctx, link.id, &link.keys.R, opened.lease); err != nil && !client.IsKind(err, "gone") {
		fmt.Fprintf(env.Stderr, "%s: warning: the server did not confirm the read; it frees the message when the lease ends\n", program)
	}
	for _, path := range paths {
		fmt.Fprintln(env.Stdout, path)
	}
	return exitOK
}

type openedMessage struct {
	payload core.Payload
	lease   string
}

func readMessage(ctx context.Context, env Env, c *client.Client, link *openLink, src passphraseSource) (openedMessage, error) {
	meta, err := c.Metadata(ctx, link.id, &link.keys.R)
	if err != nil {
		return openedMessage{}, err
	}
	if meta.State != "available" {
		return openedMessage{}, &client.APIError{Status: 410, Kind: "gone", State: meta.State}
	}
	if err := core.AssertProfile(link.kdf, meta.KDF); err != nil {
		return openedMessage{}, errors.New("the link does not match the message on the server")
	}
	p, err := passphraseKey(env, link, src)
	if err != nil {
		return openedMessage{}, err
	}
	keys, err := core.DeriveContent(link.id, &link.secret, p)
	clear(p)
	if err != nil {
		return openedMessage{}, err
	}
	defer clear(keys.K[:])
	defer clear(keys.V[:])
	var verifier *[core.TokenLen]byte
	if link.kdf != core.KDFNone {
		verifier = &keys.V
	}
	env2, lease, err := fetchEnvelope(ctx, c, link, verifier, meta.Size)
	if err != nil {
		return openedMessage{}, err
	}
	if env2.Flags != 0 || env2.KDF != link.kdf || int(env2.Policy.OpensMax) != meta.OpensMax || int64(env2.Policy.TTLSeconds) != meta.TTLSeconds {
		return openedMessage{}, errPolicyChanged
	}
	_, payload, err := core.Open(&keys.K, link.id, env2.Policy, link.kdf, env2.Object)
	clear(env2.Object)
	if err != nil {
		return openedMessage{}, errTampered
	}
	return openedMessage{payload: payload, lease: lease}, nil
}

func passphraseKey(env Env, link *openLink, src passphraseSource) ([]byte, error) {
	if link.kdf == core.KDFNone {
		return nil, nil
	}
	if !src.given(env) {
		src.ask = true
	}
	passphrase, err := readPassphrase(env, src, false)
	if errors.Is(err, ErrNoTerminal) {
		return nil, errors.New("this message needs a passphrase: use --passphrase-file, " + envPassphrase + " or --ask-passphrase")
	}
	if err != nil {
		return nil, err
	}
	return core.DerivePassphraseKey(link.kdf, passphrase, &link.keys.SaltPW)
}

func fetchEnvelope(ctx context.Context, c *client.Client, link *openLink, verifier *[core.TokenLen]byte, size int64) (client.Envelope, string, error) {
	claimed, err := c.Claim(ctx, link.id, &link.keys.R, verifier, size, nil)
	if err == nil {
		return claimed.Envelope, claimed.Lease, nil
	}
	if claimed.Lease == "" {
		return client.Envelope{}, "", err
	}
	for range blobRetries {
		env, berr := c.Blob(ctx, link.id, &link.keys.R, claimed.Lease, size)
		if berr == nil {
			return env, claimed.Lease, nil
		}
		err = berr
		if client.IsKind(berr, "gone") || client.IsKind(berr, "rate_limited") || ctx.Err() != nil {
			break
		}
	}
	return client.Envelope{}, "", err
}

func saveMessage(env Env, o readOptions, payload core.Payload) ([]string, error) {
	text := payload.Meta.Text
	if o.stdout && text != nil {
		fmt.Fprint(env.Stdout, *text)
		if len(*text) > 0 && (*text)[len(*text)-1] != '\n' {
			fmt.Fprintln(env.Stdout)
		}
		text = nil
	}
	if text == nil && payload.Meta.File == nil {
		return nil, nil
	}
	dir, err := outputDir(o.out)
	if err != nil {
		return nil, err
	}
	var paths []string
	if text != nil {
		path, err := writePrivate(dir, textFileName, []byte(*text))
		if err != nil {
			return paths, err
		}
		paths = append(paths, path)
	}
	if payload.Meta.File != nil {
		name := attachmentName(payload.Meta.File.Name)
		if name == textFileName {
			name = "attachment-" + name
		}
		path, err := writePrivate(dir, name, payload.File)
		if err != nil {
			return paths, err
		}
		paths = append(paths, path)
	}
	if o.stdout {
		for _, path := range paths {
			fmt.Fprintf(env.Stderr, "%s: the attachment is in %s\n", program, path)
		}
		return nil, nil
	}
	return paths, nil
}

func outputDir(out string) (string, error) {
	if out == "" {
		return os.MkdirTemp("", outputDirPrefix)
	}
	info, err := os.Stat(out)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--out must be a directory")
	}
	return out, nil
}

func writePrivate(dir, name string, data []byte) (string, error) {
	path := filepath.Join(dir, name)
	if filepath.Dir(path) != filepath.Clean(dir) {
		return "", fmt.Errorf("refusing to write outside %s", dir)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err = errors.Join(err, f.Sync(), f.Close()); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
