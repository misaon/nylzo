package vectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/crypto/scrypt"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	Spec      = "nylzo/v1"
	Generator = "go run ./cmd/nylzo-vectors -out spec/vectors.json"
	seed      = "nylzo/vectors/2026-10-22"
	inlineMax = 70000
)

type File struct {
	Spec      string     `json:"spec"`
	Generator string     `json:"generator"`
	Scrypt    []Scrypt   `json:"scrypt_rfc7914"`
	Derive    []Derive   `json:"derive"`
	Envelope  []Envelope `json:"envelope"`
	Link      []Link     `json:"link"`
	Meta      []MetaCase `json:"meta"`
	Strength  []Strength `json:"passphrase_strength"`
	Request   []Request  `json:"request"`
	Server    Server     `json:"server"`
}

type Strength struct {
	Name       string `json:"name"`
	Passphrase string `json:"passphrase"`
	Accepted   bool   `json:"accepted"`
}

type Scrypt struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Salt     string `json:"salt"`
	N        int    `json:"N"`
	R        int    `json:"r"`
	P        int    `json:"p"`
	DKLen    int    `json:"dkLen"`
	DK       string `json:"dk_hex"`
	Heavy    bool   `json:"heavy"`
}

type Derive struct {
	Name        string  `json:"name"`
	ID          string  `json:"id"`
	S           string  `json:"s"`
	KDFID       int     `json:"kdf_id"`
	Passphrase  *string `json:"passphrase,omitempty"`
	HSalt       string  `json:"hsalt_hex"`
	R           string  `json:"r_hex"`
	SaltPW      string  `json:"salt_pw_hex"`
	P           string  `json:"p_hex"`
	K           string  `json:"k_hex"`
	V           string  `json:"v_hex"`
	M           string  `json:"m_hex"`
	Fragment    string  `json:"fragment"`
	ReadKey     string  `json:"read_key"`
	VerifierKey *string `json:"verifier_key,omitempty"`
	ManageKey   string  `json:"manage_key"`
}

type Request struct {
	Name          string `json:"name"`
	Seed          string `json:"seed_hex"`
	PublicKey     string `json:"public_key"`
	Purpose       string `json:"purpose"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	Challenge     string `json:"challenge"`
	Body          string `json:"body_hex"`
	Message       string `json:"message_hex"`
	Signature     string `json:"signature"`
	Authorization string `json:"authorization,omitempty"`
	Verifier      string `json:"verifier_header,omitempty"`
}

type Envelope struct {
	Name         string       `json:"name"`
	ID           string       `json:"id"`
	S            string       `json:"s"`
	KDFID        int          `json:"kdf_id"`
	Passphrase   *string      `json:"passphrase,omitempty"`
	Flags        int          `json:"flags"`
	IV           string       `json:"iv_hex"`
	OpensMax     int          `json:"opens_max"`
	TTLSeconds   int          `json:"ttl_s"`
	Text         *string      `json:"text,omitempty"`
	File         *FileContent `json:"file,omitempty"`
	MetaJSON     string       `json:"meta_json"`
	PlaintextLen int          `json:"plaintext_len"`
	AAD          string       `json:"aad_hex"`
	ObjectLen    int          `json:"object_len"`
	ObjectSHA256 string       `json:"object_sha256"`
	Object       *string      `json:"object_b64,omitempty"`
}

type FileContent struct {
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Size    int    `json:"size"`
	Content string `json:"content"`
}

type Link struct {
	Name      string  `json:"name"`
	Input     string  `json:"input"`
	Valid     bool    `json:"valid"`
	Canonical bool    `json:"canonical,omitempty"`
	ID        *string `json:"id,omitempty"`
	KDFID     *int    `json:"kdf_id,omitempty"`
	S         *string `json:"s_hex,omitempty"`
}

type MetaCase struct {
	Name      string  `json:"name"`
	Input     string  `json:"input"`
	Valid     bool    `json:"valid"`
	Canonical *string `json:"canonical,omitempty"`
	Text      *string `json:"text,omitempty"`
	FileName  *string `json:"file_name,omitempty"`
	FileMime  *string `json:"file_mime,omitempty"`
	FileSize  *int64  `json:"file_size,omitempty"`
}

type Server struct {
	Pepper           string    `json:"pepper_hex"`
	PepperID         int       `json:"pepper_id"`
	PreviousPepper   string    `json:"previous_pepper_hex"`
	PreviousPepperID int       `json:"previous_pepper_id"`
	MACs             []MACCase `json:"macs"`
}

type MACCase struct {
	Purpose string `json:"purpose"`
	Input   string `json:"input_hex"`
	MAC     string `json:"mac_hex"`
}

func det(label string, n int) []byte {
	out := make([]byte, 0, n+32)
	var ctr uint32
	for len(out) < n {
		sum := sha256.Sum256(binary.BigEndian.AppendUint32([]byte(seed+label), ctr))
		out = append(out, sum[:]...)
		ctr++
	}
	return out[:n]
}

func detID(label string) core.ID {
	return core.ID(det("id/"+label, core.IDLen))
}

func det32(label string) *[32]byte {
	return (*[32]byte)(det(label, 32))
}

func detIV(label string) [core.IVLen]byte {
	return [core.IVLen]byte(det("iv/"+label, core.IVLen))
}

func counterBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func Generate() (*File, error) {
	f := &File{Spec: Spec, Generator: Generator}
	for _, step := range []func() error{f.scrypt, f.derive, f.envelope, f.link, f.meta, f.strength, f.request, f.server} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func Marshal(f *File) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *File) scrypt() error {
	cases := []struct {
		name, pw, salt string
		n, r, p        int
		heavy          bool
	}{
		{"rfc7914-1", "", "", 16, 1, 1, false},
		{"rfc7914-2", "password", "NaCl", 1024, 8, 16, false},
		{"rfc7914-3", "pleaseletmein", "SodiumChloride", 16384, 8, 1, false},
		{"rfc7914-4", "pleaseletmein", "SodiumChloride", 1048576, 8, 1, true},
	}
	for _, c := range cases {
		dk, err := scrypt.Key([]byte(c.pw), []byte(c.salt), c.n, c.r, c.p, 64)
		if err != nil {
			return err
		}
		f.Scrypt = append(f.Scrypt, Scrypt{Name: c.name, Password: c.pw, Salt: c.salt, N: c.n, R: c.r, P: c.p, DKLen: 64, DK: hex.EncodeToString(dk), Heavy: c.heavy})
	}
	return nil
}

type deriveCase struct {
	name string
	kdf  core.KDF
	pw   *string
}

func deriveCases() []deriveCase {
	return []deriveCase{
		{"no-passphrase", core.KDFNone, nil},
		{"scrypt-ascii", core.KDFScrypt, new("correct horse battery staple")},
		{"scrypt-nfc-composed", core.KDFScrypt, new("héslo s češtinou")},
		{"scrypt-nfc-decomposed", core.KDFScrypt, new("héslo s češtinou")},
		{"pbkdf2-ascii", core.KDFPBKDF2, new("password")},
		{"pbkdf2-emoji", core.KDFPBKDF2, new("\U0001F510 tajné heslo")},
		{"pbkdf2-empty", core.KDFPBKDF2, new("")},
	}
}

func (f *File) derive() error {
	for _, c := range deriveCases() {
		label := c.name
		if strings.HasPrefix(c.name, "scrypt-nfc") {
			label = "scrypt-nfc"
		}
		id := detID(label)
		s := det32("s/" + label)
		m := det32("m/" + label)
		pw := ""
		if c.pw != nil {
			pw = *c.pw
		}
		link, err := core.DeriveLink(id, s)
		if err != nil {
			return err
		}
		p, err := core.DerivePassphraseKey(c.kdf, pw, &link.SaltPW)
		if err != nil {
			return err
		}
		content, err := core.DeriveContent(id, s, p)
		if err != nil {
			return err
		}
		hs := core.HSalt(id)
		readKey, manageKey := core.PublicKey(&link.R), core.PublicKey(m)
		d := Derive{
			Name: c.name, ID: id.String(), S: hex.EncodeToString(s[:]), KDFID: int(c.kdf), Passphrase: c.pw,
			HSalt: hex.EncodeToString(hs[:]), R: hex.EncodeToString(link.R[:]), SaltPW: hex.EncodeToString(link.SaltPW[:]),
			P: hex.EncodeToString(p), K: hex.EncodeToString(content.K[:]), V: hex.EncodeToString(content.V[:]), M: hex.EncodeToString(m[:]),
			Fragment: core.Fragment(c.kdf, s),
			ReadKey:  core.Encode(readKey[:]), ManageKey: core.Encode(manageKey[:]),
		}
		if c.kdf != core.KDFNone {
			verifierKey := core.PublicKey(&content.V)
			d.VerifierKey = new(core.Encode(verifierKey[:]))
		}
		f.Derive = append(f.Derive, d)
	}
	return nil
}

type envCase struct {
	name     string
	kdf      core.KDF
	pw       *string
	opens    uint16
	ttl      uint32
	text     *string
	fileName string
	fileMime string
	fileSize int
	fileHex  bool
}

func envCases() []envCase {
	return []envCase{
		{name: "text-ascii", kdf: core.KDFNone, opens: 1, ttl: 86400, text: new("hello nylzo")},
		{name: "text-escapes", kdf: core.KDFNone, opens: 2, ttl: 3600, text: new("český text \"quoted\" back\\slash\nnewline\ttab \u0001ctrl /slash   line sep \U0001F600")},
		{name: "text-exact-4096", kdf: core.KDFNone, opens: 1, ttl: 86400, text: new(strings.Repeat("a", 4075))},
		{name: "text-4097", kdf: core.KDFNone, opens: 1, ttl: 86400, text: new(strings.Repeat("a", 4076))},
		{name: "file-empty", kdf: core.KDFNone, opens: 1, ttl: 86400, fileName: "empty.bin", fileMime: "application/octet-stream", fileSize: 0},
		{name: "file-one-byte", kdf: core.KDFNone, opens: 1, ttl: 86400, fileName: "b.bin", fileMime: "application/octet-stream", fileSize: 1, fileHex: true},
		{name: "file-65536", kdf: core.KDFNone, opens: 3, ttl: 259200, fileName: "block.bin", fileMime: "", fileSize: 65536},
		{name: "file-and-text-pbkdf2", kdf: core.KDFPBKDF2, pw: new("password"), opens: 1, ttl: 86400, text: new("příloha v příloze"), fileName: "příloha č.1.pdf", fileMime: "application/pdf", fileSize: 1000, fileHex: true},
		{name: "file-5mib", kdf: core.KDFNone, opens: 1, ttl: 604800, fileName: "big.bin", fileMime: "application/octet-stream", fileSize: core.MaxFileBytes},
		{name: "scrypt-text", kdf: core.KDFScrypt, pw: new("correct horse battery staple"), opens: 1, ttl: 86400, text: new("scrypt protected")},
		{name: "scrypt-nfc-decomposed", kdf: core.KDFScrypt, pw: new("héslo s češtinou"), opens: 1, ttl: 86400, text: new("nfc")},
		{name: "pbkdf2-empty-passphrase", kdf: core.KDFPBKDF2, pw: new(""), opens: 1, ttl: 86400, text: new("empty passphrase")},
		{name: "opens-10-ttl-7d", kdf: core.KDFNone, opens: 10, ttl: 604800, text: new("many opens")},
	}
}

func (f *File) envelope() error {
	for _, c := range envCases() {
		id := detID("env/" + c.name)
		s := det32("env/s/" + c.name)
		pw := ""
		if c.pw != nil {
			pw = *c.pw
		}
		_, content, err := core.DeriveAll(id, s, c.kdf, pw)
		if err != nil {
			return err
		}
		h := core.Header{Version: core.Version, KDF: c.kdf, IV: detIV(c.name)}
		pol := core.Policy{OpensMax: c.opens, TTLSeconds: c.ttl}
		payload := core.Payload{Meta: core.Meta{Text: c.text}}
		var fc *FileContent
		if c.fileName != "" {
			payload.Meta.File = &core.FileMeta{Name: c.fileName, Mime: c.fileMime, Size: int64(c.fileSize)}
			payload.File = counterBytes(c.fileSize)
			fc = &FileContent{Name: c.fileName, Mime: c.fileMime, Size: c.fileSize, Content: "counter"}
			if c.fileHex {
				fc.Content = hex.EncodeToString(payload.File)
			}
		}
		metaJSON, err := core.EncodeMeta(payload.Meta)
		if err != nil {
			return err
		}
		object, err := core.Seal(&content.K, h, id, pol, payload)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(object)
		e := Envelope{
			Name: c.name, ID: id.String(), S: hex.EncodeToString(s[:]), KDFID: int(c.kdf), Passphrase: c.pw, Flags: 0,
			IV: hex.EncodeToString(h.IV[:]), OpensMax: int(c.opens), TTLSeconds: int(c.ttl), Text: c.text, File: fc,
			MetaJSON: string(metaJSON), PlaintextLen: len(object) - core.HeaderLen - core.TagLen,
			AAD: hex.EncodeToString(core.AAD(h, id, pol)), ObjectLen: len(object), ObjectSHA256: hex.EncodeToString(sum[:]),
		}
		if len(object) <= inlineMax {
			e.Object = new(core.Encode(object))
		}
		f.Envelope = append(f.Envelope, e)
	}
	return nil
}

func (f *File) link() error {
	id := detID("link")
	s := det32("link/s")
	base := "https://nylzo.example"
	valid := func(name string, kdf core.KDF) {
		f.Link = append(f.Link, Link{Name: name, Input: core.FormatLink(base, id, kdf, s), Valid: true, Canonical: true, ID: new(id.String()), KDFID: new(int(kdf)), S: new(hex.EncodeToString(s[:]))})
	}
	valid("valid-kdf0", core.KDFNone)
	valid("valid-kdf1", core.KDFScrypt)
	valid("valid-kdf2", core.KDFPBKDF2)
	frag := core.Fragment(core.KDFScrypt, s)
	sEnc := core.Encode(s[:])
	idEnc := id.String()
	invalid := func(name, input string) {
		f.Link = append(f.Link, Link{Name: name, Input: input, Valid: false})
	}
	invalid("fragment-version-2", base+"/m/"+idEnc+"#2.1."+sEnc)
	invalid("fragment-kdf-3", base+"/m/"+idEnc+"#1.3."+sEnc)
	invalid("fragment-missing", base+"/m/"+idEnc)
	invalid("fragment-two-parts", base+"/m/"+idEnc+"#1."+sEnc)
	invalid("secret-42-chars", base+"/m/"+idEnc+"#1.1."+sEnc[:42])
	invalid("secret-44-chars", base+"/m/"+idEnc+"#1.1."+sEnc+"A")
	invalid("secret-padding", base+"/m/"+idEnc+"#1.1."+sEnc+"=")
	invalid("secret-non-canonical-last-char", base+"/m/"+idEnc+"#1.1."+sEnc[:42]+nonCanonicalLast(sEnc[42]))
	invalid("secret-std-base64-alphabet", base+"/m/"+idEnc+"#1.1."+strings.Map(func(r rune) rune {
		switch r {
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, sEnc[:42])+"+")
	invalid("id-21-chars", base+"/m/"+idEnc[:21]+"#"+frag)
	invalid("id-23-chars", base+"/m/"+idEnc+"A#"+frag)
	invalid("id-non-canonical-last-char", base+"/m/"+idEnc[:21]+nonCanonicalLast(idEnc[21])+"#"+frag)
	invalid("id-percent-encoded", base+"/m/%"+fmt.Sprintf("%02X", idEnc[0])+idEnc[1:]+"#"+frag)
	invalid("secret-percent-encoded", base+"/m/"+idEnc+"#1.1.%"+fmt.Sprintf("%02X", sEnc[0])+sEnc[1:])
	invalid("path-not-m", base+"/x/"+idEnc+"#"+frag)
	invalid("path-trailing-slash", base+"/m/"+idEnc+"/#"+frag)
	invalid("relative-reference", "/m/"+idEnc+"#"+frag)
	invalid("scheme-relative", "//nylzo.example/m/"+idEnc+"#"+frag)
	invalid("scheme-ftp", "ftp://nylzo.example/m/"+idEnc+"#"+frag)
	invalid("dot-segment", base+"/x/../m/"+idEnc+"#"+frag)
	invalid("dot-segment-encoded", base+"/x/%2e%2e/m/"+idEnc+"#"+frag)
	invalid("backslash-path", "https://nylzo.example\\m\\"+idEnc+"#"+frag)
	invalid("userinfo", "https://user@nylzo.example/m/"+idEnc+"#"+frag)
	invalid("leading-space", " "+base+"/m/"+idEnc+"#"+frag)
	invalid("trailing-space", base+"/m/"+idEnc+"#"+frag+" ")
	invalid("tab-in-id", base+"/m/"+idEnc[:11]+"\t"+idEnc[11:]+"#"+frag)
	invalid("second-fragment", base+"/m/"+idEnc+"#"+frag+"#x")
	invalid("double-hash", base+"/m/"+idEnc+"##"+frag)
	invalid("scheme-httpss", "httpss://nylzo.example/m/"+idEnc+"#"+frag)
	for _, c := range [][2]string{
		{"uppercase-scheme-and-port", "HTTPS://nylzo.example:8443/m/" + idEnc + "#" + frag},
		{"query-ignored", base + "/m/" + idEnc + "?utm=x#" + frag},
		{"http-localhost-port", "http://localhost:8080/m/" + idEnc + "#" + frag},
		{"ipv6-literal", "http://[::1]:8080/m/" + idEnc + "#" + frag},
	} {
		f.Link = append(f.Link, Link{Name: c[0], Input: c[1], Valid: true, ID: new(idEnc), KDFID: new(int(core.KDFScrypt)), S: new(hex.EncodeToString(s[:]))})
	}
	return nil
}

func nonCanonicalLast(c byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	i := strings.IndexByte(alphabet, c)
	return string(alphabet[i+1])
}

func (f *File) meta() error {
	add := func(name, input string, valid bool) {
		mc := MetaCase{Name: name, Input: input, Valid: valid}
		if valid {
			m, err := core.DecodeMeta([]byte(input))
			if err != nil {
				panic(fmt.Sprintf("vectors: meta %q must decode: %v", name, err))
			}
			can, err := core.EncodeMeta(m)
			if err != nil {
				panic(fmt.Sprintf("vectors: meta %q must encode: %v", name, err))
			}
			mc.Canonical = new(string(can))
			mc.Text = m.Text
			if m.File != nil {
				mc.FileName = new(m.File.Name)
				mc.FileMime = new(m.File.Mime)
				mc.FileSize = new(m.File.Size)
			}
		} else if _, err := core.DecodeMeta([]byte(input)); err == nil {
			panic(fmt.Sprintf("vectors: meta %q must be rejected", name))
		}
		f.Meta = append(f.Meta, mc)
	}
	add("text-only", `{"v":1,"text":"ahoj"}`, true)
	add("file-only", `{"v":1,"file":{"name":"a.txt","mime":"text/plain","size":3}}`, true)
	add("text-and-file", `{"v":1,"text":"t","file":{"name":"a.txt","mime":"text/plain","size":0}}`, true)
	add("escapes-canonical", `{"v":1,"text":"q\"b\\s\b\t\n\f\r\u0001\u001f/é  "}`, true)
	add("whitespace-and-order", "{ \"text\" : \"x\" ,\n \"v\" : 1 }", true)
	add("unknown-key-ignored", `{"v":1,"text":"x","extra":{"nested":[1,2,3]}}`, true)
	add("duplicate-key-last-wins", `{"v":1,"text":"first","text":"second"}`, true)
	add("escaped-slash-and-unicode", `{"v":1,"text":"a\/bAé"}`, true)
	add("mime-missing", `{"v":1,"file":{"name":"a","size":1}}`, true)
	add("file-key-case-variants-ignored", `{"v":1,"file":{"name":"a.txt","NAME":"b.exe","mime":"text/plain","Mime":"x","size":3,"SIZE":9}}`, true)
	add("top-level-case-variant-v", `{"V":1,"text":"x"}`, false)
	add("trailing-brace", `{"v":1,"text":"x"}}`, false)
	add("trailing-bracket", `{"v":1,"text":"x"}]`, false)
	add("trailing-whitespace", `{"v":1,"text":"x"} `+"\n", true)
	add("depth-32", `{"v":1,"text":"x","extra":`+strings.Repeat("[", 31)+strings.Repeat("]", 31)+`}`, true)
	add("depth-33", `{"v":1,"text":"x","extra":`+strings.Repeat("[", 32)+strings.Repeat("]", 32)+`}`, false)
	add("depth-brackets-in-string", `{"v":1,"text":"`+strings.Repeat("[", 40)+`"}`, true)
	add("depth-escaped-quote-then-brackets", `{"v":1,"text":"\"`+strings.Repeat("[", 40)+`"}`, true)
	add("depth-escaped-backslash-then-nesting", `{"v":1,"text":"\\","extra":`+strings.Repeat("[", 32)+strings.Repeat("]", 32)+`}`, false)
	add("name-del", `{"v":1,"file":{"name":"a\u007fb","mime":"","size":1}}`, false)
	add("name-unit-separator", `{"v":1,"file":{"name":"a\u001fb","mime":"","size":1}}`, false)
	add("mime-null", `{"v":1,"file":{"name":"a","mime":null,"size":1}}`, true)
	add("size-negative-zero", `{"v":1,"file":{"name":"a","mime":"","size":-0}}`, true)
	add("duplicate-file-last-wins", `{"v":1,"file":{"name":"first","mime":"x","size":1},"file":{"name":"second","mime":"y","size":2}}`, true)
	add("duplicate-file-incomplete-last", `{"v":1,"file":{"name":"a","size":1},"file":{"mime":"x"}}`, false)
	add("null-text-with-file", `{"v":1,"text":null,"file":{"name":"a","mime":"","size":1}}`, true)
	add("null-text-only", `{"v":1,"text":null}`, false)
	add("null-v", `{"v":null,"text":"x"}`, false)
	add("missing-v", `{"text":"x"}`, false)
	add("v-2", `{"v":2,"text":"x"}`, false)
	add("v-string", `{"v":"1","text":"x"}`, false)
	add("empty-object", `{"v":1}`, false)
	add("not-object", `[1]`, false)
	add("trailing-garbage", `{"v":1,"text":"x"} {}`, false)
	add("name-slash", `{"v":1,"file":{"name":"a/b","mime":"","size":1}}`, false)
	add("name-backslash", `{"v":1,"file":{"name":"a\\b","mime":"","size":1}}`, false)
	add("name-control", `{"v":1,"file":{"name":"a\u0000b","mime":"","size":1}}`, false)
	add("name-empty", `{"v":1,"file":{"name":"","mime":"","size":1}}`, false)
	add("name-256-bytes", `{"v":1,"file":{"name":"`+strings.Repeat("n", 256)+`","mime":"","size":1}}`, false)
	add("size-negative", `{"v":1,"file":{"name":"a","mime":"","size":-1}}`, false)
	add("size-float", `{"v":1,"file":{"name":"a","mime":"","size":1.0}}`, false)
	add("size-string", `{"v":1,"file":{"name":"a","mime":"","size":"1"}}`, false)
	add("size-over-limit", fmt.Sprintf(`{"v":1,"file":{"name":"a","mime":"","size":%d}}`, core.MaxFileBytes+1), false)
	add("text-over-limit", `{"v":1,"text":"`+strings.Repeat("x", core.MaxTextBytes+1)+`"}`, false)
	add("text-at-limit", `{"v":1,"text":"`+strings.Repeat("x", core.MaxTextBytes)+`"}`, true)
	add("mime-256-bytes", `{"v":1,"file":{"name":"a","mime":"`+strings.Repeat("m", 256)+`","size":1}}`, false)
	return nil
}

func (f *File) request() error {
	id := detID("request")
	for _, c := range []struct {
		name, purpose, method, path string
		body                        []byte
	}{
		{"metadata read", core.PurposeRead, "GET", "/api/v1/messages/" + id.String(), nil},
		{"claim verifier", core.PurposeVerifier, "POST", "/api/v1/messages/" + id.String() + "/claim", nil},
		{"status manage", core.PurposeManage, "GET", "/api/v1/messages/" + id.String() + "/status", nil},
		{"abuse report with a body", core.PurposeRead, "POST", "/api/v1/abuse", []byte(`{"id":"` + id.String() + `","reason":"spam"}`)},
	} {
		seed := det32("request-seed/" + c.name)
		challenge := [core.ChallengeLen]byte(det("request-challenge/"+c.name, core.ChallengeLen))
		sig := core.SignRequest(seed, c.purpose, c.method, c.path, challenge, c.body)
		if !sig.Verify(c.purpose, c.method, c.path, c.body) {
			return fmt.Errorf("vectors: request signature %q does not verify", c.name)
		}
		f.Request = append(f.Request, Request{
			Name: c.name, Seed: hex.EncodeToString(seed[:]), PublicKey: core.Encode(sig.PublicKey[:]),
			Purpose: c.purpose, Method: c.method, Path: c.path, Challenge: core.Encode(challenge[:]),
			Body: hex.EncodeToString(c.body), Message: hex.EncodeToString(core.RequestMessage(c.purpose, c.method, c.path, &challenge, c.body)),
			Signature: core.Encode(sig.Sig[:]),
		})
		if c.purpose == core.PurposeVerifier {
			f.Request[len(f.Request)-1].Verifier = sig.VerifierHeader()
		} else {
			f.Request[len(f.Request)-1].Authorization = sig.Authorization()
		}
	}
	return nil
}

func (f *File) strength() error {
	face := "\U0001F600"
	for _, c := range []Strength{
		{"empty", "", false},
		{"eleven ASCII characters", "eleven char", false},
		{"twelve ASCII characters", "twelve chars", true},
		{"eleven characters after NFC, twelve code points before", "cafe\u0301 au lai", false},
		{"twelve characters after NFC", "cafe\u0301 au lait", true},
		{"eleven astral characters", strings.Repeat(face, 11), false},
		{"twelve astral characters", strings.Repeat(face, 12), true},
	} {
		if err := core.CheckPassphraseStrength(c.Passphrase); (err == nil) != c.Accepted {
			return fmt.Errorf("vectors: strength case %q disagrees with the core: %v", c.Name, err)
		}
		f.Strength = append(f.Strength, c)
	}
	return nil
}

func (f *File) server() error {
	cur := det("pepper/current", 32)
	prev := det("pepper/previous", 32)
	ring, err := pepper.NewRing(cur, prev)
	if err != nil {
		return err
	}
	curKey, _ := pepper.New(cur)
	prevKey, _ := pepper.New(prev)
	f.Server = Server{Pepper: hex.EncodeToString(cur), PepperID: int(curKey.ID()), PreviousPepper: hex.EncodeToString(prev), PreviousPepperID: int(prevKey.ID())}
	for _, p := range []pepper.Purpose{pepper.Read, pepper.Verifier, pepper.Manage, pepper.Lease, pepper.PoW, pepper.ID, pepper.Challenge} {
		input := det("mac-input/"+string(p), 32)
		mac, _ := ring.MAC(p, input)
		f.Server.MACs = append(f.Server.MACs, MACCase{Purpose: string(p), Input: hex.EncodeToString(input), MAC: hex.EncodeToString(mac[:])})
	}
	return nil
}
