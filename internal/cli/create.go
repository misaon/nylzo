package cli

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const (
	createAttempts  = 3
	powBudget       = 110 * time.Second
	powRounds       = 2
	weakBelowChars  = 16
	weakBelowWords  = 4
	attachmentMime  = "application/octet-stream"
	privateFileMode = 0o600
)

var errNothingToSend = errors.New("nothing to send: pipe text to stdin or use --from-file")

type createOptions struct {
	server      string
	fromFile    string
	ttl         time.Duration
	opens       int
	passphrase  passphraseSource
	kdf         string
	manageFile  string
	printManage bool
}

type content struct {
	meta core.Meta
	file []byte
}

func (c *content) wipe() {
	clear(c.file)
}

func runCreate(ctx context.Context, env Env, args []string) int {
	var o createOptions
	fs := newFlags(env, "create")
	fs.StringVar(&o.server, "server", "", "server address such as https://nylzo.example (or "+envServer+")")
	fs.StringVar(&o.fromFile, "from-file", "", "send this file as an attachment instead of text from stdin")
	fs.DurationVar(&o.ttl, "ttl", 0, "how long the message lives, for example 30m or 24h (default: the instance default)")
	fs.IntVar(&o.opens, "opens", 1, "how many times the message can be opened")
	fs.StringVar(&o.passphrase.file, "passphrase-file", "", "protect the message with the passphrase on the first line of this file (or "+envPassphrase+")")
	fs.BoolVar(&o.passphrase.ask, "ask-passphrase", false, "protect the message with a passphrase asked on the terminal without echo")
	fs.StringVar(&o.kdf, "kdf", "", "passphrase profile, scrypt or pbkdf2 (default: the instance default)")
	fs.StringVar(&o.manageFile, "manage-token-file", "", "write the manage token for status and cancel to this new file (mode 0600)")
	fs.BoolVar(&o.printManage, "print-manage-token", false, "print the manage token after the link")
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	if o.server == "" {
		o.server = env.Getenv(envServer)
	}
	if o.server == "" {
		fmt.Fprintln(env.Stderr, program+" create needs --server or "+envServer)
		return exitUsage
	}
	kdf, err := chosenKDF(o, env)
	if err != nil {
		fmt.Fprintln(env.Stderr, program+" create: "+err.Error())
		return exitUsage
	}
	c, err := client.New(o.server)
	if err != nil {
		return failf(env, "%s", explain(err))
	}
	msg, err := readContent(env, o.fromFile)
	if err != nil {
		return failf(env, "%s", err)
	}
	defer msg.wipe()
	var passphrase string
	if o.passphrase.given(env) {
		if passphrase, err = readPassphrase(env, o.passphrase, true); err != nil {
			return failf(env, "passphrase: %s", explain(err))
		}
		if err := core.CheckPassphraseStrength(passphrase); err != nil {
			return failf(env, "passphrase: %s", explain(err))
		}
		if weakPassphrase(passphrase) {
			fmt.Fprintf(env.Stderr, "%s: warning: a passphrase of at least %d words and %d characters is much harder to guess\n", program, weakBelowWords, weakBelowChars)
		}
	}
	manage, err := reserveManageFile(o.manageFile)
	if err != nil {
		return failf(env, "manage token file: %s", err)
	}
	created, err := createMessage(ctx, env, c, o, kdf, passphrase, msg)
	if err != nil {
		manage.discard()
		return failf(env, "%s", explain(err))
	}
	defer created.wipe()
	token := formatManageToken(c.Origin(), created.id, &created.manage)
	if err := manage.write(token); err != nil {
		fmt.Fprintln(env.Stdout, created.link)
		return failf(env, "the message was created, but its manage token file could not be written: %s", err)
	}
	fmt.Fprintln(env.Stdout, created.link)
	if o.printManage {
		fmt.Fprint(env.Stdout, token)
	}
	fmt.Fprintf(env.Stderr, "%s: the link works until %s\n", program, created.expires.Local().Format(time.RFC1123))
	return exitOK
}

func chosenKDF(o createOptions, env Env) (core.KDF, error) {
	switch o.kdf {
	case "":
		return core.KDFNone, nil
	case "scrypt", "pbkdf2":
		if !o.passphrase.given(env) {
			return 0, errors.New("--kdf needs a passphrase")
		}
		if o.kdf == "scrypt" {
			return core.KDFScrypt, nil
		}
		return core.KDFPBKDF2, nil
	}
	return 0, fmt.Errorf("--kdf must be scrypt or pbkdf2")
}

func weakPassphrase(passphrase string) bool {
	normalized := string(core.NormalizePassphrase(passphrase))
	return utf8.RuneCountInString(normalized) < weakBelowChars || len(strings.Fields(normalized)) < weakBelowWords
}

func readContent(env Env, fromFile string) (content, error) {
	if fromFile != "" {
		info, err := os.Stat(fromFile)
		if err != nil {
			return content{}, fmt.Errorf("--from-file: %w", unreadable(err))
		}
		if !info.Mode().IsRegular() || info.Size() > core.MaxFileBytes {
			return content{}, fmt.Errorf("--from-file must be a regular file of at most %d bytes", core.MaxFileBytes)
		}
		data, err := readFileLimited(fromFile, core.MaxFileBytes)
		if err != nil {
			return content{}, fmt.Errorf("--from-file: %w", err)
		}
		name := attachmentName(filepath.Base(fromFile))
		kind := mime.TypeByExtension(filepath.Ext(name))
		if kind == "" || len(kind) > core.MaxMime || !utf8.ValidString(kind) {
			kind = attachmentMime
		}
		return content{meta: core.Meta{File: &core.FileMeta{Name: name, Mime: kind, Size: int64(len(data))}}, file: data}, nil
	}
	var raw []byte
	var err error
	if env.Terminal.StdinIsTerminal() {
		raw, err = env.Terminal.ReadSecret("Message, one line (hidden): ")
	} else {
		raw, err = readLimited(env.Stdin, core.MaxTextBytes)
	}
	if err != nil {
		return content{}, err
	}
	defer clear(raw)
	if len(raw) == 0 {
		return content{}, errNothingToSend
	}
	if !utf8.Valid(raw) {
		return content{}, errors.New("the text on stdin is not valid UTF-8; send binary data with --from-file")
	}
	text := string(raw)
	return content{meta: core.Meta{Text: &text}}, nil
}

type createdMessage struct {
	link    string
	id      core.ID
	manage  [core.TokenLen]byte
	expires time.Time
}

func (m *createdMessage) wipe() {
	clear(m.manage[:])
	m.link = ""
}

func createMessage(ctx context.Context, env Env, c *client.Client, o createOptions, kdf core.KDF, passphrase string, msg content) (createdMessage, error) {
	for attempt := 1; ; attempt++ {
		created, err := createOnce(ctx, env, c, o, kdf, passphrase, msg)
		if attempt == createAttempts || !retryWithNewID(err) {
			return created, err
		}
	}
}

func retryWithNewID(err error) bool {
	return client.IsKind(err, "id_token") || client.IsKind(err, "conflict") || client.IsKind(err, "pow")
}

func createOnce(ctx context.Context, env Env, c *client.Client, o createOptions, chosen core.KDF, passphrase string, msg content) (createdMessage, error) {
	inst, err := c.Instance(ctx)
	if err != nil {
		return createdMessage{}, err
	}
	policy, err := createPolicy(o, inst.Limits, msg)
	if err != nil {
		return createdMessage{}, err
	}
	kdf := core.KDFNone
	if passphrase != "" {
		kdf = chosen
		if kdf == core.KDFNone {
			kdf = core.KDF(inst.Limits.DefaultKDF)
		}
		if !kdf.RequiresPassphrase() {
			kdf = core.KDFScrypt
		}
	}
	id := inst.Create.ID
	s, manage := core.NewSecret(), core.NewSecret()
	defer clear(s[:])
	defer clear(manage[:])
	link, err := core.DeriveLink(id, s)
	if err != nil {
		return createdMessage{}, err
	}
	defer clear(link.R[:])
	defer clear(link.SaltPW[:])
	p, err := core.DerivePassphraseKey(kdf, passphrase, &link.SaltPW)
	if err != nil {
		return createdMessage{}, err
	}
	keys, err := core.DeriveContent(id, s, p)
	clear(p)
	if err != nil {
		return createdMessage{}, err
	}
	defer clear(keys.K[:])
	defer clear(keys.V[:])
	object, err := core.Seal(&keys.K, core.NewHeader(kdf), id, policy, core.Payload{Meta: msg.meta, File: msg.file})
	if err != nil {
		return createdMessage{}, err
	}
	if int64(len(object)) > inst.Limits.MaxObjectBytes {
		return createdMessage{}, errTooLargeForInstance
	}
	req := client.CreateRequest{
		ID: id, IDToken: inst.Create.IDToken, ReadKey: core.PublicKey(&link.R), ManageKey: core.PublicKey(manage),
		KDF: kdf, OpensMax: policy.OpensMax, TTLSeconds: policy.TTLSeconds, Object: object,
	}
	if kdf != core.KDFNone {
		verifier := core.PublicKey(&keys.V)
		req.Verifier = &verifier
	}
	if inst.PoW.Required {
		if req.PoWToken, err = prove(ctx, env, c, id); err != nil {
			return createdMessage{}, err
		}
	}
	expires, err := c.Create(ctx, req)
	if err != nil {
		return createdMessage{}, err
	}
	out := createdMessage{link: core.FormatLink(c.Origin(), id, kdf, s), id: id, manage: *manage, expires: expires}
	return out, nil
}

var (
	errTooLargeForInstance = errors.New("the message is larger than this server accepts")
	errPolicy              = errors.New("the message does not fit the limits of this server")
)

func createPolicy(o createOptions, limits client.Limits, msg content) (core.Policy, error) {
	if msg.meta.Text != nil && int64(len(*msg.meta.Text)) > min(limits.MaxTextBytes, core.MaxTextBytes) {
		return core.Policy{}, fmt.Errorf("%w: the text is longer than %d bytes", errPolicy, min(limits.MaxTextBytes, core.MaxTextBytes))
	}
	if msg.meta.File != nil && msg.meta.File.Size > limits.MaxFileBytes {
		return core.Policy{}, fmt.Errorf("%w: the file is larger than %d bytes", errPolicy, limits.MaxFileBytes)
	}
	ttl := time.Duration(limits.DefaultTTL) * time.Second
	if o.ttl != 0 {
		ttl = o.ttl
	}
	if ttl%time.Second != 0 || ttl < core.MinTTLSeconds*time.Second || ttl > time.Duration(limits.MaxTTLSeconds)*time.Second {
		return core.Policy{}, fmt.Errorf("%w: --ttl must be whole seconds between %ds and %ds", errPolicy, core.MinTTLSeconds, limits.MaxTTLSeconds)
	}
	if o.opens < 1 || o.opens > limits.MaxOpens {
		return core.Policy{}, fmt.Errorf("%w: --opens must be between 1 and %d", errPolicy, limits.MaxOpens)
	}
	return core.Policy{OpensMax: uint16(o.opens), TTLSeconds: uint32(ttl / time.Second)}, nil
}

func prove(ctx context.Context, env Env, c *client.Client, id core.ID) (string, error) {
	for round := 1; ; round++ {
		inst, err := c.Instance(ctx)
		if err != nil {
			return "", err
		}
		if !inst.PoW.Required {
			return "", nil
		}
		fmt.Fprintf(env.Stderr, "%s: the server is busy, solving a proof of work (difficulty %d)\n", program, inst.PoW.Difficulty)
		solving, cancel := context.WithTimeout(ctx, powBudget)
		token, _, err := core.SolvePoW(solving, inst.PoW.Challenge, id, inst.PoW.Difficulty)
		cancel()
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, context.DeadlineExceeded) || round == powRounds || ctx.Err() != nil {
			return "", err
		}
	}
}

type manageFile struct {
	f *os.File
}

func reserveManageFile(path string) (manageFile, error) {
	if path == "" {
		return manageFile{}, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return manageFile{}, err
	}
	return manageFile{f: f}, nil
}

func (m manageFile) write(token string) error {
	if m.f == nil {
		return nil
	}
	_, err := m.f.WriteString(token)
	return errors.Join(err, m.f.Sync(), m.f.Close())
}

func (m manageFile) discard() {
	if m.f != nil {
		m.f.Close()
		os.Remove(m.f.Name())
	}
}
