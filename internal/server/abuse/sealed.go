package abuse

import (
	"bytes"
	"crypto/hpke"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/report"
)

const (
	info         = "nylzo/v1/abuse-report"
	plaintextLen = 1808
	encLen       = 1120
	tagLen       = 16
	SealedLen    = encLen + plaintextLen + tagLen
)

var (
	ErrReportKey  = errors.New("abuse: the report key is not a MLKEM768-X25519 key in base64url")
	ErrSealed     = errors.New("abuse: the sealed reports are not whole reports")
	ErrUnreadable = errors.New("abuse: a sealed report does not open with this key")
	errTooLong    = errors.New("abuse: the report does not fit the sealed length")
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
func Seal(key hpke.PublicKey, now time.Time, submitted *Report) ([]byte, error) {
	return sealStored(key, &Stored{
		Date:      reportDate(now),
		ID:        submitted.ID,
		Reason:    report.RedactReason(submitted.Reason),
		GoodFaith: submitted.GoodFaith,
		Contact:   submitted.Contact,
	})
}

func sealStored(key hpke.PublicKey, stored *Stored) ([]byte, error) {
	var plain bytes.Buffer
	encoder := json.NewEncoder(&plain)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(stored); err != nil {
		return nil, err
	}
	if plain.Len() > plaintextLen {
		return nil, errTooLong
	}
	padded := bytes.Repeat([]byte{' '}, plaintextLen)
	copy(padded, plain.Bytes())
	sealed, err := hpke.Seal(key, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(info), padded)
	if err != nil {
		return nil, err
	}
	if len(sealed) != SealedLen {
		return nil, errTooLong
	}
	return sealed, nil
}

// DamagedError names the reports that did not open and a partial report at the end, after Open read all others.
type DamagedError struct {
	Unreadable []int
	TailBytes  int
}

func (e *DamagedError) Error() string {
	text := "abuse: "
	if len(e.Unreadable) > 0 {
		positions := make([]string, len(e.Unreadable))
		for i, position := range e.Unreadable {
			positions[i] = strconv.Itoa(position)
		}
		text += fmt.Sprintf(
			"the sealed reports at positions %s do not open with this key",
			strings.Join(positions, ", "),
		)
	}
	if e.TailBytes > 0 {
		if len(e.Unreadable) > 0 {
			text += "; "
		}
		text += fmt.Sprintf("the last %d bytes are a partial report", e.TailBytes)
	}
	return text
}

func (e *DamagedError) Unwrap() error {
	if e.TailBytes > 0 {
		return ErrSealed
	}
	return ErrUnreadable
}

// Open reads reports that lie one after another, as the server appends them, and skips any that do not open.
func Open(key hpke.PrivateKey, sealed []byte) ([]Stored, error) {
	damaged := &DamagedError{TailBytes: len(sealed) % SealedLen}
	whole := sealed[:len(sealed)-damaged.TailBytes]
	reports := make([]Stored, 0, len(whole)/SealedLen)
	position := 0
	for chunk := range slices.Chunk(whole, SealedLen) {
		position++
		stored, err := openOne(key, chunk)
		if err != nil {
			damaged.Unreadable = append(damaged.Unreadable, position)
			continue
		}
		reports = append(reports, stored)
	}
	if len(damaged.Unreadable) > 0 || damaged.TailBytes > 0 {
		return reports, damaged
	}
	return reports, nil
}

func openOne(key hpke.PrivateKey, chunk []byte) (Stored, error) {
	plain, err := hpke.Open(key, hpke.HKDFSHA256(), hpke.AES256GCM(), []byte(info), chunk)
	if err != nil {
		return Stored{}, err
	}
	var stored Stored
	err = json.Unmarshal(bytes.TrimRight(plain, " \n"), &stored)
	return stored, err
}

func NewKeyPair() (hpke.PrivateKey, error) {
	return kem().GenerateKey()
}

func ParsePublicKey(text string) (hpke.PublicKey, error) {
	return parseKey(text, kem().NewPublicKey)
}

func ParsePrivateKey(text string) (hpke.PrivateKey, error) {
	return parseKey(text, kem().NewPrivateKey)
}

//nolint:ireturn // Key is hpke.PublicKey or hpke.PrivateKey, the interfaces the exported parsers return.
func parseKey[Key any](text string, newKey func(raw []byte) (Key, error)) (Key, error) {
	var none Key
	raw, err := core.DecodeExact(text, len(text)*3/4)
	if err != nil {
		return none, ErrReportKey
	}
	key, err := newKey(raw)
	if err != nil {
		return none, ErrReportKey
	}
	return key, nil
}
