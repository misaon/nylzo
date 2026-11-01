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

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

const (
	createAttempts = 3
	workBudget     = 110 * time.Second
	workRounds     = 2
	weakBelowChars = 16
	weakBelowWords = 4
	attachmentMime = "application/octet-stream"
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
	compress    bool
}

type content struct {
	meta envelope.Meta
	file []byte
}

func (c *content) wipe() {
	clear(c.file)
}

func createFlags(environment *command.Environment) (*flag.FlagSet, *createOptions) {
	options := &createOptions{}
	flags := command.NewFlags(environment, "create")
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
	flags.BoolVar(
		&options.compress,
		"compress",
		false,
		"gzip the attachment when that halves its stored size; the server then sees roughly how well it compresses",
	)
	return flags, options
}

func runCreate(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags, options := createFlags(environment)
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if options.server == "" {
		options.server = environment.Getenv(envServer)
	}
	if options.server == "" {
		fmt.Fprintln(environment.Stderr, command.Program+" create needs --server or "+envServer)
		return command.ExitUsage
	}
	api, err := client.New(options.server)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	message, err := readContent(environment, options.fromFile)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer message.wipe()
	passphrase, generated, err := chosenPassphrase(environment, options)
	if err != nil {
		return command.Failf(environment, "passphrase: %s", explain(err))
	}
	manage, err := reserveManageFile(options.manageFile)
	if err != nil {
		generated.discard()
		return command.Failf(environment, "--manage-token-file: the file already exists or cannot be created")
	}
	created, err := createMessage(
		ctx,
		environment,
		api,
		createInput{options: options, passphrase: passphrase, message: &message},
	)
	if err != nil {
		manage.discard()
		generated.discard()
		return command.Failf(environment, "%s", explain(err))
	}
	defer created.wipe()
	return reportCreated(environment, api, options, &created, manage)
}

// chosenPassphrase writes a generated passphrase before the create, so a failed write never leaves an unreadable message.
func chosenPassphrase(environment *command.Environment, options *createOptions) (string, *generatedPassphrase, error) {
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
	passphrase := keys.GeneratePassphrase()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, command.PrivateFileMode)
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

func createPassphrase(environment *command.Environment, source passphraseSource) (string, error) {
	if !source.given(environment) {
		return "", nil
	}
	passphrase, err := readPassphrase(environment, source, true)
	if err != nil {
		return "", err
	}
	if strengthErr := keys.CheckPassphraseStrength(passphrase); strengthErr != nil {
		return "", strengthErr
	}
	if weakPassphrase(passphrase) {
		fmt.Fprintf(
			environment.Stderr,
			"%s: warning: a passphrase of at least %d words and %d characters is much harder to guess\n",
			command.Program,
			weakBelowWords,
			weakBelowChars,
		)
	}
	return passphrase, nil
}

func reportCreated(
	environment *command.Environment,
	api *client.Client,
	options *createOptions,
	created *createdMessage,
	manage manageFile,
) int {
	token := formatManageToken(api.Origin(), created.id, &created.manage)
	if err := manage.write(token); err != nil {
		fmt.Fprintln(environment.Stdout, created.link)
		return command.Failf(environment, "the message was created, but its manage token file could not be written")
	}
	fmt.Fprintln(environment.Stdout, created.link)
	if options.printManage {
		fmt.Fprint(environment.Stdout, token)
	}
	fmt.Fprintf(
		environment.Stderr,
		"%s: the link works until %s\n", command.Program, created.expires.Local().Format(time.RFC1123),
	)
	return command.ExitOK
}

func weakPassphrase(passphrase string) bool {
	normalized := string(keys.NormalizePassphrase(passphrase))
	words := strings.FieldsFunc(normalized, func(character rune) bool {
		return unicode.IsSpace(character) || character == '-'
	})
	return utf8.RuneCountInString(normalized) < weakBelowChars || len(words) < weakBelowWords
}

func readContent(environment *command.Environment, fromFile string) (content, error) {
	if fromFile != "" {
		return readAttachment(fromFile)
	}
	var raw []byte
	var err error
	if environment.Terminal.StdinIsTerminal() {
		raw, err = environment.Terminal.ReadSecret("Message, one line (hidden): ")
	} else {
		raw, err = command.ReadLimited(environment.Stdin, envelope.MaxTextBytes)
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
	return content{meta: envelope.Meta{Text: &text}}, nil
}

func readAttachment(path string) (content, error) {
	info, err := os.Stat(path)
	if err != nil {
		return content{}, fmt.Errorf("--from-file: %w", command.Unreadable(err))
	}
	if !info.Mode().IsRegular() || info.Size() > envelope.MaxFileBytes {
		return content{}, fmt.Errorf("--from-file must be a regular file of at most %d bytes", envelope.MaxFileBytes)
	}
	data, err := command.ReadFileLimited(path, envelope.MaxFileBytes)
	if err != nil {
		return content{}, fmt.Errorf("--from-file: %w", err)
	}
	name := attachmentName(filepath.Base(path))
	kind := mime.TypeByExtension(filepath.Ext(name))
	if kind == "" || len(kind) > envelope.MaxMime || !utf8.ValidString(kind) {
		kind = attachmentMime
	}
	return content{
		meta: envelope.Meta{File: &envelope.FileMeta{Name: name, Mime: kind, Size: int64(len(data))}},
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

type createInput struct {
	options    *createOptions
	passphrase string
	message    *content
	policy     envelope.Policy
	maxObject  int64
}

// createMessage retries with a fresh id when the server refused the id or the proof of work.
func createMessage(
	ctx context.Context,
	environment *command.Environment,
	api *client.Client,
	input createInput,
) (createdMessage, error) {
	instance, err := api.Instance(ctx, nil)
	if err != nil {
		return createdMessage{}, err
	}
	if input.policy, err = createPolicy(input.options, instance.Limits, input.message); err != nil {
		return createdMessage{}, err
	}
	input.maxObject = instance.Limits.MaxObjectBytes
	for attempt := 1; ; attempt++ {
		created, createErr := createOnce(ctx, environment, api, &input)
		if attempt == createAttempts || !retryWithNewID(createErr) {
			return created, createErr
		}
	}
}

func retryWithNewID(err error) bool {
	return client.IsKind(err, "id_token") || client.IsKind(err, "conflict") || client.IsKind(err, "share_lost") ||
		client.IsKind(err, "pow")
}

func createOnce(
	ctx context.Context,
	environment *command.Environment,
	api *client.Client,
	input *createInput,
) (createdMessage, error) {
	shareKey := share.NewKey()
	instance, err := api.Instance(
		ctx,
		&client.Creation{TTLSeconds: input.policy.TTLSeconds, ShareKey: shareKey.PublicKey()},
	)
	if err != nil {
		return createdMessage{}, err
	}
	issued := instance.Create
	expiryShare, err := share.Unwrap(shareKey, &issued.Share)
	if err != nil {
		return createdMessage{}, err
	}
	defer clear(expiryShare[:])
	kdf := core.KDFNone
	if input.passphrase != "" {
		kdf = core.KDFScrypt
	}
	secret, manage := core.NewSecret(), core.NewSecret()
	defer clear(secret[:])
	defer clear(manage[:])
	request, err := sealMessage(&sealInput{
		id:         issued.ID,
		secret:     secret,
		manage:     manage,
		share:      &expiryShare,
		kdf:        kdf,
		passphrase: input.passphrase,
		policy:     input.policy,
		message:    input.message,
		compress:   input.options.compress,
	})
	if err != nil {
		return createdMessage{}, err
	}
	if int64(len(request.Object)) > input.maxObject {
		return createdMessage{}, errTooLargeForInstance
	}
	request.IDToken, request.Epoch = issued.IDToken, issued.Epoch
	if instance.PoW.Required {
		if request.PoWToken, err = prove(ctx, environment, api, issued.ID); err != nil {
			return createdMessage{}, err
		}
	}
	objectHash := envelope.ObjectHash(request.Object)
	expires, err := api.Create(ctx, &request)
	if err != nil {
		return createdMessage{}, err
	}
	return createdMessage{
		link:    core.FormatLink(api.Origin(), issued.ID, kdf, secret, &objectHash),
		id:      issued.ID,
		manage:  *manage,
		expires: expires,
	}, nil
}

type sealInput struct {
	id         core.ID
	secret     *[core.SecretLen]byte
	manage     *[core.SecretLen]byte
	share      *[core.ShareLen]byte
	kdf        core.KDF
	passphrase string
	policy     envelope.Policy
	message    *content
	compress   bool
}

func sealMessage(input *sealInput) (client.CreateRequest, error) {
	derived, err := keys.DeriveAll(input.id, input.secret, input.kdf, input.passphrase, input.share)
	if err != nil {
		return client.CreateRequest{}, err
	}
	defer derived.Wipe()
	payload := envelope.Payload{Meta: input.message.meta, File: input.message.file}
	encode := envelope.Seal
	if input.compress {
		encode = envelope.SealWithCompression
	}
	object, err := encode(&derived.Content, envelope.NewHeader(input.kdf), input.id, input.policy, payload)
	if err != nil {
		return client.CreateRequest{}, err
	}
	return client.CreateRequest{
		ID:         input.id,
		ReadKey:    signing.PublicKey(&derived.Link.R),
		Verifier:   signing.PublicKey(&derived.Verifier),
		ManageKey:  signing.PublicKey(input.manage),
		KDF:        input.kdf,
		OpensMax:   input.policy.OpensMax,
		TTLSeconds: input.policy.TTLSeconds,
		Object:     object,
	}, nil
}

func createPolicy(options *createOptions, limits client.Limits, message *content) (envelope.Policy, error) {
	maxText := min(limits.MaxTextBytes, envelope.MaxTextBytes)
	if message.meta.Text != nil && int64(len(*message.meta.Text)) > maxText {
		return envelope.Policy{}, fmt.Errorf("%w: the text is longer than %d bytes", errPolicy, maxText)
	}
	if message.meta.File != nil && message.meta.File.Size > limits.MaxFileBytes {
		return envelope.Policy{}, fmt.Errorf("%w: the file is larger than %d bytes", errPolicy, limits.MaxFileBytes)
	}
	ttl := time.Duration(limits.DefaultTTL) * time.Second
	if options.ttl != 0 {
		ttl = options.ttl
	}
	maxTTL := time.Duration(limits.MaxTTLSeconds) * time.Second
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 || seconds < envelope.MinTTLSeconds || ttl > maxTTL || seconds > math.MaxUint32 {
		return envelope.Policy{}, fmt.Errorf(
			"%w: --ttl must be whole seconds between %ds and %ds",
			errPolicy,
			envelope.MinTTLSeconds,
			limits.MaxTTLSeconds,
		)
	}
	if options.opens < 1 || options.opens > limits.MaxOpens || options.opens > math.MaxUint16 {
		return envelope.Policy{}, fmt.Errorf("%w: --opens must be between 1 and %d", errPolicy, limits.MaxOpens)
	}
	return envelope.Policy{OpensMax: uint16(options.opens), TTLSeconds: uint32(seconds)}, nil
}

// prove solves the proof of work and fetches a new challenge once if the first one runs out of time.
func prove(ctx context.Context, environment *command.Environment, api *client.Client, id core.ID) (string, error) {
	for round := 1; ; round++ {
		instance, err := api.Instance(ctx, nil)
		if err != nil {
			return "", err
		}
		if !instance.PoW.Required {
			return "", nil
		}
		fmt.Fprintf(
			environment.Stderr,
			"%s: the server is busy, solving a proof of work (difficulty %d)\n",
			command.Program,
			instance.PoW.Difficulty,
		)
		solving, cancel := context.WithTimeout(ctx, workBudget)
		token, err := proofofwork.Solve(solving, instance.PoW.Challenge, id, instance.PoW.Difficulty)
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
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, command.PrivateFileMode)
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
