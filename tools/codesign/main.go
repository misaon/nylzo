package main

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxFileSize          = 1 << 30
	headerSize           = 32
	commandCountOffset   = 16
	commandSizesOffset   = 20
	commandCodeSignature = 0x1d
	commandBuildVersion  = 0x32
	codeSignatureSize    = 16
	signatureDataOffset  = 8
	signatureDataSize    = 12
	buildVersionSDK      = 16
	buildVersionSize     = 24
	segmentVMSize        = 32
	segmentFileSize      = 48
	signatureAlignment   = 16
	segmentAlignment     = 0x4000
	pageBitsX86          = 12
	pageBitsARM          = 14

	magicEmbeddedSignature  = 0xfade0cc0
	magicCodeDirectory      = 0xfade0c02
	slotCodeDirectory       = 0
	slotRequirements        = 2
	slotSignature           = 0x10000
	superBlobBlobs          = 3
	superBlobIndexSize      = 36
	codeDirectoryHeaderSize = 96
	codeDirectoryVersion    = 0x20500
	flagAdhoc               = 0x2
	flagRuntime             = 0x10000
	hashTypeSHA256          = 2
	execSegMainBinary       = 1
	specialSlots            = 2
)

var (
	errUsage          = errors.New("usage: codesign FILE")
	errTooLarge       = errors.New("larger than 1 GiB")
	errNotExecutable  = errors.New("not a 64-bit little-endian Mach-O executable")
	errArchitecture   = errors.New("unsupported architecture")
	errLayout         = errors.New("unexpected Mach-O layout")
	errNoCommandSpace = errors.New("no free space for LC_CODE_SIGNATURE after the load commands")
	errNoBuildVersion = errors.New("no LC_BUILD_VERSION to take the hardened runtime version from")
	emptyRequirements = [...]byte{0xfa, 0xde, 0x0c, 0x01, 0, 0, 0, 12, 0, 0, 0, 0}
	emptyBlobWrapper  = [...]byte{0xfa, 0xde, 0x0b, 0x01, 0, 0, 0, 8}
)

type blobIndex struct {
	Type, Offset uint32
}

type superBlobHeader struct {
	Magic, Length, Count uint32
	Blobs                [superBlobBlobs]blobIndex
}

type codeDirectoryHeader struct {
	Magic, Length, Version, Flags                     uint32
	HashOffset, IdentOffset, SpecialSlots, CodeSlots  uint32
	CodeLimit                                         uint32
	HashSize, HashType, Platform, PageSize            uint8
	Spare2, ScatterOffset, TeamOffset, Spare3         uint32
	CodeLimit64, ExecSegBase, ExecSegLimit, ExecFlags uint64
	Runtime, PreEncryptOffset                         uint32
}

type layout struct {
	pageBits         uint8
	codeEnd          uint32
	commandsEnd      uint32
	signatureCommand uint32
	linkeditCommand  uint32
	linkeditOffset   uint64
	textOffset       uint64
	textSize         uint64
	sdk              uint32
}

func main() {
	flag.Usage = func() { fmt.Fprintln(flag.CommandLine.Output(), errUsage) }
	flag.Parse()
	if err := run(flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "codesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return errUsage
	}
	path := filepath.Clean(args[0])
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	signed, err := sign(data, filepath.Base(path))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return replace(path, signed, info)
}

// replace renames a new file into place, since macOS caches the signature of a file it has run.
func replace(path string, data []byte, original os.FileInfo) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".codesign-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err == nil {
		err = file.Chmod(original.Mode().Perm())
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chtimes(file.Name(), original.ModTime(), original.ModTime())
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func sign(data []byte, identifier string) ([]byte, error) {
	identifierLength, fits := size32(len(identifier))
	if !fits {
		return nil, errTooLarge
	}
	place, err := inspect(data)
	if err != nil {
		return nil, err
	}
	signatureOffset := alignUp(place.codeEnd, signatureAlignment)
	directoryLength := place.directoryLength(signatureOffset, identifierLength)
	superBlobLength := superBlobIndexSize + directoryLength + uint32(len(emptyRequirements)+len(emptyBlobWrapper))
	signatureSize := alignUp(superBlobLength, signatureAlignment)
	signed := make([]byte, signatureOffset, signatureOffset+signatureSize)
	copy(signed, data[:place.codeEnd])
	place.reserve(signed, signatureOffset, signatureSize)
	requirementsOffset := superBlobIndexSize + directoryLength
	signed, _ = binary.Append(signed, binary.BigEndian, superBlobHeader{
		Magic:  magicEmbeddedSignature,
		Length: superBlobLength,
		Count:  superBlobBlobs,
		Blobs: [superBlobBlobs]blobIndex{
			{slotCodeDirectory, superBlobIndexSize},
			{slotRequirements, requirementsOffset},
			{slotSignature, requirementsOffset + uint32(len(emptyRequirements))},
		},
	})
	signed = place.appendCodeDirectory(signed, signatureOffset, identifier, identifierLength)
	signed = append(signed, emptyRequirements[:]...)
	signed = append(signed, emptyBlobWrapper[:]...)
	return append(signed, make([]byte, signatureSize-superBlobLength)...), nil
}

func inspect(data []byte) (layout, error) {
	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return layout{}, err
	}
	if file.Magic != macho.Magic64 || file.Type != macho.TypeExec || file.ByteOrder != binary.LittleEndian {
		return layout{}, errNotExecutable
	}
	pageBits, err := codePageBits(file.Cpu)
	if err != nil {
		return layout{}, err
	}
	place := layout{pageBits: pageBits, commandsEnd: headerSize + file.Cmdsz}
	size, fits := size32(len(data))
	if !fits {
		return layout{}, errTooLarge
	}
	err = place.readCommands(file)
	if err == nil {
		err = place.readSegments(file, size)
	}
	if err == nil && place.signatureCommand == 0 {
		place.codeEnd = size
		err = place.checkCommandSpace(file, data)
	} else if err == nil {
		command := data[place.signatureCommand:]
		place.codeEnd = binary.LittleEndian.Uint32(command[signatureDataOffset:])
		signatureEnd := uint64(place.codeEnd) + uint64(binary.LittleEndian.Uint32(command[signatureDataSize:]))
		if uint64(place.codeEnd) < place.linkeditOffset || signatureEnd != uint64(size) {
			err = fmt.Errorf("%w: the signature does not end __LINKEDIT", errLayout)
		}
	}
	return place, err
}

// codePageBits follows Apple's codesign, which hashes 16 KiB pages on arm64 and 4 KiB pages on x86_64.
func codePageBits(cpu macho.Cpu) (uint8, error) {
	switch cpu {
	case macho.CpuAmd64:
		return pageBitsX86, nil
	case macho.CpuArm64:
		return pageBitsARM, nil
	case macho.Cpu386, macho.CpuArm, macho.CpuPpc, macho.CpuPpc64:
		return 0, fmt.Errorf("%w: %v", errArchitecture, cpu)
	default:
		return 0, fmt.Errorf("%w: %v", errArchitecture, cpu)
	}
}

func (place *layout) readCommands(file *macho.File) error {
	offset := uint32(headerSize)
	for _, load := range file.Loads {
		raw := load.Raw()
		switch binary.LittleEndian.Uint32(raw) {
		case commandCodeSignature:
			if len(raw) != codeSignatureSize {
				return fmt.Errorf("%w: LC_CODE_SIGNATURE is %d bytes", errLayout, len(raw))
			}
			place.signatureCommand = offset
		case commandBuildVersion:
			if len(raw) < buildVersionSize {
				return fmt.Errorf("%w: LC_BUILD_VERSION is %d bytes", errLayout, len(raw))
			}
			place.sdk = binary.LittleEndian.Uint32(raw[buildVersionSDK:])
		default:
		}
		if segment, isSegment := load.(*macho.Segment); isSegment && segment.Name == "__LINKEDIT" {
			place.linkeditCommand = offset
		}
		offset += binary.LittleEndian.Uint32(raw[4:])
	}
	if offset != place.commandsEnd {
		return fmt.Errorf("%w: the load commands do not add up to sizeofcmds", errLayout)
	}
	if place.sdk == 0 {
		return errNoBuildVersion
	}
	return nil
}

func (place *layout) readSegments(file *macho.File, size uint32) error {
	text, linkedit := file.Segment("__TEXT"), file.Segment("__LINKEDIT")
	if text == nil || linkedit == nil || linkedit.Offset+linkedit.Filesz != uint64(size) {
		return fmt.Errorf("%w: __LINKEDIT must end the file", errLayout)
	}
	for _, load := range file.Loads {
		segment, isSegment := load.(*macho.Segment)
		if isSegment && segment != linkedit &&
			(segment.Offset+segment.Filesz > linkedit.Offset || segment.Addr+segment.Memsz > linkedit.Addr) {
			return fmt.Errorf("%w: %s lies after __LINKEDIT", errLayout, segment.Name)
		}
	}
	place.textOffset, place.textSize, place.linkeditOffset = text.Offset, text.Filesz, linkedit.Offset
	return nil
}

func (place *layout) checkCommandSpace(file *macho.File, data []byte) error {
	end := place.commandsEnd + codeSignatureSize
	for _, section := range file.Sections {
		if section.Offset != 0 && section.Offset < end {
			return errNoCommandSpace
		}
	}
	if end > place.codeEnd || !bytes.Equal(data[place.commandsEnd:end], make([]byte, codeSignatureSize)) {
		return errNoCommandSpace
	}
	return nil
}

func (place *layout) reserve(signed []byte, signatureOffset, signatureSize uint32) {
	order := binary.LittleEndian
	if place.signatureCommand == 0 {
		place.signatureCommand = place.commandsEnd
		command := signed[place.signatureCommand:]
		order.PutUint32(command, commandCodeSignature)
		order.PutUint32(command[4:], codeSignatureSize)
		order.PutUint32(signed[commandCountOffset:], order.Uint32(signed[commandCountOffset:])+1)
		order.PutUint32(signed[commandSizesOffset:], order.Uint32(signed[commandSizesOffset:])+codeSignatureSize)
	}
	order.PutUint32(signed[place.signatureCommand+signatureDataOffset:], signatureOffset)
	order.PutUint32(signed[place.signatureCommand+signatureDataSize:], signatureSize)
	fileSize := uint64(signatureOffset) + uint64(signatureSize) - place.linkeditOffset
	order.PutUint64(signed[place.linkeditCommand+segmentFileSize:], fileSize)
	order.PutUint64(signed[place.linkeditCommand+segmentVMSize:], (fileSize+segmentAlignment-1)&^(segmentAlignment-1))
}

func (place *layout) codeSlots(codeLimit uint32) uint32 {
	return alignUp(codeLimit, 1<<place.pageBits) >> place.pageBits
}

func (place *layout) directoryLength(codeLimit, identifierLength uint32) uint32 {
	return codeDirectoryHeaderSize + identifierLength + 1 + (specialSlots+place.codeSlots(codeLimit))*sha256.Size
}

func (place *layout) appendCodeDirectory(
	signed []byte,
	codeLimit uint32,
	identifier string,
	identifierLength uint32,
) []byte {
	codeSlots := place.codeSlots(codeLimit)
	signed, _ = binary.Append(signed, binary.BigEndian, codeDirectoryHeader{
		Magic:        magicCodeDirectory,
		Length:       place.directoryLength(codeLimit, identifierLength),
		Version:      codeDirectoryVersion,
		Flags:        flagAdhoc | flagRuntime,
		HashOffset:   codeDirectoryHeaderSize + identifierLength + 1 + specialSlots*sha256.Size,
		IdentOffset:  codeDirectoryHeaderSize,
		SpecialSlots: specialSlots,
		CodeSlots:    codeSlots,
		CodeLimit:    codeLimit,
		HashSize:     sha256.Size,
		HashType:     hashTypeSHA256,
		PageSize:     place.pageBits,
		ExecSegBase:  place.textOffset,
		ExecSegLimit: place.textSize,
		ExecFlags:    execSegMainBinary,
		Runtime:      place.sdk,
	})
	signed = append(append(signed, identifier...), 0)
	requirements := sha256.Sum256(emptyRequirements[:])
	signed = append(signed, requirements[:]...)
	signed = append(signed, make([]byte, sha256.Size)...)
	pageSize := uint32(1) << place.pageBits
	for page := range codeSlots {
		hash := sha256.Sum256(signed[page*pageSize : min((page+1)*pageSize, codeLimit)])
		signed = append(signed, hash[:]...)
	}
	return signed
}

func size32(length int) (uint32, bool) {
	if length < 0 || length > maxFileSize {
		return 0, false
	}
	return uint32(length), true
}

func alignUp(value, alignment uint32) uint32 {
	return (value + alignment - 1) / alignment * alignment
}
