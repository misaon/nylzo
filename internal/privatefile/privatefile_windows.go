package privatefile

import (
	"cmp"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff

type entry struct {
	access      uint32
	options     uint32
	inheritance string
	aceFlags    uint8
}

var (
	privateFile = entry{
		access:  windows.FILE_GENERIC_WRITE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL,
		options: windows.FILE_NON_DIRECTORY_FILE,
	}
	privateDirectory = entry{
		access:      windows.FILE_LIST_DIRECTORY | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE,
		options:     windows.FILE_DIRECTORY_FILE,
		inheritance: "OICI",
		aceFlags:    windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE,
	}

	errName     = errors.New("privatefile: not a single valid Windows file name")
	errReplaced = errors.New("privatefile: the new directory was replaced before it was opened")
)

func Create(path string) (*os.File, error) {
	directory, name := filepath.Split(path)
	root, err := os.OpenRoot(cmp.Or(directory, "."))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return CreateIn(root, name)
}

func CreateIn(root *os.Root, name string) (*os.File, error) {
	path := filepath.Join(root.Name(), name)
	handle, err := createEntry(root, name, privateFile)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// MkdirTemp returns the new directory open, so files land in it even if its name is later replaced.
func MkdirTemp(directory, prefix string) (*os.Root, error) {
	directory = cmp.Or(directory, os.TempDir())
	base, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	name := prefix + rand.Text()
	handle, err := createEntry(base, name, privateDirectory)
	if err != nil {
		return nil, &fs.PathError{Op: "mkdirtemp", Path: filepath.Join(directory, prefix+"*"), Err: err}
	}
	created := os.NewFile(uintptr(handle), filepath.Join(directory, name))
	root, err := base.OpenRoot(name)
	if err == nil && !sameDirectory(created, root) {
		err = errors.Join(errReplaced, root.Close())
	}
	created.Close()
	if err != nil {
		return nil, &fs.PathError{Op: "mkdirtemp", Path: created.Name(), Err: errors.Join(err, base.Remove(name))}
	}
	return root, nil
}

func sameDirectory(created *os.File, root *os.Root) bool {
	opened, err := root.Open(".")
	if err != nil {
		return false
	}
	defer opened.Close()
	createdInfo, createdErr := created.Stat()
	openedInfo, openedErr := opened.Stat()
	return createdErr == nil && openedErr == nil && os.SameFile(createdInfo, openedInfo)
}

// createEntry hands the descriptor to NtCreateFile, so the entry never exists with the access it would inherit.
func createEntry(root *os.Root, name string, kind entry) (windows.Handle, error) {
	if !validName(name) {
		return windows.InvalidHandle, errName
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return windows.InvalidHandle, err
	}
	sid := user.User.Sid.String()
	// P protects the list from the inheritable entries of the parent.
	descriptor, err := windows.SecurityDescriptorFromString(
		"O:" + sid + "D:P(A;" + kind.inheritance + ";FA;;;" + sid + ")",
	)
	if err != nil {
		return windows.InvalidHandle, err
	}
	directory, err := root.Open(".")
	if err != nil {
		return windows.InvalidHandle, err
	}
	defer directory.Close()
	if !localVolume(windows.Handle(directory.Fd())) {
		return windows.InvalidHandle, ErrNotPrivate
	}
	handle, err := createAt(windows.Handle(directory.Fd()), name, descriptor, kind)
	if err != nil {
		return windows.InvalidHandle, err
	}
	if !ownerOnly(handle, user.User.Sid, kind.aceFlags) {
		return windows.InvalidHandle, errors.Join(ErrNotPrivate, windows.CloseHandle(handle), root.Remove(name))
	}
	return handle, nil
}

// localVolume accepts only a local NTFS or ReFS volume before anything is created: FAT and exFAT store no access list,
// and a share or a user-mode file system can return the list it was given while its server or helper decides access.
func localVolume(directory windows.Handle) bool {
	var flags uint32
	var name [windows.MAX_PATH + 1]uint16
	if windows.GetVolumeInformationByHandle(directory, nil, 0, nil, nil, &flags, &name[0], uint32(len(name))) != nil {
		return false
	}
	fileSystem := windows.UTF16ToString(name[:])
	return flags&windows.FILE_PERSISTENT_ACLS != 0 && (fileSystem == "NTFS" || fileSystem == "ReFS") &&
		!remote(directory)
}

// remote relies on FileRemoteProtocolInfo failing with ERROR_INVALID_PARAMETER for a local handle only.
func remote(handle windows.Handle) bool {
	var information [512]byte
	err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileRemoteProtocolInfo,
		&information[0],
		uint32(len(information)),
	)
	return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

func createAt(
	directory windows.Handle,
	name string,
	descriptor *windows.SECURITY_DESCRIPTOR,
	kind entry,
) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory:      directory,
		ObjectName:         objectName,
		Attributes:         windows.OBJ_CASE_INSENSITIVE,
		SecurityDescriptor: descriptor,
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var handle windows.Handle
	err = windows.NtCreateFile(
		&handle,
		kind.access,
		&attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		windows.FILE_CREATE,
		kind.options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT,
		0,
		0,
	)
	if status, ok := errors.AsType[windows.NTStatus](err); ok {
		return windows.InvalidHandle, status.Errno()
	}
	return handle, err
}

// ownerOnly reads the list back, since some network shares accept a descriptor and store another one.
func ownerOnly(handle windows.Handle, user *windows.SID, aceFlags uint8) bool {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return false
	}
	owner, _, ownerErr := descriptor.Owner()
	control, _, controlErr := descriptor.Control()
	list, _, listErr := descriptor.DACL()
	if errors.Join(ownerErr, controlErr, listErr) != nil || owner == nil || list == nil || list.AceCount != 1 ||
		control&windows.SE_DACL_PROTECTED == 0 || !owner.Equals(user) {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(list, 0, &ace) != nil {
		return false
	}
	return ace.Header.AceType == windows.ACCESS_ALLOWED_ACE_TYPE && ace.Header.AceFlags == aceFlags &&
		ace.Mask == fileAllAccess && (*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(user)
}

// validName refuses what Win32 would rewrite or treat as a device or stream, since NtCreateFile takes names as given.
func validName(name string) bool {
	return filepath.IsLocal(name) &&
		!strings.ContainsFunc(name, func(character rune) bool {
			return character < ' ' || strings.ContainsRune(`\/:*?"<>|`, character)
		}) &&
		!strings.HasSuffix(name, ".") &&
		!strings.HasSuffix(name, " ")
}
