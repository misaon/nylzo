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

// Rejected is an object a reader of the link of envelope case Base must refuse with Error. The reader
// receives the object of ObjectFrom (default Base) or Object, with FlipByte flipped and cut to TruncateTo.
type Rejected struct {
	Name       string  `json:"name"`
	Base       string  `json:"base"`
	ObjectFrom string  `json:"object_from,omitempty"`
	Object     *string `json:"object_b64,omitempty"`
	FlipByte   *int    `json:"flip_byte,omitempty"`
	TruncateTo *int    `json:"truncate_to,omitempty"`
	ObjectHash string  `json:"h"`
	E          string  `json:"e_hex"`
	KDFID      int     `json:"kdf_id"`
	Passphrase *string `json:"passphrase,omitempty"`
	Error      string  `json:"error"`
}

const (
	rejectedBase     = "text-ascii"
	errorIntegrity   = "integrity"
	errorPlaintext   = "plaintext"
	errorVersion     = "version"
	errorFlags       = "flags"
	errorKDFMismatch = "kdf_mismatch"
)

type rejection struct {
	vector Rejected
	object []byte
	hash   *[core.ObjectHashLen]byte
	share  *[core.ShareLen]byte
}

func (f *File) rejected() error {
	sealed := map[string]sealedCase{}
	for _, vector := range envelopeCases {
		if vector.name == rejectedBase || vector.name == "text-escapes" {
			sealedVector, err := sealCase(vector)
			if err != nil {
				return err
			}
			sealed[vector.name] = sealedVector
		}
	}
	base := sealed[rejectedBase]
	secondShare := deterministic32("rejected/second-share")
	twoKey, err := twoKeyObject(
		base.id,
		base.secret,
		[2]*[core.ShareLen]byte{base.share, secondShare},
		base.header,
		base.policy,
	)
	if err != nil {
		return err
	}
	prefix := envelope.PrefixLen
	cases := []rejection{
		{
			vector: Rejected{Name: "hash-of-another-object", Error: errorIntegrity},
			hash:   hashOf(sealed["text-escapes"].object),
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
		{vector: Rejected{Name: "version-other-than-2", FlipByte: new(0), Error: errorVersion}},
		{
			vector: Rejected{
				Name:       "kdf-other-than-link",
				KDFID:      int(core.KDFScrypt),
				Passphrase: new("a passphrase the link asks for"),
				Error:      errorKDFMismatch,
			},
		},
		{vector: Rejected{Name: "two-key-object-first-share", Error: errorPlaintext}, object: twoKey},
		{
			vector: Rejected{Name: "two-key-object-second-share", Error: errorIntegrity},
			object: twoKey,
			share:  secondShare,
		},
	}
	for index := range cases {
		vector, checkErr := rejectedVector(sealed, &cases[index])
		if checkErr != nil {
			return checkErr
		}
		f.Rejected = append(f.Rejected, vector)
	}
	return nil
}

func rejectedVector(sealed map[string]sealedCase, rejected *rejection) (Rejected, error) {
	base := sealed[rejectedBase]
	vector := rejected.vector
	vector.Base = rejectedBase
	object := rejected.object
	switch {
	case object != nil:
		vector.Object = new(core.Encode(object))
	case vector.ObjectFrom != "":
		object = sealed[vector.ObjectFrom].object
	default:
		object = base.object
	}
	object = bytes.Clone(object)
	if vector.FlipByte != nil {
		object[*vector.FlipByte] ^= 1
	}
	if vector.TruncateTo != nil {
		object = object[:*vector.TruncateTo]
	}
	hash := rejected.hash
	if hash == nil {
		hash = hashOf(object)
	}
	share := rejected.share
	if share == nil {
		share = base.share
	}
	if vector.KDFID < 0 || vector.KDFID > math.MaxUint8 {
		return Rejected{}, fmt.Errorf("vectors: kdf id %d out of range", vector.KDFID)
	}
	kdf := core.KDF(vector.KDFID)
	derived, err := keys.DeriveAll(base.id, base.secret, kdf, valueOr(vector.Passphrase), share)
	if err != nil {
		return Rejected{}, err
	}
	expected := envelope.Expected{ID: base.id, Policy: base.policy, KDF: kdf, ObjectHash: hash}
	_, _, openErr := envelope.Open(&derived.Content, &expected, object)
	if got := errorName(openErr); got != vector.Error {
		return Rejected{}, fmt.Errorf(
			"vectors: rejected case %q fails with %q, want %q",
			vector.Name,
			got,
			vector.Error,
		)
	}
	vector.ObjectHash = core.Encode(hash[:])
	vector.E = hex.EncodeToString(share[:])
	return vector, nil
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
