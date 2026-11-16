package hostusage

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"runtime"
)

const (
	supported     = true
	procReadLimit = 64 << 10
)

type machineReader struct {
	root        fs.FS
	cores       int
	previous    processorTicks
	hasPrevious bool
}

func newMachineReader() *machineReader {
	return newProcReader(os.DirFS("/proc"), runtime.NumCPU())
}

func newProcReader(root fs.FS, cores int) *machineReader {
	return &machineReader{root: root, cores: cores}
}

func (r *machineReader) read() (machineReading, error) {
	reading := unknownReading()
	var failures []error
	if data, err := readBounded(r.root, "stat"); err != nil {
		failures = append(failures, err)
	} else if ticks, parseErr := parseProcessorTicks(data); parseErr != nil {
		failures = append(failures, parseErr)
	} else {
		if r.hasPrevious {
			reading.Processor = processorShare(r.previous, ticks)
		}
		r.previous, r.hasPrevious = ticks, true
	}
	if data, err := readBounded(r.root, "meminfo"); err != nil {
		failures = append(failures, err)
	} else if memory, parseErr := parseMemory(data); parseErr != nil {
		failures = append(failures, parseErr)
	} else {
		reading.MemoryUsed, reading.MemoryTotal = memory.Used, memory.Total
	}
	if data, err := readBounded(r.root, "loadavg"); err != nil {
		failures = append(failures, err)
	} else if load, parseErr := parseLoad(data, r.cores); parseErr != nil {
		failures = append(failures, parseErr)
	} else {
		reading.Load = load
	}
	return reading, errors.Join(failures...)
}

func readBounded(root fs.FS, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, procReadLimit))
}
