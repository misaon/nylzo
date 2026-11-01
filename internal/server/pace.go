package server

import (
	"io"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const (
	paceChunk = 64 << 10
	paceSlack = 10 * time.Second
)

// pace is the minimum transfer rate of spec §9: every 64 KiB of an object must move within its own window, so a
// peer that stops reading or sending loses its stream slot, upload slot and disk reservation within seconds.
type pace struct {
	chunk  int
	window time.Duration
}

func defaultPace() pace {
	return pace{chunk: paceChunk, window: time.Duration(paceChunk)*time.Second/store.MinTransferRate + paceSlack}
}

type pacedReader struct {
	reader     io.Reader
	controller *http.ResponseController
	pace       pace
	overall    time.Time
	left       int
}

func (p *pacedReader) Read(buffer []byte) (int, error) {
	if p.left <= 0 {
		if err := p.controller.SetReadDeadline(earlier(time.Now().Add(p.pace.window), p.overall)); err != nil {
			return 0, err
		}
		p.left = p.pace.chunk
	}
	read, err := p.reader.Read(buffer[:min(len(buffer), p.left)])
	p.left -= read
	if err == io.EOF {
		if deadlineErr := p.controller.SetReadDeadline(p.overall); deadlineErr != nil {
			return read, deadlineErr
		}
	}
	return read, err
}

type pacedWriter struct {
	writer     io.Writer
	controller *http.ResponseController
	pace       pace
	overall    time.Time
}

func (p *pacedWriter) Write(data []byte) (int, error) {
	written := 0
	for written < len(data) {
		if err := p.controller.SetWriteDeadline(earlier(time.Now().Add(p.pace.window), p.overall)); err != nil {
			return written, err
		}
		chunk := data[written:min(len(data), written+p.pace.chunk)]
		count, err := p.writer.Write(chunk)
		written += count
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func earlier(first, second time.Time) time.Time {
	if second.Before(first) {
		return second
	}
	return first
}
