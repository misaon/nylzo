package local

func systemOpener(target string) []string {
	return []string{"rundll32", "url.dll,FileProtocolHandler", target}
}
