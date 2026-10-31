package cli

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	maxNameBytes      = 255
	maxExtensionBytes = 16
	fallbackName      = "nylzo"
)

var (
	reservedRunes = regexp.MustCompile(`[/\\:*?"<>|]`)
	windowsDevice = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9¹²³]|lpt[0-9¹²³]|conin\$|conout\$) *(\.|$)`)
)

func invisible(character rune) bool {
	switch character {
	case 0x115F, 0x1160, 0x3164, 0xFFA0:
		return true
	default:
		return unicode.In(character, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
	}
}

// attachmentName makes a received file name safe on every system the same way the web client does.
func attachmentName(name string) string {
	clean := strings.Map(func(character rune) rune {
		if invisible(character) {
			return -1
		}
		return character
	}, norm.NFC.String(strings.ToValidUTF8(name, "")))
	clean = reservedRunes.ReplaceAllString(clean, "_")
	clean = strings.Join(strings.FieldsFunc(clean, unicode.IsSpace), " ")
	dotOrSpace := func(character rune) bool { return character == '.' || character == ' ' }
	clean = strings.TrimRightFunc(strings.TrimLeftFunc(clean, dotOrSpace), dotOrSpace)
	if windowsDevice.MatchString(clean) {
		clean = "_" + clean
	}
	if clean == "" {
		return fallbackName
	}
	if len(clean) <= maxNameBytes {
		return clean
	}
	extension := ""
	if dot := strings.LastIndex(clean, "."); dot > 0 && len(clean[dot+1:]) <= maxExtensionBytes {
		extension = clean[dot:]
	}
	stem := strings.TrimSuffix(clean, extension)
	return strings.ToValidUTF8(stem[:min(maxNameBytes-len(extension), len(stem))], "") + extension
}

func sameFileName(first, second string) bool {
	fold := cases.Fold()
	return fold.String(norm.NFC.String(first)) == fold.String(norm.NFC.String(second))
}
