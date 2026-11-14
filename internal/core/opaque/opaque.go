package opaque

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"math"

	"github.com/gtank/ristretto255"
)

const (
	NonceLen = 32
	SeedLen  = 32
	HashLen  = sha512.Size
	MACLen   = sha512.Size

	EnvelopeLen             = NonceLen + MACLen
	RegistrationRequestLen  = ElementLen
	RegistrationResponseLen = ElementLen + ElementLen
	RecordLen               = ElementLen + HashLen + EnvelopeLen
	KE1Len                  = ElementLen + NonceLen + ElementLen
	credentialResponseLen   = ElementLen + NonceLen + maskedResponseLen
	maskedResponseLen       = ElementLen + EnvelopeLen
	KE2Len                  = credentialResponseLen + NonceLen + ElementLen + MACLen
	KE3Len                  = MACLen
	ExportKeyLen            = HashLen
	OPRFSeedLen             = HashLen
	// MaxInputLen bounds the OPRF input, which Finalize prefixes with a two-byte length (RFC 9497 section 3.3.1).
	MaxInputLen = math.MaxUint16

	infoDeriveKeyPair   = "OPAQUE-DeriveKeyPair"
	infoDiffieHellman   = "OPAQUE-DeriveDiffieHellmanKeyPair"
	labelOPRFKey        = "OprfKey"
	labelMaskingKey     = "MaskingKey"
	labelAuthKey        = "AuthKey"
	labelExportKey      = "ExportKey"
	labelPrivateKey     = "PrivateKey"
	labelResponsePad    = "CredentialResponsePad"
	labelPreamble       = "OPAQUEv1-"
	labelPrefix         = "OPAQUE-"
	labelHandshake      = "HandshakeSecret"
	labelSessionKey     = "SessionKey"
	labelServerMAC      = "ServerMAC"
	labelClientMAC      = "ClientMAC"
	diffieHellmanShares = 3
	lengthPrefixLen     = 2
	// credentialsLen is CleartextCredentials with both identities set to the public keys.
	credentialsLen = ElementLen + 2*(lengthPrefixLen+ElementLen)
)

var (
	// ErrEnvelopeRecovery is EnvelopeRecoveryError of RFC 9807: the envelope does not open, so the password is wrong.
	ErrEnvelopeRecovery = errors.New("opaque: the envelope does not open")
	// ErrServerAuthentication is ServerAuthenticationError: KE2 did not come from the server of the registration.
	ErrServerAuthentication = errors.New("opaque: the server did not authenticate")
	// ErrClientAuthentication is ClientAuthenticationError: KE3 does not match.
	ErrClientAuthentication = errors.New("opaque: the client did not authenticate")
	errLength               = errors.New("opaque: a message has the wrong length")
)

// Stretch is the key stretching function of a configuration (RFC 9807 section 2.3).
type Stretch func(message []byte) ([]byte, error)

// Identity is the stretching function of the RFC 9807 test vectors and of Nylzo's profile without a passphrase.
func Identity(message []byte) ([]byte, error) {
	return append([]byte(nil), message...), nil
}

// Config holds what client and server agree on beyond the fixed primitives; both identities are left out, so each
// defaults to its public key (RFC 9807 section 4).
type Config struct {
	Context []byte
	Stretch Stretch
}

// ServerKeys are the keys a server uses for one credential.
type ServerKeys struct {
	PrivateKey *ristretto255.Scalar
	PublicKey  [ElementLen]byte
	OPRFSeed   [OPRFSeedLen]byte
}

// Wipe clears the private key and the OPRF seed once a request is done with them.
func (k *ServerKeys) Wipe() {
	if k.PrivateKey != nil {
		k.PrivateKey.Zero()
	}
	clear(k.OPRFSeed[:])
}

// DeriveServerKeys derives the AKE key pair from a seed of SeedLen bytes (DeriveDiffieHellmanKeyPair).
func DeriveServerKeys(akeSeed []byte, oprfSeed *[OPRFSeedLen]byte) (ServerKeys, error) {
	private, public, err := deriveKeyPair(akeSeed, infoDiffieHellman)
	if err != nil {
		return ServerKeys{}, err
	}
	return ServerKeys{PrivateKey: private, PublicKey: [ElementLen]byte(public.Bytes()), OPRFSeed: *oprfSeed}, nil
}

// RegistrationState is what a client keeps between its registration request and the response.
type RegistrationState struct {
	password []byte
	blind    *ristretto255.Scalar
}

// Wipe clears the password and the blind.
func (s *RegistrationState) Wipe() {
	clear(s.password)
	if s.blind != nil {
		s.blind.Zero()
	}
}

// CreateRegistrationRequest is RFC 9807 section 5.2.1.
func CreateRegistrationRequest(password []byte) ([RegistrationRequestLen]byte, RegistrationState, error) {
	blindScalar := NewBlind()
	defer clear(blindScalar[:])
	return CreateRegistrationRequestWith(password, &blindScalar)
}

// CreateRegistrationRequestWith takes the blind as an input, for test vectors.
func CreateRegistrationRequestWith(
	password []byte,
	blindScalar *[ScalarLen]byte,
) ([RegistrationRequestLen]byte, RegistrationState, error) {
	factor, err := blindFrom(blindScalar)
	if err != nil {
		return [RegistrationRequestLen]byte{}, RegistrationState{}, err
	}
	blinded, err := blind(password, factor)
	if err != nil {
		return [RegistrationRequestLen]byte{}, RegistrationState{}, err
	}
	state := RegistrationState{password: append([]byte(nil), password...), blind: factor}
	return [RegistrationRequestLen]byte(blinded), state, nil
}

func oprfKey(seed *[OPRFSeedLen]byte, credentialIdentifier []byte) (*ristretto255.Scalar, error) {
	info := append(append([]byte(nil), credentialIdentifier...), labelOPRFKey...)
	derived, err := hkdf.Expand(sha512.New, seed[:], string(info), ScalarLen)
	if err != nil {
		return nil, err
	}
	defer clear(derived)
	key, _, err := deriveKeyPair(derived, infoDeriveKeyPair)
	return key, err
}

// CreateRegistrationResponse is RFC 9807 section 5.2.2.
func CreateRegistrationResponse(
	request []byte,
	keys *ServerKeys,
	credentialIdentifier []byte,
) ([RegistrationResponseLen]byte, error) {
	var response [RegistrationResponseLen]byte
	key, err := oprfKey(&keys.OPRFSeed, credentialIdentifier)
	if err != nil {
		return response, err
	}
	defer key.Zero()
	evaluated, err := blindEvaluate(key, request)
	if err != nil {
		return response, err
	}
	copy(response[:ElementLen], evaluated)
	copy(response[ElementLen:], keys.PublicKey[:])
	return response, nil
}

// Registration is what a finished registration gives the client: the record for the server and export_key.
type Registration struct {
	Record    [RecordLen]byte
	ExportKey [ExportKeyLen]byte
}

func (r *Registration) Wipe() {
	clear(r.ExportKey[:])
}

// FinalizeRegistrationRequest is RFC 9807 section 5.2.3.
func FinalizeRegistrationRequest(state *RegistrationState, response []byte, config *Config) (Registration, error) {
	var nonce [NonceLen]byte
	rand.Read(nonce[:])
	return FinalizeRegistrationRequestWith(state, response, config, &nonce)
}

// FinalizeRegistrationRequestWith takes the envelope nonce as an input, for test vectors.
func FinalizeRegistrationRequestWith(
	state *RegistrationState,
	response []byte,
	config *Config,
	nonce *[NonceLen]byte,
) (Registration, error) {
	var finished Registration
	if len(response) != RegistrationResponseLen {
		return finished, errLength
	}
	serverPublicKey := response[ElementLen:]
	if _, err := DeserializeElement(serverPublicKey); err != nil {
		return finished, err
	}
	randomized, err := randomizedPassword(state.password, state.blind, response[:ElementLen], config.Stretch)
	if err != nil {
		return finished, err
	}
	defer clear(randomized)
	stored, err := store(randomized, serverPublicKey, nonce)
	if err != nil {
		return finished, err
	}
	defer stored.wipe()
	copy(finished.Record[:ElementLen], stored.clientPublicKey)
	copy(finished.Record[ElementLen:ElementLen+HashLen], stored.maskingKey)
	copy(finished.Record[ElementLen+HashLen:], stored.envelope)
	copy(finished.ExportKey[:], stored.exportKey)
	return finished, nil
}

func randomizedPassword(
	password []byte,
	factor *ristretto255.Scalar,
	evaluated []byte,
	stretch Stretch,
) ([]byte, error) {
	output, err := finalize(password, factor, evaluated)
	if err != nil {
		return nil, err
	}
	defer clear(output)
	stretched, err := stretch(output)
	if err != nil {
		return nil, err
	}
	defer clear(stretched)
	ikm := append(append(make([]byte, 0, len(output)+len(stretched)), output...), stretched...)
	defer clear(ikm)
	return hkdf.Extract(sha512.New, ikm, nil)
}

type stored struct {
	envelope        []byte
	clientPublicKey []byte
	clientPrivate   *ristretto255.Scalar
	maskingKey      []byte
	exportKey       []byte
	authKey         []byte
}

func (s *stored) wipe() {
	clear(s.maskingKey)
	clear(s.exportKey)
	clear(s.authKey)
	if s.clientPrivate != nil {
		s.clientPrivate.Zero()
	}
}

func expand(secret, info []byte, length int) ([]byte, error) {
	return hkdf.Expand(sha512.New, secret, string(info), length)
}

func withNonce(nonce []byte, label string) []byte {
	return append(append(make([]byte, 0, len(nonce)+len(label)), nonce...), label...)
}

// store is Store of RFC 9807 section 4.1.2 and, with the stored nonce, Recover of section 4.1.3.
func store(randomized, serverPublicKey []byte, nonce *[NonceLen]byte) (*stored, error) {
	result := &stored{}
	var err error
	if result.maskingKey, err = expand(randomized, []byte(labelMaskingKey), HashLen); err != nil {
		return nil, err
	}
	if result.authKey, err = expand(randomized, withNonce(nonce[:], labelAuthKey), HashLen); err != nil {
		return nil, err
	}
	if result.exportKey, err = expand(randomized, withNonce(nonce[:], labelExportKey), HashLen); err != nil {
		return nil, err
	}
	seed, err := expand(randomized, withNonce(nonce[:], labelPrivateKey), SeedLen)
	if err != nil {
		return nil, err
	}
	defer clear(seed)
	private, public, err := deriveKeyPair(seed, infoDiffieHellman)
	if err != nil {
		return nil, err
	}
	result.clientPrivate, result.clientPublicKey = private, public.Bytes()
	tag := hmac.New(sha512.New, result.authKey)
	tag.Write(nonce[:])
	tag.Write(cleartextCredentials(serverPublicKey, result.clientPublicKey))
	result.envelope = append(append(make([]byte, 0, EnvelopeLen), nonce[:]...), tag.Sum(nil)...)
	return result, nil
}

// cleartextCredentials serializes CleartextCredentials with both identities set to the public keys.
func cleartextCredentials(serverPublicKey, clientPublicKey []byte) []byte {
	encoded := make([]byte, 0, credentialsLen)
	encoded = append(encoded, serverPublicKey...)
	encoded = appendWithLength(encoded, serverPublicKey)
	return appendWithLength(encoded, clientPublicKey)
}

func appendWithLength(encoded, field []byte) []byte {
	return append(appendLength16(encoded, len(field)), field...)
}

// ClientLogin is the client state between KE1 and KE3 (ClientState of RFC 9807).
type ClientLogin struct {
	password     []byte
	blind        *ristretto255.Scalar
	clientSecret *ristretto255.Scalar
	ke1          [KE1Len]byte
}

func (c *ClientLogin) Wipe() {
	clear(c.password)
	if c.blind != nil {
		c.blind.Zero()
	}
	if c.clientSecret != nil {
		c.clientSecret.Zero()
	}
}

// GenerateKE1 is RFC 9807 section 6.2.1.
func GenerateKE1(password []byte) ([KE1Len]byte, ClientLogin, error) {
	var nonce [NonceLen]byte
	var seed [SeedLen]byte
	rand.Read(nonce[:])
	rand.Read(seed[:])
	defer clear(seed[:])
	blindScalar := NewBlind()
	defer clear(blindScalar[:])
	return GenerateKE1With(password, &blindScalar, &nonce, &seed)
}

// GenerateKE1With takes the blind, the client nonce and the keyshare seed as inputs, for test vectors.
func GenerateKE1With(
	password []byte,
	blindScalar *[ScalarLen]byte,
	nonce *[NonceLen]byte,
	keyshareSeed *[SeedLen]byte,
) ([KE1Len]byte, ClientLogin, error) {
	var ke1 [KE1Len]byte
	factor, err := blindFrom(blindScalar)
	if err != nil {
		return ke1, ClientLogin{}, err
	}
	blinded, err := blind(password, factor)
	if err != nil {
		return ke1, ClientLogin{}, err
	}
	secret, keyshare, err := deriveKeyPair(keyshareSeed[:], infoDiffieHellman)
	if err != nil {
		return ke1, ClientLogin{}, err
	}
	copy(ke1[:ElementLen], blinded)
	copy(ke1[ElementLen:ElementLen+NonceLen], nonce[:])
	copy(ke1[ElementLen+NonceLen:], keyshare.Bytes())
	login := ClientLogin{password: append([]byte(nil), password...), blind: factor, clientSecret: secret, ke1: ke1}
	return ke1, login, nil
}

// ClientResult is what a successful KE3 gives the client.
type ClientResult struct {
	KE3        [KE3Len]byte
	SessionKey [HashLen]byte
	ExportKey  [ExportKeyLen]byte
}

func (r *ClientResult) Wipe() {
	clear(r.SessionKey[:])
	clear(r.ExportKey[:])
}

// GenerateKE3 is RFC 9807 section 6.2.3: RecoverCredentials, then AuthClientFinalize.
func (c *ClientLogin) GenerateKE3(ke2 []byte, config *Config) (ClientResult, error) {
	var result ClientResult
	if len(ke2) != KE2Len {
		return result, errLength
	}
	response := ke2[:credentialResponseLen]
	auth := ke2[credentialResponseLen:]
	serverNonce, serverKeyshare := auth[:NonceLen], auth[NonceLen:NonceLen+ElementLen]
	serverMAC := auth[NonceLen+ElementLen:]
	serverKeyshareElement, err := DeserializeElement(serverKeyshare)
	if err != nil {
		return result, err
	}
	recovered, serverPublicKey, err := c.recoverCredentials(response, config.Stretch)
	if err != nil {
		return result, err
	}
	defer recovered.wipe()
	serverPublicElement, err := DeserializeElement(serverPublicKey)
	if err != nil {
		return result, err
	}
	ikm := diffieHellman(
		[]*ristretto255.Scalar{c.clientSecret, c.clientSecret, recovered.clientPrivate},
		[]*ristretto255.Element{serverKeyshareElement, serverPublicElement, serverKeyshareElement},
	)
	defer clear(ikm)
	preamble := buildPreamble(
		config.Context, recovered.clientPublicKey, c.ke1[:], serverPublicKey, response, serverNonce, serverKeyshare,
	)
	keys, err := deriveKeys(ikm, preamble)
	if err != nil {
		return result, err
	}
	defer keys.wipe()
	expectedServerMAC := mac(keys.serverMAC, preambleHash(preamble))
	if !hmac.Equal(serverMAC, expectedServerMAC) {
		return result, ErrServerAuthentication
	}
	copy(result.KE3[:], mac(keys.clientMAC, preambleHash(append(preamble, expectedServerMAC...))))
	copy(result.SessionKey[:], keys.sessionKey)
	copy(result.ExportKey[:], recovered.exportKey)
	return result, nil
}

func (c *ClientLogin) recoverCredentials(response []byte, stretch Stretch) (*stored, []byte, error) {
	randomized, err := randomizedPassword(c.password, c.blind, response[:ElementLen], stretch)
	if err != nil {
		return nil, nil, err
	}
	defer clear(randomized)
	maskingKey, err := expand(randomized, []byte(labelMaskingKey), HashLen)
	if err != nil {
		return nil, nil, err
	}
	defer clear(maskingKey)
	maskingNonce := response[ElementLen : ElementLen+NonceLen]
	pad, err := expand(maskingKey, withNonce(maskingNonce, labelResponsePad), maskedResponseLen)
	if err != nil {
		return nil, nil, err
	}
	defer clear(pad)
	unmasked := make([]byte, maskedResponseLen)
	subtle.XORBytes(unmasked, pad, response[ElementLen+NonceLen:])
	serverPublicKey := unmasked[:ElementLen]
	envelope := unmasked[ElementLen:]
	nonce := [NonceLen]byte(envelope[:NonceLen])
	recovered, err := store(randomized, serverPublicKey, &nonce)
	if err != nil {
		return nil, nil, err
	}
	if !hmac.Equal(recovered.envelope, envelope) {
		recovered.wipe()
		return nil, nil, ErrEnvelopeRecovery
	}
	return recovered, serverPublicKey, nil
}

// ServerLogin is what a server keeps between KE2 and KE3 (ServerState of RFC 9807).
type ServerLogin struct {
	ExpectedClientMAC [MACLen]byte
	SessionKey        [HashLen]byte
}

// ServerFinish is RFC 9807 section 6.2.4.
func (s *ServerLogin) ServerFinish(ke3 []byte) error {
	if len(ke3) != KE3Len || !hmac.Equal(ke3, s.ExpectedClientMAC[:]) {
		return ErrClientAuthentication
	}
	return nil
}

func (s *ServerLogin) Wipe() {
	clear(s.ExpectedClientMAC[:])
	clear(s.SessionKey[:])
}

// GenerateKE2 is RFC 9807 section 6.2.2 for a stored record.
func GenerateKE2(
	keys *ServerKeys,
	record []byte,
	credentialIdentifier []byte,
	ke1 []byte,
	config *Config,
) ([KE2Len]byte, ServerLogin, error) {
	var maskingNonce, serverNonce [NonceLen]byte
	var keyshareSeed [SeedLen]byte
	rand.Read(maskingNonce[:])
	rand.Read(serverNonce[:])
	rand.Read(keyshareSeed[:])
	defer clear(keyshareSeed[:])
	return GenerateKE2With(keys, record, credentialIdentifier, ke1, config, &ServerRandomness{
		MaskingNonce: maskingNonce, ServerNonce: serverNonce, KeyshareSeed: keyshareSeed,
	})
}

// ServerRandomness is what GenerateKE2 draws; GenerateKE2With takes it as an input, for test vectors.
type ServerRandomness struct {
	MaskingNonce [NonceLen]byte
	ServerNonce  [NonceLen]byte
	KeyshareSeed [SeedLen]byte
}

func GenerateKE2With(
	keys *ServerKeys,
	record []byte,
	credentialIdentifier []byte,
	ke1 []byte,
	config *Config,
	random *ServerRandomness,
) ([KE2Len]byte, ServerLogin, error) {
	var ke2 [KE2Len]byte
	if len(record) != RecordLen || len(ke1) != KE1Len {
		return ke2, ServerLogin{}, errLength
	}
	clientPublicKey := record[:ElementLen]
	clientPublicElement, err := DeserializeElement(clientPublicKey)
	if err != nil {
		return ke2, ServerLogin{}, err
	}
	clientKeyshare, err := DeserializeElement(ke1[ElementLen+NonceLen:])
	if err != nil {
		return ke2, ServerLogin{}, err
	}
	response, err := createCredentialResponse(
		keys,
		record,
		credentialIdentifier,
		ke1[:ElementLen],
		&random.MaskingNonce,
	)
	if err != nil {
		return ke2, ServerLogin{}, err
	}
	secret, keyshare, err := deriveKeyPair(random.KeyshareSeed[:], infoDiffieHellman)
	if err != nil {
		return ke2, ServerLogin{}, err
	}
	defer secret.Zero()
	keyshareBytes := keyshare.Bytes()
	preamble := buildPreamble(
		config.Context, clientPublicKey, ke1, keys.PublicKey[:], response, random.ServerNonce[:], keyshareBytes,
	)
	ikm := diffieHellman(
		[]*ristretto255.Scalar{secret, keys.PrivateKey, secret},
		[]*ristretto255.Element{clientKeyshare, clientKeyshare, clientPublicElement},
	)
	defer clear(ikm)
	derived, err := deriveKeys(ikm, preamble)
	if err != nil {
		return ke2, ServerLogin{}, err
	}
	defer derived.wipe()
	serverMAC := mac(derived.serverMAC, preambleHash(preamble))
	var login ServerLogin
	copy(login.ExpectedClientMAC[:], mac(derived.clientMAC, preambleHash(append(preamble, serverMAC...))))
	copy(login.SessionKey[:], derived.sessionKey)
	encoded := append(append(append(response, random.ServerNonce[:]...), keyshareBytes...), serverMAC...)
	copy(ke2[:], encoded)
	return ke2, login, nil
}

// createCredentialResponse is CreateCredentialResponse of RFC 9807 section 6.3.2.2 for a stored record.
func createCredentialResponse(
	keys *ServerKeys,
	record []byte,
	credentialIdentifier []byte,
	blinded []byte,
	maskingNonce *[NonceLen]byte,
) ([]byte, error) {
	key, err := oprfKey(&keys.OPRFSeed, credentialIdentifier)
	if err != nil {
		return nil, err
	}
	defer key.Zero()
	evaluated, err := blindEvaluate(key, blinded)
	if err != nil {
		return nil, err
	}
	maskingKey := record[ElementLen : ElementLen+HashLen]
	pad, err := expand(maskingKey, withNonce(maskingNonce[:], labelResponsePad), maskedResponseLen)
	if err != nil {
		return nil, err
	}
	defer clear(pad)
	plain := append(append(make([]byte, 0, maskedResponseLen), keys.PublicKey[:]...), record[ElementLen+HashLen:]...)
	masked := make([]byte, maskedResponseLen)
	subtle.XORBytes(masked, pad, plain)
	response := make([]byte, 0, credentialResponseLen)
	response = append(response, evaluated...)
	response = append(response, maskingNonce[:]...)
	return append(response, masked...), nil
}

func diffieHellman(scalars []*ristretto255.Scalar, elements []*ristretto255.Element) []byte {
	ikm := make([]byte, 0, diffieHellmanShares*ElementLen)
	for i := range scalars {
		ikm = append(ikm, ristretto255.NewIdentityElement().ScalarMult(scalars[i], elements[i]).Bytes()...)
	}
	return ikm
}

func buildPreamble(
	context, clientPublicKey, ke1, serverPublicKey, credentialResponse, serverNonce, serverKeyshare []byte,
) []byte {
	preamble := make([]byte, 0, len(labelPreamble)+len(context)+KE1Len+KE2Len+3*ElementLen)
	preamble = append(preamble, labelPreamble...)
	preamble = appendWithLength(preamble, context)
	preamble = appendWithLength(preamble, clientPublicKey)
	preamble = append(preamble, ke1...)
	preamble = appendWithLength(preamble, serverPublicKey)
	preamble = append(preamble, credentialResponse...)
	preamble = append(preamble, serverNonce...)
	return append(preamble, serverKeyshare...)
}

func preambleHash(preamble []byte) []byte {
	digest := sha512.Sum512(preamble)
	return digest[:]
}

func mac(key, message []byte) []byte {
	tag := hmac.New(sha512.New, key)
	tag.Write(message)
	return tag.Sum(nil)
}

type akeKeys struct {
	serverMAC  []byte
	clientMAC  []byte
	sessionKey []byte
}

func (k *akeKeys) wipe() {
	clear(k.serverMAC)
	clear(k.clientMAC)
	clear(k.sessionKey)
}

// deriveKeys is DeriveKeys of RFC 9807 section 6.4.2.2.
func deriveKeys(ikm, preamble []byte) (*akeKeys, error) {
	prk, err := hkdf.Extract(sha512.New, ikm, nil)
	if err != nil {
		return nil, err
	}
	defer clear(prk)
	transcript := preambleHash(preamble)
	handshake, err := expandLabel(prk, labelHandshake, transcript)
	if err != nil {
		return nil, err
	}
	defer clear(handshake)
	keys := &akeKeys{}
	if keys.sessionKey, err = expandLabel(prk, labelSessionKey, transcript); err != nil {
		return nil, err
	}
	if keys.serverMAC, err = expandLabel(handshake, labelServerMAC, nil); err != nil {
		return nil, err
	}
	if keys.clientMAC, err = expandLabel(handshake, labelClientMAC, nil); err != nil {
		return nil, err
	}
	return keys, nil
}

// expandLabel is Derive-Secret of RFC 9807 section 6.4.2.1: Expand-Label with Nx bytes of output.
func expandLabel(secret []byte, label string, context []byte) ([]byte, error) {
	full := labelPrefix + label
	info := appendLength16(nil, HashLen)
	info = append(info, lengthByte(len(full)))
	info = append(info, full...)
	info = append(info, lengthByte(len(context)))
	info = append(info, context...)
	return expand(secret, info, HashLen)
}
