package main

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testSDK        = 0x1a0200
	textFileSize   = 0x2000
	linkeditSize   = 0x150
	sectionOffset  = 0x1000
	executableType = 2
	dylibType      = 6
)

type executable struct {
	cpu              macho.Cpu
	fileType         uint32
	sectionOffset    uint32
	buildVersionSize uint32
	linkeditAddr     uint64
	linkerSignature  uint32
	trailing         int
}

type segmentCommand struct {
	Cmd, Cmdsize                uint32
	Name                        [16]byte
	Addr, Memsz, Offset, Filesz uint64
	Maxprot, Prot, Nsect, Flag  uint32
}

type sectionHeader struct {
	Name, Seg                                       [16]byte
	Addr, Size                                      uint64
	Offset, Align, Reloff, Nreloc, Flags, Reserved1 uint32
	Reserved2, Reserved3                            uint32
}

func goExecutable(cpu macho.Cpu) executable {
	return executable{
		cpu:              cpu,
		fileType:         executableType,
		sectionOffset:    sectionOffset,
		buildVersionSize: buildVersionSize,
		linkeditAddr:     0x100000000 + textFileSize,
	}
}

func name16(name string) [16]byte {
	var padded [16]byte
	copy(padded[:], name)
	return padded
}

func (spec executable) build() []byte {
	text := segmentCommand{
		Cmd: uint32(macho.LoadCmdSegment64), Cmdsize: 152, Name: name16("__TEXT"),
		Addr: 0x100000000, Memsz: textFileSize, Filesz: textFileSize, Maxprot: 5, Prot: 5, Nsect: 1,
	}
	section := sectionHeader{
		Name: name16("__text"), Seg: name16("__TEXT"), Addr: 0x100000000 + uint64(spec.sectionOffset),
		Size: textFileSize - uint64(spec.sectionOffset), Offset: spec.sectionOffset, Flags: 0x80000400,
	}
	linkedit := segmentCommand{
		Cmd:     uint32(macho.LoadCmdSegment64),
		Cmdsize: 72,
		Name:    name16("__LINKEDIT"),
		Addr:    spec.linkeditAddr,
		Memsz:   linkeditSize + uint64(spec.linkerSignature),
		Offset:  textFileSize,
		Filesz:  linkeditSize + uint64(spec.linkerSignature),
		Maxprot: 1,
		Prot:    1,
	}
	var commands []byte
	commands, _ = binary.Append(commands, binary.LittleEndian, text)
	commands, _ = binary.Append(commands, binary.LittleEndian, section)
	count := uint32(2)
	if spec.buildVersionSize > 0 {
		buildVersion := []uint32{commandBuildVersion, spec.buildVersionSize, 1, 0xd0000, testSDK, 0}
		commands, _ = binary.Append(commands, binary.LittleEndian, buildVersion[:spec.buildVersionSize/4])
		count++
	}
	commands, _ = binary.Append(commands, binary.LittleEndian, linkedit)
	if spec.linkerSignature > 0 {
		signature := []uint32{
			commandCodeSignature,
			codeSignatureSize,
			textFileSize + linkeditSize,
			spec.linkerSignature,
		}
		commands, _ = binary.Append(commands, binary.LittleEndian, signature)
		count++
	}
	header := []uint32{macho.Magic64, uint32(spec.cpu), 0, spec.fileType, count, uint32(len(commands)), 0, 0}
	data, _ := binary.Append(nil, binary.LittleEndian, header)
	data = append(data, commands...)
	data = append(data, make([]byte, textFileSize+linkeditSize-len(data))...)
	for index := sectionOffset; index < len(data); index++ {
		data[index] = byte(index)
	}
	data = append(data, bytes.Repeat([]byte{0xfa}, int(spec.linkerSignature))...)
	return append(data, make([]byte, spec.trailing)...)
}

type codeDirectoryBlob struct {
	codeDirectoryHeader

	blob []byte
}

func codeDirectoryOf(t *testing.T, signed []byte) codeDirectoryBlob {
	t.Helper()
	file, err := macho.NewFile(bytes.NewReader(signed))
	if err != nil {
		t.Fatal(err)
	}
	var dataOffset uint32
	for _, load := range file.Loads {
		if raw := load.Raw(); binary.LittleEndian.Uint32(raw) == commandCodeSignature {
			dataOffset = binary.LittleEndian.Uint32(raw[8:])
		}
	}
	if dataOffset == 0 {
		t.Fatal("no LC_CODE_SIGNATURE")
	}
	linkedit := file.Segment("__LINKEDIT")
	if linkedit.Offset+linkedit.Filesz != uint64(len(signed)) || linkedit.Memsz%segmentAlignment != 0 {
		t.Fatalf("__LINKEDIT does not cover the signature: %+v, file size %d", linkedit.SegmentHeader, len(signed))
	}
	blob := signed[dataOffset+superBlobIndexSize:]
	var directory codeDirectoryHeader
	if _, err = binary.Decode(blob, binary.BigEndian, &directory); err != nil {
		t.Fatal(err)
	}
	if directory.CodeLimit != dataOffset {
		t.Fatalf("the code limit %d is not the signature offset %d", directory.CodeLimit, dataOffset)
	}
	return codeDirectoryBlob{directory, blob}
}

// TestSignAddsHardenedRuntime covers the macOS release signature: adhoc and runtime flags, the file name as identifier,
// the SDK as runtime version, page hashes over the final header, and the same bytes when a signed file is signed again.
func TestSignAddsHardenedRuntime(t *testing.T) {
	for _, cpu := range []macho.Cpu{macho.CpuAmd64, macho.CpuArm64} {
		t.Run(cpu.String(), func(t *testing.T) {
			signed, err := sign(goExecutable(cpu).build(), "nylzo")
			if err != nil {
				t.Fatal(err)
			}
			directory := codeDirectoryOf(t, signed)
			blob := directory.blob
			if directory.Magic != magicCodeDirectory || directory.Flags != flagAdhoc|flagRuntime ||
				directory.Version != codeDirectoryVersion || directory.Runtime != testSDK {
				t.Fatalf("code directory %+v", directory)
			}
			identifier := blob[directory.IdentOffset : directory.IdentOffset+uint32(len("nylzo\x00"))]
			if string(identifier) != "nylzo\x00" {
				t.Fatalf("identifier %q", identifier)
			}
			firstPage := sha256.Sum256(signed[:min(1<<directory.PageSize, int(directory.CodeLimit))])
			if !bytes.Equal(blob[directory.HashOffset:directory.HashOffset+sha256.Size], firstPage[:]) {
				t.Fatal("the first page hash does not match the signed header")
			}
			again, err := sign(signed, "nylzo")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, signed) {
				t.Fatal("signing a signed file changed it")
			}
		})
	}
}

// TestSignReplacesTheLinkerSignature covers darwin/arm64, where Go's linker already wrote a shorter ad-hoc signature.
func TestSignReplacesTheLinkerSignature(t *testing.T) {
	unsigned := goExecutable(macho.CpuArm64)
	linkerSigned := unsigned
	linkerSigned.linkerSignature = 0x40
	want, err := sign(unsigned.build(), "nylzo")
	if err != nil {
		t.Fatal(err)
	}
	got, err := sign(linkerSigned.build(), "nylzo")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("replacing the linker signature gave other bytes than signing the unsigned file")
	}
}

func TestSignRefusesUnexpectedFiles(t *testing.T) {
	withoutRoom := goExecutable(macho.CpuAmd64)
	withoutRoom.sectionOffset = 0x100
	library := goExecutable(macho.CpuArm64)
	library.fileType = dylibType
	arm32 := goExecutable(macho.CpuArm)
	noBuildVersion := goExecutable(macho.CpuArm64)
	noBuildVersion.buildVersionSize = 0
	shortBuildVersion := goExecutable(macho.CpuArm64)
	shortBuildVersion.buildVersionSize = 16
	trailing := goExecutable(macho.CpuAmd64)
	trailing.trailing = 16
	mappedAfter := goExecutable(macho.CpuAmd64)
	mappedAfter.linkeditAddr = 0x100000000
	occupied := goExecutable(macho.CpuAmd64).build()
	occupied[headerSize+binary.LittleEndian.Uint32(occupied[commandSizesOffset:])] = 1
	linkerSigned := goExecutable(macho.CpuArm64)
	linkerSigned.linkerSignature = 0x40
	shortSignature := linkerSigned.build()
	signatureCommand := headerSize + binary.LittleEndian.Uint32(shortSignature[commandSizesOffset:]) - codeSignatureSize
	binary.LittleEndian.PutUint32(shortSignature[signatureCommand+signatureDataSize:], 0x30)
	oversized := linkerSigned.build()
	binary.LittleEndian.PutUint32(oversized[signatureCommand+4:], codeSignatureSize+8)
	binary.LittleEndian.PutUint32(oversized[commandSizesOffset:], signatureCommand-headerSize+codeSignatureSize+8)
	early := linkerSigned.build()
	binary.LittleEndian.PutUint32(early[signatureCommand+signatureDataOffset:], textFileSize-0x10)
	binary.LittleEndian.PutUint32(early[signatureCommand+signatureDataSize:], linkeditSize+0x50)
	for _, test := range []struct {
		name string
		data []byte
		want error
	}{
		{"dynamic library", library.build(), errNotExecutable},
		{"unsupported architecture", arm32.build(), errArchitecture},
		{"no room for the load command", withoutRoom.build(), errNoCommandSpace},
		{"bytes after the load commands", occupied, errNoCommandSpace},
		{"no build version", noBuildVersion.build(), errNoBuildVersion},
		{"short build version", shortBuildVersion.build(), errLayout},
		{"data after __LINKEDIT", trailing.build(), errLayout},
		{"segment mapped after __LINKEDIT", mappedAfter.build(), errLayout},
		{"signature that does not end the file", shortSignature, errLayout},
		{"signature command of another size", oversized, errLayout},
		{"signature before __LINKEDIT", early, errLayout},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := sign(test.data, "nylzo"); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
	var format *macho.FormatError
	if _, err := sign([]byte("#!/bin/sh\n"), "nylzo"); !errors.As(err, &format) {
		t.Fatalf("a script was not refused as Mach-O: %v", err)
	}
}

func TestRunSignsInPlaceAndKeepsModeAndTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo")
	data := goExecutable(macho.CpuArm64).build()
	commitTime := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, commitTime, commitTime); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{path}); err != nil {
		t.Fatal(err)
	}
	signed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := sign(data, "nylzo")
	if err != nil || !bytes.Equal(signed, want) {
		t.Fatalf("the file holds other bytes than sign returns (%v)", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 || !info.ModTime().Equal(commitTime) {
		t.Fatalf("mode or time changed: %v, %v", info, err)
	}
	if err = run(nil); !errors.Is(err, errUsage) {
		t.Fatalf("no argument gave %v", err)
	}
}
