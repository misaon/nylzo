//go:build darwin

package hostusage

import (
	"encoding/binary"
	"errors"
	"runtime"

	"golang.org/x/sys/unix"
)

const (
	supported       = true
	loadAverageSize = 24
	loadScaleOffset = 16
)

type machineReader struct {
	cores int
}

func newMachineReader() *machineReader {
	return &machineReader{cores: runtime.NumCPU()}
}

func (r *machineReader) read() (machineReading, error) {
	reading := unknownReading()
	var failures []error
	if memory, err := darwinMemory(); err != nil {
		failures = append(failures, err)
	} else {
		reading.MemoryUsed, reading.MemoryTotal = memory.Used, memory.Total
	}
	if load, err := darwinLoad(r.cores); err != nil {
		failures = append(failures, err)
	} else {
		reading.Load = load
		// Machine-wide processor use needs Mach calls, so development builds report the load per core.
		reading.Processor = min(load, 1)
	}
	return reading, errors.Join(failures...)
}

func darwinMemory() (memoryUse, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return memoryUse{}, err
	}
	pageSize, err := unix.SysctlUint32("hw.pagesize")
	if err != nil {
		return memoryUse{}, err
	}
	reclaimable := uint64(0)
	for _, name := range []string{
		"vm.page_free_count", "vm.page_speculative_count", "vm.page_pageable_external_count",
	} {
		pages, pagesErr := unix.SysctlUint32(name)
		if pagesErr != nil {
			return memoryUse{}, pagesErr
		}
		reclaimable += uint64(pages) * uint64(pageSize)
	}
	purgeable, err := unix.SysctlUint64("vm.page_purgeable_count")
	if err != nil {
		return memoryUse{}, err
	}
	reclaimable += purgeable * uint64(pageSize)
	return memoryUse{Used: total - min(reclaimable, total), Total: total}, nil
}

func darwinLoad(cores int) (float64, error) {
	data, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return unknownShare, err
	}
	if len(data) < loadAverageSize {
		return unknownShare, errMalformed
	}
	scale := binary.NativeEndian.Uint64(data[loadScaleOffset:])
	if scale == 0 || cores < 1 {
		return unknownShare, errMalformed
	}
	return float64(binary.NativeEndian.Uint32(data)) / float64(scale) / float64(cores), nil
}
