package core

const (
	Version         = 1
	FragmentVersion = 1

	IDLen     = 16
	SecretLen = 32
	KeyLen    = 32
	TokenLen  = 32
	IVLen     = 12
	TagLen    = 16
	HeaderLen = 15
	PolicyLen = 6
	AADLen    = HeaderLen + IDLen + PolicyLen

	IDChars    = 22
	TokenChars = 43

	MaxTextBytes   = 100 * 1024
	MaxFileBytes   = 5 * 1024 * 1024
	MaxMetaBytes   = 102 * 1024
	MaxObjectBytes = MaxFileBytes + MaxTextBytes + 64*1024 + 64
	MaxFileName    = 255
	MaxMime        = 255

	PadText = 4096
	PadFile = 65536

	MinTTLSeconds = 60
)
