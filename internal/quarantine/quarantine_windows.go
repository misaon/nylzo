package quarantine

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	streamName = ":Zone.Identifier"
	// about:internet is the source Attachment Services records for the Internet zone when it keeps no address.
	zoneIdentifier = "[ZoneTransfer]\r\nZoneId=3\r\nHostUrl=about:internet\r\n"
)

// Mark opens the stream relative to the file's own handle, so it lands on this file whatever its name leads to now.
func Mark(file *os.File) error {
	stream, err := createStream(windows.Handle(file.Fd()))
	if err != nil {
		return err
	}
	marked := os.NewFile(uintptr(stream), file.Name()+streamName)
	_, err = marked.WriteString(zoneIdentifier)
	return errors.Join(err, marked.Sync(), marked.Close())
}

func createStream(file windows.Handle) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(streamName)
	if err != nil {
		return windows.InvalidHandle, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{RootDirectory: file, ObjectName: objectName}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var stream windows.Handle
	err = windows.NtCreateFile(
		&stream,
		windows.FILE_GENERIC_WRITE,
		&attributes,
		&windows.IO_STATUS_BLOCK{},
		nil,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
		windows.FILE_CREATE,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0,
		0,
	)
	if status, ok := errors.AsType[windows.NTStatus](err); ok {
		return windows.InvalidHandle, status.Errno()
	}
	return stream, err
}
