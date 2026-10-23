package cli

import (
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

func TestLongNamesKeepTheirExtension(t *testing.T) {
	t.Parallel()
	name := attachmentName(strings.Repeat("č", 200) + ".tar.gz")
	if len(name) > maxNameBytes || name[len(name)-3:] != ".gz" {
		t.Fatalf("%d bytes, %q", len(name), name[len(name)-10:])
	}
}
