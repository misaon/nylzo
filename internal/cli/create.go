package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

const (
	createAttempts  = 3
	workBudget      = 110 * time.Second
	workRounds      = 2
	weakBelowChars  = 16
	weakBelowWords  = 4
	attachmentMime  = "application/octet-stream"
	privateFileMode = 0o600
)

var (
	errNothingToSend       = errors.New("nothing to send: pipe text to stdin or use --from-file")
	errTooLargeForInstance = errors.New("the message is larger than this server accepts")
	errPolicy              = errors.New("the message does not fit the limits of this server")
)

type createOptions struct {
	server      string
	fromFile    string
	ttl         time.Duration
	opens       int
	passphrase  passphraseSource
	generate    string
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

func createFlags(environment *Environment) (*flag.FlagSet, *createOptions) {
	options := &createOptions{}
	flags := newFlags(environment, "create")
	flags.StringVar(&options.server, "server", "", "server address such as https://nylzo.example (or "+envServer+")")
	flags.StringVar(&options.fromFile, "from-file", "", "send this file as an attachment instead of text from stdin")
	flags.DurationVar(
		&options.ttl,
		"ttl",
		0,
		"how long the message lives, for example 30m or 24h (default: the instance default)",
	)
	flags.IntVar(&options.opens, "opens", 1, "how many times the message can be opened")
	flags.StringVar(
		&options.passphrase.file,
		"passphrase-file",
		"",
		"protect the message with the passphrase on the first line of this file (or "+envPassphrase+")",
	)
	flags.BoolVar(
		&options.passphrase.ask,
		"ask-passphrase",
		false,
		"protect the message with a passphrase asked on the terminal without echo",
	)
	flags.StringVar(
		&options.generate,
		"generate-passphrase-file",
		"",
		"protect the message with a generated passphrase written to this new file (mode 0600)",
	)
	flags.StringVar(
		&options.manageFile,
		"manage-token-file",
		"",
		"write the manage token for status and cancel to this new file (mode 0600)",
	)
	flags.BoolVar(&options.printManage, "print-manage-token", false, "print the manage token after the link")
	return flags, options
}

func runCreate(ctx context.Context, environment *Environment, arguments []string) int {
	flags, options := createFlags(environment)
	if code, done := flagsDone(parseFlags(flags, environment, arguments)); done {
		return code
	}
	if options.server == "" {
		options.server = environment.Getenv(envServer)
	}
	if options.server == "" {
		fmt.Fprintln(environment.Stderr, program+" create needs --server or "+envServer)
		return exitUsage
	}
	api, err := client.New(options.server)
	if err != nil {
		return failf(environment, "%s", explain(err))
	}
	message, err := readContent(environment, options.fromFile)
	if err != nil {
		return failf(environment, "%s", err)
	}
	defer message.wipe()
	passphrase, generated, err := chosenPassphrase(environment, options)
	if err != nil {
		return failf(environment, "passphrase: %s", explain(err))
	}
	manage, err := reserveManageFile(options.manageFile)
	if err != nil {
		generated.discard()
		return failf(environment, "--manage-token-file: the file already exists or cannot be created")
	}
	created, err := createMessage(ctx, environment, api, options, passphrase, &message)
	if err != nil {
		manage.discard()
		generated.discard()
		return failf(environment, "%s", explain(err))
	}
	defer created.wipe()
	return reportCreated(environment, api, options, &created, manage)
}

// chosenPassphrase writes a generated passphrase before the create, so a failed write never leaves an unreadable message.
func chosenPassphrase(environment *Environment, options *createOptions) (string, *generatedPassphrase, error) {
	if options.generate == "" {
		passphrase, err := createPassphrase(environment, options.passphrase)
		return passphrase, nil, err
	}
	if options.passphrase.given(environment) {
		return "", nil, errPassphraseSource
	}
	generated, err := writeGeneratedPassphrase(options.generate)
	if err != nil {
		return "", nil, err
	}
	return generated.passphrase, generated, nil
}

type generatedPassphrase struct {
	passphrase string
	path       string
}

func (g *generatedPassphrase) discard() {
	if g != nil {
		os.Remove(g.path)
	}
}

func writeGeneratedPassphrase(path string) (*generatedPassphrase, error) {
	passphrase := core.GeneratePassphrase()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return nil, errors.New("--generate-passphrase-file: the file already exists or cannot be created")
	}
	_, err = file.WriteString(passphrase + "\n")
	if err = errors.Join(err, file.Sync(), file.Close()); err != nil {
		os.Remove(path)
		return nil, errors.New("--generate-passphrase-file: the passphrase could not be written")
	}
	return &generatedPassphrase{passphrase: passphrase, path: path}, nil
}

func createPassphrase(environment *Environment, source passphraseSource) (string, error) {
	if !source.given(environment) {
		return "", nil
	}
	passphrase, err := readPassphrase(environment, source, true)
	if err != nil {
		return "", err
	}
	if strengthErr := core.CheckPassphraseStrength(passphrase); strengthErr != nil {
		return "", strengthErr
	}
	if weakPassphrase(passphrase) {
		fmt.Fprintf(
			environment.Stderr,
			"%s: warning: a passphrase of at least %d words and %d characters is much harder to guess\n",
			program,
			weakBelowWords,
			weakBelowChars,
		)
	}
	return passphrase, nil
}

func reportCreated(
	environment *Environment,
	api *client.Client,
	options *createOptions,
	created *createdMessage,
	manage manageFile,
) int {
	token := formatManageToken(api.Origin(), created.id, &created.manage)
	if err := manage.write(token); err != nil {
		fmt.Fprintln(environment.Stdout, created.link)
		return failf(environment, "the message was created, but its manage token file could not be written")
	}
	fmt.Fprintln(environment.Stdout, created.link)
	if options.printManage {
		fmt.Fprint(environment.Stdout, token)
	}
	fmt.Fprintf(
		environment.Stderr,
		"%s: the link works until %s\n",
		program,
		created.expires.Local().Format(time.RFC1123),
	)
	return exitOK
}

func weakPassphrase(passphrase string) bool {
	normalized := string(core.NormalizePassphrase(passphrase))
	words := strings.FieldsFunc(normalized, func(character rune) bool {
		return unicode.IsSpace(character) || character == '-'
	})
	return utf8.RuneCountInString(normalized) < weakBelowChars || len(words) < weakBelowWords
}

func readContent(environment *Environment, fromFile string) (content, error) {
	if fromFile != "" {
		return readAttachment(fromFile)
	}
	var raw []byte
	var err error
	if environment.Terminal.StdinIsTerminal() {
		raw, err = environment.Terminal.ReadSecret("Message, one line (hidden): ")
	} else {
		raw, err = readLimited(environment.Stdin, core.MaxTextBytes)
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

func readAttachment(path string) (content, error) {
	info, err := os.Stat(path)
	if err != nil {
		return content{}, fmt.Errorf("--from-file: %w", unreadable(err))
	}
	if !info.Mode().IsRegular() || info.Size() > core.MaxFileBytes {
		return content{}, fmt.Errorf("--from-file must be a regular file of at most %d bytes", core.MaxFileBytes)
	}
	data, err := readFileLimited(path, core.MaxFileBytes)
	if err != nil {
		return content{}, fmt.Errorf("--from-file: %w", err)
	}
	name := attachmentName(filepath.Base(path))
	kind := mime.TypeByExtension(filepath.Ext(name))
	if kind == "" || len(kind) > core.MaxMime || !utf8.ValidString(kind) {
		kind = attachmentMime
	}
	return content{
		meta: core.Meta{File: &core.FileMeta{Name: name, Mime: kind, Size: int64(len(data))}},
		file: data,
	}, nil
}

type createdMessage struct {
	link    string
	id      core.ID
	manage  [core.SeedLen]byte
	expires time.Time
}

func (m *createdMessage) wipe() {
	clear(m.manage[:])
	m.link = ""
}

// createMessage retries with a fresh id when the server refused the id or the proof of work.
func createMessage(
	ctx context.Context,
	environment *Environment,
	api *client.Client,
	options *createOptions,
	passphrase string,
	message *content,
) (createdMessage, error) {
	for attempt := 1; ; attempt++ {
		created, err := createOnce(ctx, environment, api, options, passphrase, message)
		if attempt == createAttempts || !retryWithNewID(err) {
			return created, err
		}
	}
}

func retryWithNewID(err error) bool {
	return client.IsKind(err, "id_token") || client.IsKind(err, "conflict") || client.IsKind(err, "pow")
}

func createOnce(
	ctx context.Context,
	environment *Environment,
	api *client.Client,
	options *createOptions,
	passphrase string,
	message *content,
) (createdMessage, error) {
	instance, err := api.Instance(ctx)
	if err != nil {
		return createdMessage{}, err
	}
	policy, err := createPolicy(options, instance.Limits, message)
	if err != nil {
		return createdMessage{}, err
	}
	kdf := core.KDFNone
	if passphrase != "" {
		kdf = core.KDFScrypt
	}
	id := instance.Create.ID
	secret, manage := core.NewSecret(), core.NewSecret()
	defer clear(secret[:])
	defer clear(manage[:])
	request, err := sealMessage(id, secret, manage, kdf, passphrase, policy, message)
	if err != nil {
		return createdMessage{}, err
	}
	if int64(len(request.Object)) > instance.Limits.MaxObjectBytes {
		return createdMessage{}, errTooLargeForInstance
	}
	request.IDToken = instance.Create.IDToken
	if instance.PoW.Required {
		if request.PoWToken, err = prove(ctx, environment, api, id); err != nil {
			return createdMessage{}, err
		}
	}
	expires, err := api.Create(ctx, &request)
	if err != nil {
		return createdMessage{}, err
	}
	return createdMessage{
		link:    core.FormatLink(api.Origin(), id, kdf, secret),
		id:      id,
		manage:  *manage,
		expires: expires,
	}, nil
}

func sealMessage(
	id core.ID,
	secret, manage *[core.SecretLen]byte,
	kdf core.KDF,
	passphrase string,
	policy core.Policy,
	message *content,
) (client.CreateRequest, error) {
	link, err := core.DeriveLink(id, secret)
	if err != nil {
		return client.CreateRequest{}, err
	}
	defer clear(link.R[:])
	defer clear(link.PassphraseSalt[:])
	passphraseKey, err := core.DerivePassphraseKey(kdf, passphrase, &link.PassphraseSalt)
	if err != nil {
		return client.CreateRequest{}, err
	}
	keys, err := core.DeriveContent(id, secret, passphraseKey)
	clear(passphraseKey)
	if err != nil {
		return client.CreateRequest{}, err
	}
	defer clear(keys.K[:])
	defer clear(keys.V[:])
	payload := core.Payload{Meta: message.meta, File: message.file}
	object, err := core.Seal(&keys.K, core.NewHeader(kdf), id, policy, payload)
	if err != nil {
		return client.CreateRequest{}, err
	}
	request := client.CreateRequest{
		ID:         id,
		ReadKey:    core.PublicKey(&link.R),
		ManageKey:  core.PublicKey(manage),
		KDF:        kdf,
		OpensMax:   policy.OpensMax,
		TTLSeconds: policy.TTLSeconds,
		Object:     object,
	}
	if kdf != core.KDFNone {
		request.Verifier = new(core.PublicKey(&keys.V))
	}
	return request, nil
}

func createPolicy(options *createOptions, limits client.Limits, message *content) (core.Policy, error) {
	maxText := min(limits.MaxTextBytes, core.MaxTextBytes)
	if message.meta.Text != nil && int64(len(*message.meta.Text)) > maxText {
		return core.Policy{}, fmt.Errorf("%w: the text is longer than %d bytes", errPolicy, maxText)
	}
	if message.meta.File != nil && message.meta.File.Size > limits.MaxFileBytes {
		return core.Policy{}, fmt.Errorf("%w: the file is larger than %d bytes", errPolicy, limits.MaxFileBytes)
	}
	ttl := time.Duration(limits.DefaultTTL) * time.Second
	if options.ttl != 0 {
		ttl = options.ttl
	}
	maxTTL := time.Duration(limits.MaxTTLSeconds) * time.Second
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 || seconds < core.MinTTLSeconds || ttl > maxTTL || seconds > math.MaxUint32 {
		return core.Policy{}, fmt.Errorf(
			"%w: --ttl must be whole seconds between %ds and %ds",
			errPolicy,
			core.MinTTLSeconds,
			limits.MaxTTLSeconds,
		)
	}
	if options.opens < 1 || options.opens > limits.MaxOpens || options.opens > math.MaxUint16 {
		return core.Policy{}, fmt.Errorf("%w: --opens must be between 1 and %d", errPolicy, limits.MaxOpens)
	}
	return core.Policy{OpensMax: uint16(options.opens), TTLSeconds: uint32(seconds)}, nil
}

// prove solves the proof of work and fetches a new challenge once if the first one runs out of time.
func prove(ctx context.Context, environment *Environment, api *client.Client, id core.ID) (string, error) {
	for round := 1; ; round++ {
		instance, err := api.Instance(ctx)
		if err != nil {
			return "", err
		}
		if !instance.PoW.Required {
			return "", nil
		}
		fmt.Fprintf(
			environment.Stderr,
			"%s: the server is busy, solving a proof of work (difficulty %d)\n",
			program,
			instance.PoW.Difficulty,
		)
		solving, cancel := context.WithTimeout(ctx, workBudget)
		token, err := core.SolvePoW(solving, instance.PoW.Challenge, id, instance.PoW.Difficulty)
		cancel()
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, context.DeadlineExceeded) || round == workRounds || ctx.Err() != nil {
			return "", err
		}
	}
}

type manageFile struct {
	file *os.File
}

func reserveManageFile(path string) (manageFile, error) {
	if path == "" {
		return manageFile{}, nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return manageFile{}, err
	}
	return manageFile{file: file}, nil
}

func (m manageFile) write(token string) error {
	if m.file == nil {
		return nil
	}
	_, err := m.file.WriteString(token)
	return errors.Join(err, m.file.Sync(), m.file.Close())
}

func (m manageFile) discard() {
	if m.file != nil {
		m.file.Close()
		os.Remove(m.file.Name())
	}
}
