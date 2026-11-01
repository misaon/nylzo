package local

func systemOpener(target string) (string, []string) {
	return "rundll32", []string{"url.dll,FileProtocolHandler", target}
}
