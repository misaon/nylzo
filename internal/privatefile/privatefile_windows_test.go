package privatefile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func userSID(t *testing.T) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid.String()
}

func securityOf(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}

// shareWithEveryone also proves that a file made the usual way there inherits access for everyone (WD).
func shareWithEveryone(t *testing.T, directory string) {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)(A;OICI;FA;;;" + userSID(t) + ")")
	if err != nil {
		t.Fatal(err)
	}
	list, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	err = windows.SetNamedSecurityInfo(
		directory,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		list,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	usual := filepath.Join(directory, "usual")
	if err = os.WriteFile(usual, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := securityOf(t, usual); !strings.Contains(got, ";;;WD)") {
		t.Fatalf("a file made with os.WriteFile does not inherit access for everyone: %s", got)
	}
}

func requirePrivate(t *testing.T, path string, directory bool) {
	t.Helper()
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	sid := userSID(t)
	want, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;" + inheritance + ";FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Replace(securityOf(t, path), "D:PAI(", "D:P(", 1); got != want.String() {
		t.Fatalf("%s: %s, want %s", path, got, want)
	}
}

func TestCreateRefusesNamesThatWin32WouldRewrite(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{
		"", ".", "..", `a\b`, "a/b", "a:stream", "NUL", "COM1", "CONOUT$", "dot.", "space ", "a*b", "a\x01b",
	} {
		if file, createErr := CreateIn(root, name); !errors.Is(createErr, errName) {
			file.Close()
			t.Errorf("CreateIn(%q): %v", name, createErr)
		}
	}
	if _, err = MkdirTemp(directory, `a\`); !errors.Is(err, errName) {
		t.Errorf("MkdirTemp with a separator in the prefix: %v", err)
	}
	if entries, readErr := os.ReadDir(directory); readErr != nil || len(entries) != 0 {
		t.Fatalf("refused names left %v %v", entries, readErr)
	}
}

func TestReadbackRefusesAnInheritedList(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	shareWithEveryone(t, directory)
	name, err := windows.UTF16PtrFromString(filepath.Join(directory, "usual"))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.READ_CONTROL,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if ownerOnly(handle, user.User.Sid, 0) {
		t.Fatal("the readback accepted a list inherited from a directory everyone can use")
	}
}
