package debugenv

import "testing"

func TestHeaderDump(t *testing.T) {
	for _, test := range []struct {
		variables map[string]string
		http2     string
		any       string
	}{
		{nil, "", ""},
		{map[string]string{"GODEBUG": "madvdontneed=1,http2debug=0"}, "", ""},
		{map[string]string{"GODEBUG": "http2debug=1"}, "GODEBUG=http2debug=1", "GODEBUG=http2debug=1"},
		{map[string]string{"GODEBUG": "x509sha1=1,http2debug=2"}, "GODEBUG=http2debug=2", "GODEBUG=http2debug=2"},
		{map[string]string{"GODEBUG": "http2debug=10"}, "GODEBUG=http2debug=1", "GODEBUG=http2debug=1"},
		{map[string]string{"QLOGDIR": "/tmp/qlog"}, "", "QLOGDIR"},
	} {
		getenv := func(name string) string { return test.variables[name] }
		if got := HTTP2HeaderDump(getenv); got != test.http2 {
			t.Errorf("HTTP2HeaderDump %v: got %q, want %q", test.variables, got, test.http2)
		}
		if got := HeaderDump(getenv); got != test.any {
			t.Errorf("HeaderDump %v: got %q, want %q", test.variables, got, test.any)
		}
	}
}
