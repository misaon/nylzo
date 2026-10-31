package cli

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAttachmentNamesMatchTheWebClient(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"report.pdf":                    "report.pdf",
		"../../etc/passwd":              "_.._etc_passwd",
		"C:\\Windows\\evil.exe":         "C__Windows_evil.exe",
		"invoice\u202efdp.exe":          "invoicefdp.exe",
		"bidi\u2066isolate\u2069.txt":   "bidiisolate.txt",
		"zero\u200bwidth.txt":           "zerowidth.txt",
		"soft\u00adhyphen.txt":          "softhyphen.txt",
		"line\u2028separator\u2029.txt": "lineseparator.txt",
		"hangul\u3164filler\u115f.txt":  "hangulfiller.txt",
		"tab\tand\nnewline.txt":         "tabandnewline.txt",
		"  .hidden  ":                   "hidden",
		"trailing dot.":                 "trailing dot",
		"many   spaces.txt":             "many spaces.txt",
		"CON.txt":                       "_CON.txt",
		"con .txt":                      "_con .txt",
		"com¹.txt":                      "_com¹.txt",
		"LPT³":                          "_LPT³",
		"CONIN$":                        "_CONIN$",
		"conout$.log":                   "_conout$.log",
		"console.txt":                   "console.txt",
		"com10.txt":                     "com10.txt",
		"":                              "nylzo",
		"...":                           "nylzo",
		"\u202e":                        "nylzo",
		"cafe\u0301.txt":                "caf\u00e9.txt",
		"a\u00a0\u00a0b.txt":            "a b.txt",
		"file.txt\u00a0.":               "file.txt",
		". \u3000.bashrc":               "bashrc",
		"CON\u00a0.txt":                 "_CON .txt",
		"x\u2003\u2003y":                "x y",
		"nbsp\u00a0end\u00a0":           "nbsp end",
		"\u3000\u3000":                  "nylzo",
		"a\u0085b":                      "ab",
		"m\ufeffid.txt":                 "mid.txt",
		"AUX.tar.gz":                    "_AUX.tar.gz",
		"name\u2028x":                   "namex",
		" .. lead":                      "lead",
	} {
		if got := attachmentName(in); got != want {
			t.Fatalf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestLongNamesMatchTheSharedVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../spec/file-name-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Cases []struct {
			Name      string `json:"name"`
			Input     []part `json:"input"`
			Sanitized []part `json:"sanitized"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(data, &vectors); err != nil || len(vectors.Cases) == 0 {
		t.Fatalf("spec/file-name-vectors.json: %v", err)
	}
	for _, vector := range vectors.Cases {
		if got, want := attachmentName(joinParts(vector.Input)), joinParts(vector.Sanitized); got != want {
			t.Errorf("%s: %d bytes %q, want %d bytes %q", vector.Name, len(got), got, len(want), want)
		}
	}
}

type part struct {
	text  string
	count int
}

func (p *part) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 2 {
		return errors.New("a part is a text and a count")
	}
	return errors.Join(json.Unmarshal(fields[0], &p.text), json.Unmarshal(fields[1], &p.count))
}

func joinParts(parts []part) string {
	var builder strings.Builder
	for _, piece := range parts {
		builder.WriteString(strings.Repeat(piece.text, piece.count))
	}
	return builder.String()
}

func TestNamesCompareWithFullCaseFolding(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"MESSAGE.TXT", "Meßage.txt", "meſſage.txt", "message.txt"} {
		if !sameFileName(name, textFileName) {
			t.Fatalf("%q is not treated as %q", name, textFileName)
		}
	}
	if sameFileName("messages.txt", textFileName) {
		t.Fatal("a different name was treated as the text file")
	}
}
