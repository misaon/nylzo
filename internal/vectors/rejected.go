package vectors

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/keys"
)

var (
	errKDFIDOutOfRange  = errors.New("out of range")
	errRejectionDiffers = errors.New("vectors: rejected case")
)

// Rejected is an object a reader of the link of envelope case Base must refuse with Error. The reader
// receives the object of Base or Object, with FlipByte flipped, SetByte set and cut to
// TruncateTo. ReaderID, OpensMax, TTLSeconds and Deadline replace what the reader expects: a link to another id, or
// a policy that the server reports differently, where a Deadline of 0 is a policy without one.
type Rejected struct {
	Name       string   `json:"name"`
	Base       string   `json:"base"`
	Object     *string  `json:"object_b64,omitempty"`
	FlipByte   *int     `json:"flip_byte,omitempty"`
	SetByte    *SetByte `json:"set_byte,omitempty"`
	TruncateTo *int     `json:"truncate_to,omitempty"`
	ReaderID   *string  `json:"reader_id,omitempty"`
	OpensMax   *int     `json:"opens_max,omitempty"`
	TTLSeconds *int     `json:"ttl_s,omitempty"`
	Deadline   *int     `json:"deadline,omitempty"`
	ObjectHash string   `json:"h"`
	E          string   `json:"e_hex"`
	KDFID      int      `json:"kdf_id"`
	P          *string  `json:"p_hex,omitempty"`
	Error      string   `json:"error"`
}

type SetByte struct {
	Offset int  `json:"offset"`
	Value  byte `json:"value"`
}

const (
	rejectedBase     = "text-ascii"
	scryptBase       = "scrypt-text"
	errorIntegrity   = "integrity"
	errorPlaintext   = "plaintext"
	errorVersion     = "version"
	errorFlags       = "flags"
	errorKDFMismatch = "kdf_mismatch"
	errorKDF         = "kdf"
	errorHeader      = "header"
)

type rejection struct {
	vector    Rejected
	object    []byte
	hash      *[core.ObjectHashLen]byte
	share     *[core.ShareLen]byte
	exportKey *keys.ExportKey
}

func (f *File) rejected() {
	sealed := map[string]sealedCase{}
	for _, vector := range envelopeCases {
		if vector.name == rejectedBase || vector.name == "text-escapes" || vector.name == scryptBase {
			sealed[vector.name] = sealCase(vector)
		}
	}
	base := sealed[rejectedBase]
	secondShare := deterministic32("rejected/second-share")
	twoKey := twoKeyObject(
		base.id,
		base.secret,
		base.exportKey,
		[2]*[core.ShareLen]byte{base.share, secondShare},
		base.header,
		base.policy,
	)
	cases := rejectedCases(&base, twoKey, secondShare, sealed["text-escapes"].object)
	cases = append(cases, deadlineRejections(&base)...)
	for index := range cases {
		f.Rejected = append(f.Rejected, rejectedVector(sealed, &cases[index]))
	}
}

func rejectedCases(
	base *sealedCase,
	twoKey []byte,
	secondShare *[core.ShareLen]byte,
	otherObject []byte,
) []rejection {
	prefix := envelope.PrefixLen
	return []rejection{
		{
			vector: Rejected{Name: "hash-of-another-object", Error: errorIntegrity},
			hash:   hashOf(otherObject),
		},
		{vector: Rejected{Name: "wrong-share", Error: errorIntegrity}, share: secondShare},
		{vector: Rejected{Name: "flipped-commitment", FlipByte: new(envelope.HeaderLen), Error: errorIntegrity}},
		{vector: Rejected{Name: "flipped-ciphertext", FlipByte: new(prefix + 10), Error: errorIntegrity}},
		{vector: Rejected{Name: "flipped-tag", FlipByte: new(len(base.object) - 1), Error: errorIntegrity}},
		{
			vector: Rejected{
				Name:       "length-not-a-multiple-of-64-kib",
				TruncateTo: new(len(base.object) - envelope.TagLen),
				Error:      errorIntegrity,
			},
		},
		{vector: Rejected{Name: "flags-set", FlipByte: new(2), Error: errorFlags}},
		{vector: Rejected{Name: "version-other-than-3", FlipByte: new(0), Error: errorVersion}},
		{vector: Rejected{Name: "kdf-other-than-link", KDFID: int(core.KDFScrypt), Error: errorKDFMismatch}},
		{
			vector: Rejected{
				Name:     "wrong-id",
				ReaderID: new(deterministicID("rejected/other-id").String()),
				Error:    errorIntegrity,
			},
		},
		{
			vector: Rejected{
				Name:     "changed-opens-max",
				OpensMax: new(int(base.policy.OpensMax) + 1),
				Error:    errorIntegrity,
			},
		},
		{
			vector: Rejected{
				Name:       "changed-ttl",
				TTLSeconds: new(int(base.policy.TTLSeconds) - 60),
				Error:      errorIntegrity,
			},
		},
		{
			vector: Rejected{
				Name:  "wrong-export-key",
				Base:  scryptBase,
				KDFID: int(core.KDFScrypt),
				Error: errorIntegrity,
			},
			exportKey: deterministicExportKey("rejected/wrong-export-key"),
		},
		{vector: Rejected{Name: "envelope-kdf-1", SetByte: &SetByte{Offset: 1, Value: 1}, Error: errorKDF}},
		{vector: Rejected{Name: "envelope-kdf-2", SetByte: &SetByte{Offset: 1, Value: 2}, Error: errorKDF}},
		{vector: Rejected{Name: "envelope-kdf-4", SetByte: &SetByte{Offset: 1, Value: 4}, Error: errorKDF}},
		{
			vector: Rejected{
				Name:       "shorter-than-the-header",
				TruncateTo: new(envelope.HeaderLen - 1),
				Error:      errorHeader,
			},
		},
		{vector: Rejected{Name: "two-key-object-first-share", Error: errorPlaintext}, object: twoKey},
		{
			vector: Rejected{Name: "two-key-object-second-share", Error: errorIntegrity},
			object: twoKey,
			share:  secondShare,
		},
	}
}

// deadlineRejections change the deadline a reader expects or the flag that seals it in.
func deadlineRejections(base *sealedCase) []rejection {
	return []rejection{
		{
			vector: Rejected{
				Name:     "changed-deadline",
				Deadline: new(int(base.policy.Deadline) + 60),
				Error:    errorIntegrity,
			},
		},
		{vector: Rejected{Name: "deadline-missing", Deadline: new(0), Error: errorIntegrity}},
		{
			vector: Rejected{
				Name:    "deadline-flag-cleared",
				SetByte: &SetByte{Offset: 2, Value: 0},
				Error:   errorFlags,
			},
		},
	}
}

func rejectedVector(sealed map[string]sealedCase, rejected *rejection) Rejected {
	vector := rejected.vector
	if vector.Base == "" {
		vector.Base = rejectedBase
	}
	base := sealed[vector.Base]
	object := rejected.object
	if object != nil {
		vector.Object = new(core.Encode(object))
	} else {
		object = base.object
	}
	object = mutated(object, &vector)
	hash := rejected.hash
	if hash == nil {
		hash = hashOf(object)
	}
	share := rejected.share
	if share == nil {
		share = base.share
	}
	require(
		vector.KDFID >= 0 && vector.KDFID <= math.MaxUint8,
		fmt.Errorf("vectors: kdf id %d %w", vector.KDFID, errKDFIDOutOfRange),
	)
	kdf := core.KDF(min(max(vector.KDFID, 0), math.MaxUint8))
	id, policy := readerOf(&base, &vector)
	exportKey := rejected.exportKey
	if exportKey == nil {
		exportKey = base.exportKey
	} else {
		vector.P = new(hex.EncodeToString(exportKey[:]))
	}
	content := must(keys.DeriveContent(id, base.secret, exportKey, share))
	expected := envelope.Expected{ID: id, Policy: policy, KDF: kdf, ObjectHash: hash}
	_, _, openErr := envelope.Open(&content, &expected, object)
	got := errorName(openErr)
	require(
		got == vector.Error,
		fmt.Errorf("%w %q fails with %q, want %q", errRejectionDiffers, vector.Name, got, vector.Error),
	)
	vector.ObjectHash = core.Encode(hash[:])
	vector.E = hex.EncodeToString(share[:])
	return vector
}

func mutated(object []byte, vector *Rejected) []byte {
	object = bytes.Clone(object)
	if vector.FlipByte != nil {
		object[*vector.FlipByte] ^= 1
	}
	if vector.SetByte != nil {
		object[vector.SetByte.Offset] = vector.SetByte.Value
	}
	if vector.TruncateTo != nil {
		object = object[:*vector.TruncateTo]
	}
	return object
}

// readerOf is the id and the policy the reader expects: those of the base case unless the vector replaces them.
func readerOf(base *sealedCase, vector *Rejected) (core.ID, envelope.Policy) {
	id, policy := base.id, base.policy
	if vector.ReaderID != nil {
		id = must(core.ParseID(*vector.ReaderID))
	}
	if vector.OpensMax != nil {
		policy.OpensMax = uint16(*vector.OpensMax) //nolint:gosec // G115: a generated case near the base policy
	}
	if vector.TTLSeconds != nil {
		policy.TTLSeconds = uint32(*vector.TTLSeconds) //nolint:gosec // G115: a generated case near the base policy
	}
	if vector.Deadline != nil {
		policy.Deadline = uint32(*vector.Deadline) //nolint:gosec // G115: a generated case near the base policy
	}
	return id, policy
}

func errorName(err error) string {
	switch {
	case errors.Is(err, envelope.ErrIntegrity):
		return errorIntegrity
	case errors.Is(err, envelope.ErrPlaintext), errors.Is(err, envelope.ErrMeta):
		return errorPlaintext
	case errors.Is(err, envelope.ErrVersion):
		return errorVersion
	case errors.Is(err, envelope.ErrFlags):
		return errorFlags
	case errors.Is(err, core.ErrKDFMismatch):
		return errorKDFMismatch
	case errors.Is(err, core.ErrKDF):
		return errorKDF
	case errors.Is(err, envelope.ErrHeader):
		return errorHeader
	case err == nil:
		return "accepted"
	default:
		return err.Error()
	}
}

func hashOf(object []byte) *[core.ObjectHashLen]byte {
	hash := envelope.ObjectHash(object)
	return &hash
}
