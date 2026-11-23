package opaque

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/gtank/ristretto255"
)

var errStretch = errors.New("the stretching function failed")

func failingStretch([]byte) ([]byte, error) { return nil, errStretch }

type pendingRegistration struct {
	state    RegistrationState
	response [RegistrationResponseLen]byte
}

func registrationFor(t *testing.T, password []byte, run *rfcRun) pendingRegistration {
	t.Helper()
	request, state, err := CreateRegistrationRequest(password)
	if err != nil {
		t.Fatal(err)
	}
	response, err := CreateRegistrationResponse(request[:], &run.keys, run.credential)
	if err != nil {
		t.Fatal(err)
	}
	return pendingRegistration{state: state, response: response}
}

func TestClientRefusesAMalformedRegistrationResponse(t *testing.T) {
	vector, run := rfcLogin(t)
	password := unhex(t, vector.Password)
	identity := make([]byte, ElementLen)
	pending := registrationFor(t, password, run)
	response := pending.response
	withServerKey := bytes.Clone(response[:])
	copy(withServerKey[ElementLen:], identity)
	withEvaluation := bytes.Clone(response[:])
	copy(withEvaluation, identity)
	for name, test := range map[string]struct {
		response []byte
		stretch  Stretch
		want     error
	}{
		"a short response":                          {response[:RegistrationResponseLen-1], Identity, errLength},
		"a server key that is not an element":       {withServerKey, Identity, ErrDeserialize},
		"an evaluated element that is the identity": {withEvaluation, Identity, ErrDeserialize},
		"a stretching function that fails":          {response[:], failingStretch, errStretch},
	} {
		config := Config{Context: run.config.Context, Stretch: test.stretch}
		if _, err := FinalizeRegistrationRequest(&pending.state, test.response, &config); !errors.Is(err, test.want) {
			t.Errorf("%s: got %v, want %v", name, err, test.want)
		}
	}
}

func TestKE3StopsWhenTheStretchingFunctionFails(t *testing.T) {
	_, run := rfcLogin(t)
	failing := Config{Context: run.config.Context, Stretch: failingStretch}
	if _, err := run.login.GenerateKE3(run.ke2[:], &failing); !errors.Is(err, errStretch) {
		t.Fatalf("got %v, want the stretching error", err)
	}
}

func TestKE1RefusesAZeroBlind(t *testing.T) {
	vector, _ := rfcLogin(t)
	if _, _, err := GenerateKE1With(
		unhex(t, vector.Password),
		new([ScalarLen]byte),
		new([NonceLen]byte),
		new([SeedLen]byte),
	); !errors.Is(err, ErrDeserialize) {
		t.Fatalf("got %v, want ErrDeserialize", err)
	}
}

func TestKE2RefusesMalformedInput(t *testing.T) {
	_, run := rfcLogin(t)
	withBlinded := bytes.Clone(run.ke1[:])
	copy(withBlinded, make([]byte, ElementLen))
	for name, test := range map[string]struct {
		record []byte
		ke1    []byte
		want   error
	}{
		"a short record":                         {run.record[:RecordLen-1], run.ke1[:], errLength},
		"a short KE1":                            {run.record[:], run.ke1[:KE1Len-1], errLength},
		"a blinded element that is the identity": {run.record[:], withBlinded, ErrDeserialize},
	} {
		if _, _, err := GenerateKE2(&run.keys, test.record, run.credential, test.ke1, &run.config); !errors.Is(
			err,
			test.want,
		) {
			t.Errorf("%s: got %v, want %v", name, err, test.want)
		}
	}
}

// A record whose envelope binds a server key that is no group element opens, so the client must still refuse that key.
func TestClientRefusesAServerKeyThatIsNoElementEvenWhenTheEnvelopeOpens(t *testing.T) {
	vector, run := rfcLogin(t)
	password := unhex(t, vector.Password)
	pending := registrationFor(t, password, run)
	notAnElement := bytes.Repeat([]byte{0xff}, ElementLen)
	randomized, err := randomizedPassword(
		pending.state.password,
		pending.state.blind,
		pending.response[:ElementLen],
		Identity,
	)
	if err != nil {
		t.Fatal(err)
	}
	stored := store(randomized, notAnElement, new([NonceLen]byte))
	record := bytes.Join([][]byte{stored.clientPublicKey, stored.maskingKey, stored.envelope}, nil)
	keys := run.keys
	copy(keys.PublicKey[:], notAnElement)
	ke1, login, err := GenerateKE1(password)
	if err != nil {
		t.Fatal(err)
	}
	ke2, _, err := GenerateKE2(&keys, record, run.credential, ke1[:], &run.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = login.GenerateKE3(ke2[:], &run.config); !errors.Is(err, ErrDeserialize) {
		t.Fatalf("got %v, want ErrDeserialize", err)
	}
}

// RFC 9497 section 3.2.1 retries with the next counter after a zero scalar and gives up after counter 255.
func TestDeriveKeyPairRetriesAZeroScalarAndGivesUpAfterCounter255(t *testing.T) {
	one := scalarOne(t)
	var tried []byte
	got := firstNonZeroScalar(func(counter byte) *ristretto255.Scalar {
		tried = append(tried, counter)
		if counter < 2 {
			return ristretto255.NewScalar()
		}
		return one
	})
	if got.Equal(one) != 1 || !bytes.Equal(tried, []byte{0, 1, 2}) {
		t.Fatalf("tried counters %v", tried)
	}
	defer func() {
		if recovered, _ := recover().(error); !errors.Is(recovered, errDeriveKeyPair) {
			t.Fatalf("recovered %v, want DeriveKeyPairError", recovered)
		}
	}()
	firstNonZeroScalar(func(byte) *ristretto255.Scalar { return ristretto255.NewScalar() })
}

func scalarOne(t *testing.T) *ristretto255.Scalar {
	t.Helper()
	encoded := make([]byte, ScalarLen)
	encoded[0] = 1
	scalar, err := ristretto255.NewScalar().SetCanonicalBytes(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return scalar
}

func TestBlindRefusesTheIdentity(t *testing.T) {
	if _, err := blindElement(ristretto255.NewIdentityElement(), scalarOne(t)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

func TestDeserializeElementRefusesAnotherLength(t *testing.T) {
	if _, err := DeserializeElement(make([]byte, ElementLen+1)); !errors.Is(err, ErrDeserialize) {
		t.Fatalf("got %v, want ErrDeserialize", err)
	}
}

// I2OSP and expand_message_xmd (RFC 9380 section 5.3.1) fail for values their encodings cannot hold.
func TestEncodingsRefuseValuesTheyCannotHold(t *testing.T) {
	for name, encode := range map[string]func(){
		"a negative two-byte length": func() { appendLength16(nil, -1) },
		"a two-byte length of 65536": func() { appendLength16(nil, 1<<16) },
		"a negative one-byte length": func() { lengthByte(-1) },
		"a one-byte length of 256":   func() { lengthByte(1 << 8) },
		"an expansion of 65536 bytes": func() {
			expandMessageXMD(nil, dstHashToGroup, 1<<16)
		},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: encoded", name)
				}
			}()
			encode()
		}()
	}
}

// The vectors of RFC 9380 appendix K.3, including outputs of two blocks, which Nylzo's 64-byte inputs never need.
func TestExpandMessageXMDMatchesRFC9380(t *testing.T) {
	const dst = "QUUX-V01-CS02-with-expander-SHA512-256"
	for _, vector := range []struct {
		message string
		length  int
		want    string
	}{
		{"abc", 0x20, "0da749f12fbe5483eb066a5f595055679b976e93abe9be6f0f6318bce7aca8dc"},
		{"", 0x80, "41b037d1734a5f8df225dd8c7de38f851efdb45c372887be655212d07251b921b052b62eaed99b46f72f2ef4cc96bfaf" +
			"254ebbbec091e1a3b9e4fb5e5b619d2e0c5414800a1d882b62bb5cd1778f098b8eb6cb399d5d9d18f5d5842cf5d13d7eb00a7cff" +
			"859b605da678b318bd0e65ebff70bec88c753b159a805d2c89c55961"},
		{"abc", 0x80, "7f1dddd13c08b543f2e2037b14cefb255b44c83cc397c1786d975653e36a6b11bdd7732d8b38adb4a0edc26a0cef4bb4" +
			"5217135456e58fbca1703cd6032cb1347ee720b87972d63fbf232587043ed2901bce7f22610c0419751c065922b4884318510413" +
			"10ad659e4b23520e1772ab29dcdeb2002222a363f0c2b1c972b3efe1"},
		{"abcdef0123456789", 0x80, "3f721f208e6199fe903545abc26c837ce59ac6fa45733f1baaf0222f8b7acb0424814fcb5eecf6c1d3" +
			"8f06e9d0a6ccfbf85ae612ab8735dfdf9ce84c372a77c8f9e1c1e952c3a61b7567dd0693016af51d2745822663d0c2367e3f4f0b" +
			"ed827feecc2aaf98c949b5ed0d35c3f1023d64ad1407924288d366ea159f46287e61ac"},
	} {
		if got := hex.EncodeToString(expandMessageXMD([]byte(vector.message), dst, vector.length)); got != vector.want {
			t.Errorf("%q, %d bytes: %s", vector.message, vector.length, got)
		}
	}
}

func TestWipeClearsEverySecretOfAnExchange(t *testing.T) {
	vector, run := rfcLogin(t)
	password := unhex(t, vector.Password)
	pending := registrationFor(t, password, run)
	registration, err := FinalizeRegistrationRequest(&pending.state, pending.response[:], &run.config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := run.login.GenerateKE3(run.ke2[:], &run.config)
	if err != nil {
		t.Fatal(err)
	}
	keys := vectorServerKeys(t, vector)
	for _, wipe := range []func(){
		keys.Wipe, pending.state.Wipe, registration.Wipe, run.login.Wipe, result.Wipe, run.serverLogin.Wipe,
	} {
		wipe()
	}
	zero := ristretto255.NewScalar()
	if keys.PrivateKey.Equal(zero) != 1 || pending.state.blind.Equal(zero) != 1 ||
		run.login.blind.Equal(zero) != 1 || run.login.clientSecret.Equal(zero) != 1 {
		t.Fatal("a scalar survived its wipe")
	}
	for name, secret := range map[string][]byte{
		"OPRF seed":             keys.OPRFSeed[:],
		"registration password": pending.state.password,
		"export key":            registration.ExportKey[:],
		"login password":        run.login.password,
		"login export key":      result.ExportKey[:],
		"expected client MAC":   run.serverLogin.ExpectedClientMAC[:],
	} {
		if !bytes.Equal(secret, make([]byte, len(secret))) {
			t.Errorf("the %s survived its wipe", name)
		}
	}
	for _, empty := range []func(){new(ServerKeys).Wipe, new(RegistrationState).Wipe, new(ClientLogin).Wipe} {
		empty()
	}
}
