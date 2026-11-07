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

// Together with Cf these two properties make up Unicode's Default_Ignorable_Code_Point, which Go does not list itself.
func invisible(character rune) bool {
	return unicode.In(
		character,
		unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp,
		unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector,
	)
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
	stem, extension := clean, ""
	if before, after, found := strings.CutLast(clean, "."); found && before != "" && len(after) <= maxExtensionBytes {
		stem, extension = before, "."+after
	}
	return strings.ToValidUTF8(stem[:min(maxNameBytes-len(extension), len(stem))], "") + extension
}

func sameFileName(first, second string) bool {
	fold := cases.Fold()
	return fold.String(norm.NFC.String(first)) == fold.String(norm.NFC.String(second))
}
