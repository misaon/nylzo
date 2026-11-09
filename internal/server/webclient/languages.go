package webclient

import (
	"slices"
	"strings"
)

const defaultLanguage = "en"

type language struct {
	tag     string
	preview previewText
}

// languages lists the languages of the web client in web/app/src/shared/language/language.ts.
//
//nolint:gosmopolitan // Link previews speak the reader's language, Japanese and Chinese included.
var languages = []language{
	{
		"en",
		previewText{
			"One-time message · Nylzo",
			"Open the link in your browser. The message appears only after a click.",
		},
	},
	{"cs", previewText{"Jednorázová zpráva · Nylzo", "Otevřete odkaz v prohlížeči, zpráva se zobrazí až po kliknutí."}},
	{"de", previewText{
		"Einmalige Nachricht · Nylzo",
		"Öffnen Sie den Link im Browser. Die Nachricht erscheint erst nach einem Klick.",
	}},
	{"es", previewText{
		"Mensaje de un solo uso · Nylzo",
		"Abre el enlace en tu navegador. El mensaje aparece solo después de hacer clic.",
	}},
	{"fr", previewText{
		"Message à usage unique · Nylzo",
		"Ouvrez le lien dans votre navigateur. Le message n’apparaît qu’après un clic.",
	}},
	{"id", previewText{
		"Pesan sekali buka · Nylzo",
		"Buka tautan di browser Anda. Pesan baru muncul setelah Anda mengeklik.",
	}},
	{
		"it",
		previewText{"Messaggio monouso · Nylzo", "Apri il link nel browser. Il messaggio compare solo dopo un clic."},
	},
	{"ja", previewText{
		"一度だけ開けるメッセージ · Nylzo",
		"ブラウザーでリンクを開いてください。メッセージはクリックした後に表示されます。",
	}},
	{"pl", previewText{
		"Jednorazowa wiadomość · Nylzo",
		"Otwórz link w przeglądarce. Wiadomość pojawi się dopiero po kliknięciu.",
	}},
	{"pt-BR", previewText{
		"Mensagem de uso único · Nylzo",
		"Abra o link no navegador. A mensagem só aparece depois de um clique.",
	}},
	{"sk", previewText{"Jednorazová správa · Nylzo", "Otvorte odkaz v prehliadači, správa sa zobrazí až po kliknutí."}},
	{"zh-Hans", previewText{"一次性消息 · Nylzo", "请在浏览器中打开链接。点击后才会显示消息。"}},
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
