package hostusage

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	kibibyte       = 1024
	idleField      = 4
	iowaitField    = 5
	countedFields  = 8
	minimumFields  = iowaitField + 1
	memoryTotalKey = "MemTotal:"
	memoryFreeKey  = "MemAvailable:"
)

var errMalformed = errors.New("hostusage: malformed reading")

type processorTicks struct {
	Busy  uint64
	Total uint64
}

func parseProcessorTicks(data []byte) (processorTicks, error) {
	line, _, _ := bytes.Cut(data, []byte("\n"))
	fields := strings.Fields(string(line))
	if len(fields) < minimumFields || fields[0] != "cpu" {
		return processorTicks{}, fmt.Errorf("%w: processor line", errMalformed)
	}
	var total, idle uint64
	for index, field := range fields[1:min(len(fields), countedFields+1)] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return processorTicks{}, fmt.Errorf("%w: processor ticks", errMalformed)
		}
		total += value
		if index+1 == idleField || index+1 == iowaitField {
			idle += value
		}
	}
	return processorTicks{Busy: total - idle, Total: total}, nil
}

func processorShare(previous, current processorTicks) float64 {
	if current.Total <= previous.Total || current.Busy < previous.Busy {
		return unknownShare
	}
	share := float64(current.Busy-previous.Busy) / float64(current.Total-previous.Total)
	return min(share, 1)
}

type memoryUse struct {
	Used  uint64
	Total uint64
}

func parseMemory(data []byte) (memoryUse, error) {
	var total, available uint64
	var foundTotal, foundAvailable bool
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != memoryTotalKey && fields[0] != memoryFreeKey) {
			continue
		}
		kibibytes, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil || kibibytes > math.MaxUint64/kibibyte {
			return memoryUse{}, fmt.Errorf("%w: memory value", errMalformed)
		}
		if fields[0] == memoryTotalKey {
			total, foundTotal = kibibytes*kibibyte, true
		} else {
			available, foundAvailable = kibibytes*kibibyte, true
		}
	}
	if !foundTotal || !foundAvailable || total == 0 {
		return memoryUse{}, fmt.Errorf("%w: memory totals", errMalformed)
	}
	return memoryUse{Used: total - min(available, total), Total: total}, nil
}

func parseLoad(data []byte, cores int) (float64, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || cores < 1 {
		return unknownShare, fmt.Errorf("%w: load average", errMalformed)
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(load) || math.IsInf(load, 0) || load < 0 {
		return unknownShare, fmt.Errorf("%w: load average", errMalformed)
	}
	return load / float64(cores), nil
}
