package vectors

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

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

var (
	nested32 = strings.Repeat("[", 31) + strings.Repeat("]", 31)
	nested33 = strings.Repeat("[", 32) + strings.Repeat("]", 32)
)

var metaCases = []MetaCase{
	{Name: "text-only", Input: `{"v":1,"text":"ahoj"}`, Valid: true},
	{Name: "file-only", Input: `{"v":1,"file":{"name":"a.txt","mime":"text/plain","size":3}}`, Valid: true},
	{
		Name:  "text-and-file",
		Input: `{"v":1,"text":"t","file":{"name":"a.txt","mime":"text/plain","size":0}}`,
		Valid: true,
	},
	{
		Name:  "escapes-canonical",
		Input: `{"v":1,"text":"q\"b\\s\b\t\n\f\r\u0001\u001f/é` + "\u2028\u2029" + `"}`,
		Valid: true,
	},
	{Name: "whitespace-and-order", Input: "{ \"text\" : \"x\" ,\n \"v\" : 1 }", Valid: true},
	{Name: "unknown-key-ignored", Input: `{"v":1,"text":"x","extra":{"nested":[1,2,3]}}`, Valid: true},
	{Name: "duplicate-key-last-wins", Input: `{"v":1,"text":"first","text":"second"}`, Valid: true},
	{Name: "escaped-slash-and-unicode", Input: `{"v":1,"text":"a\/bAé"}`, Valid: true},
	{Name: "mime-missing", Input: `{"v":1,"file":{"name":"a","size":1}}`, Valid: true},
	{
		Name:  "file-key-case-variants-ignored",
		Input: `{"v":1,"file":{"name":"a.txt","NAME":"b.exe","mime":"text/plain","Mime":"x","size":3,"SIZE":9}}`,
		Valid: true,
	},
	{Name: "top-level-case-variant-v", Input: `{"V":1,"text":"x"}`},
	{Name: "trailing-brace", Input: `{"v":1,"text":"x"}}`},
	{Name: "trailing-bracket", Input: `{"v":1,"text":"x"}]`},
	{Name: "trailing-whitespace", Input: `{"v":1,"text":"x"} ` + "\n", Valid: true},
	{Name: "depth-32", Input: `{"v":1,"text":"x","extra":` + nested32 + `}`, Valid: true},
	{Name: "depth-33", Input: `{"v":1,"text":"x","extra":` + nested33 + `}`},
	{Name: "depth-brackets-in-string", Input: `{"v":1,"text":"` + strings.Repeat("[", 40) + `"}`, Valid: true},
	{
		Name:  "depth-escaped-quote-then-brackets",
		Input: `{"v":1,"text":"\"` + strings.Repeat("[", 40) + `"}`,
		Valid: true,
	},
	{Name: "depth-escaped-backslash-then-nesting", Input: `{"v":1,"text":"\\","extra":` + nested33 + `}`},
	{Name: "name-del", Input: `{"v":1,"file":{"name":"a\u007fb","mime":"","size":1}}`},
	{Name: "name-unit-separator", Input: `{"v":1,"file":{"name":"a\u001fb","mime":"","size":1}}`},
	{Name: "mime-null", Input: `{"v":1,"file":{"name":"a","mime":null,"size":1}}`, Valid: true},
	{Name: "size-negative-zero", Input: `{"v":1,"file":{"name":"a","mime":"","size":-0}}`, Valid: true},
	{
		Name:  "duplicate-file-last-wins",
		Input: `{"v":1,"file":{"name":"first","mime":"x","size":1},"file":{"name":"second","mime":"y","size":2}}`,
		Valid: true,
	},
	{Name: "duplicate-file-incomplete-last", Input: `{"v":1,"file":{"name":"a","size":1},"file":{"mime":"x"}}`},
	{Name: "null-text-with-file", Input: `{"v":1,"text":null,"file":{"name":"a","mime":"","size":1}}`, Valid: true},
	{Name: "null-text-only", Input: `{"v":1,"text":null}`},
	{Name: "null-v", Input: `{"v":null,"text":"x"}`},
	{Name: "missing-v", Input: `{"text":"x"}`},
	{Name: "v-2", Input: `{"v":2,"text":"x"}`},
	{Name: "v-string", Input: `{"v":"1","text":"x"}`},
	{Name: "empty-object", Input: `{"v":1}`},
	{Name: "not-object", Input: `[1]`},
	{Name: "trailing-garbage", Input: `{"v":1,"text":"x"} {}`},
	{Name: "name-slash", Input: `{"v":1,"file":{"name":"a/b","mime":"","size":1}}`},
	{Name: "name-backslash", Input: `{"v":1,"file":{"name":"a\\b","mime":"","size":1}}`},
	{Name: "name-control", Input: `{"v":1,"file":{"name":"a\u0000b","mime":"","size":1}}`},
	{Name: "name-empty", Input: `{"v":1,"file":{"name":"","mime":"","size":1}}`},
	{Name: "name-256-bytes", Input: `{"v":1,"file":{"name":"` + strings.Repeat("n", 256) + `","mime":"","size":1}}`},
	{Name: "size-negative", Input: `{"v":1,"file":{"name":"a","mime":"","size":-1}}`},
	{Name: "size-float", Input: `{"v":1,"file":{"name":"a","mime":"","size":1.0}}`},
	{Name: "size-string", Input: `{"v":1,"file":{"name":"a","mime":"","size":"1"}}`},
	{
		Name:  "size-over-limit",
		Input: `{"v":1,"file":{"name":"a","mime":"","size":` + strconv.Itoa(core.MaxFileBytes+1) + `}}`,
	},
	{Name: "text-over-limit", Input: `{"v":1,"text":"` + strings.Repeat("x", core.MaxTextBytes+1) + `"}`},
	{Name: "text-at-limit", Input: `{"v":1,"text":"` + strings.Repeat("x", core.MaxTextBytes) + `"}`, Valid: true},
	{Name: "mime-256-bytes", Input: `{"v":1,"file":{"name":"a","mime":"` + strings.Repeat("m", 256) + `","size":1}}`},
}

func (f *File) meta() error {
	for _, vector := range metaCases {
		meta, err := core.DecodeMeta([]byte(vector.Input))
		if !vector.Valid {
			if err == nil {
				return fmt.Errorf("vectors: meta %q must be rejected", vector.Name)
			}
			f.Meta = append(f.Meta, vector)
			continue
		}
		if err != nil {
			return fmt.Errorf("vectors: meta %q must decode: %w", vector.Name, err)
		}
		canonical, err := core.EncodeMeta(meta)
		if err != nil {
			return fmt.Errorf("vectors: meta %q must encode: %w", vector.Name, err)
		}
		vector.Canonical = new(string(canonical))
		vector.Text = meta.Text
		if meta.File != nil {
			vector.FileName = new(meta.File.Name)
			vector.FileMime = new(meta.File.Mime)
			vector.FileSize = new(meta.File.Size)
		}
		f.Meta = append(f.Meta, vector)
	}
	return nil
}
