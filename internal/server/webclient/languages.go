package webclient

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const defaultLanguage = "en"

type language struct {
	tag     string
	preview previewText
}

// languagesTable is the language table the web client in web/app/src/shared/language/language.ts reads too.
//
//go:embed languages.json
var languagesTable []byte

var languages = mustParseLanguages(languagesTable)

var languageTag = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})*$`)

func mustParseLanguages(data []byte) []language {
	var table struct {
		Languages []struct {
			Tag string `json:"tag"`
		} `json:"languages"`
		Previews map[string]struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"previews"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		panic(fmt.Sprintf("webclient: languages.json: %v", err))
	}
	parsed := make([]language, 0, len(table.Languages))
	for _, entry := range table.Languages {
		if !languageTag.MatchString(entry.Tag) || slices.ContainsFunc(parsed, func(known language) bool {
			return strings.EqualFold(known.tag, entry.Tag)
		}) {
			panic("webclient: languages.json has an invalid or repeated tag " + entry.Tag)
		}
		preview, ok := table.Previews[entry.Tag]
		if !ok || preview.Title == "" || preview.Description == "" {
			panic("webclient: languages.json has no link preview for " + entry.Tag)
		}
		parsed = append(parsed, language{entry.Tag, previewText{preview.Title, preview.Description}})
	}
	if !slices.ContainsFunc(parsed, func(known language) bool { return known.tag == defaultLanguage }) {
		panic("webclient: languages.json has no " + defaultLanguage)
	}
	return parsed
}

var traditionalChinese = []string{"hant", "hk", "mo", "tw"}

func homePath(tag string) string {
	if tag == defaultLanguage {
		return "/"
	}
	return "/" + strings.ToLower(tag) + "/"
}

func homeTemplate(tag string) string {
	return "home." + tag + ".html"
}

func homePaths() []string {
	paths := make([]string, 0, len(languages))
	for _, language := range languages {
		paths = append(paths, homePath(language.tag))
	}
	return paths
}

// matchLanguage follows pickLanguage in web/app/src/shared/language/language.ts.
func matchLanguage(tag string) (string, bool) {
	subtags := strings.Split(strings.ToLower(tag), "-")
	base := subtags[0]
	if base == "zh" && slices.ContainsFunc(subtags[1:], isTraditionalChinese) {
		return "", false
	}
	if base == "in" {
		base = "id"
	}
	for _, language := range languages {
		if languageBase, _, _ := strings.Cut(strings.ToLower(language.tag), "-"); languageBase == base {
			return language.tag, true
		}
	}
	return "", false
}

func isTraditionalChinese(subtag string) bool {
	return slices.Contains(traditionalChinese, subtag)
}
