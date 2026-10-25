package abuse

import (
	"bytes"
	"crypto/hpke"
	"encoding/json"
	"errors"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

const (
	info      = "nylzo/v1/abuse-report"
	sealedLen = 2048
)

var (
	ErrReportKey = errors.New("abuse: the report key is not a MLKEM768-X25519 key in base64url")
	errTooLong   = errors.New("abuse: the report does not fit the sealed length")
)

type Report struct {
	ID        string
	Reason    string
	Contact   string
	GoodFaith bool
}

// Stored is a report as the operator reads it after opening it with the private key.
type Stored struct {
	Date      string `json:"date"`
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	GoodFaith bool   `json:"good_faith"`
	Contact   string `json:"contact,omitempty"`
}

func kem() hpke.KEM { return hpke.MLKEM768X25519() }

// Seal pads the report to one length, so a sealed report shows nothing about its reason or contact.
func Seal(key hpke.PublicKey, now time.Time, report *Report) ([]byte, error) {
	plain, err := json.Marshal(Stored{
		Date:      reportDate(now),
		ID:        report.ID,
		Reason:    core.RedactReason(report.Reason),
		GoodFaith: report.GoodFaith,
		Contact:   report.Contact,
	})
	if err != nil {
		return nil, err
	}
	if len(plain) > sealedLen {
		return nil, errTooLong
	}
	padded := bytes.Repeat([]byte{' '}, sealedLen)
	copy(padded, plain)
	sealed, err := hpke.Seal(key, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(info), padded)
	if err != nil {
		return nil, err
	}
	return []byte(core.Encode(sealed)), nil
}

func Open(key hpke.PrivateKey, line string) (Stored, error) {
	sealed, err := core.DecodeExact(line, len(line)*3/4)
	if err != nil {
		return Stored{}, err
	}
	plain, err := hpke.Open(key, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(info), sealed)
	if err != nil {
		return Stored{}, err
	}
	var stored Stored
	err = json.Unmarshal(bytes.TrimRight(plain, " "), &stored)
	return stored, err
}

func NewKeyPair() (hpke.PrivateKey, error) {
	return kem().GenerateKey()
}

func ParsePublicKey(text string) (hpke.PublicKey, error) {
	raw, err := core.DecodeExact(text, len(text)*3/4)
	if err != nil {
		return nil, ErrReportKey
	}
	key, err := kem().NewPublicKey(raw)
	if err != nil {
		return nil, ErrReportKey
	}
	return key, nil
}

func ParsePrivateKey(text string) (hpke.PrivateKey, error) {
	raw, err := core.DecodeExact(text, len(text)*3/4)
	if err != nil {
		return nil, ErrReportKey
	}
	key, err := kem().NewPrivateKey(raw)
	if err != nil {
		return nil, ErrReportKey
	}
	return key, nil
}
