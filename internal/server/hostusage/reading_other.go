//go:build !(linux || darwin)

package hostusage

const supported = false

type machineReader struct{}

func newMachineReader() *machineReader {
	return &machineReader{}
}

func (*machineReader) read() (machineReading, error) {
	return unknownReading(), nil
}
