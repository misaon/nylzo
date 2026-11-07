package transport

import (
	"io"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

const (
	PaceChunk = 64 << 10
	paceSlack = 10 * time.Second
)

// Pace is the minimum transfer rate of spec §9: every 64 KiB of an object must move within its own window, so a
// peer that stops reading or sending loses its stream slot, upload slot and disk reservation within seconds.
type Pace struct {
	Chunk  int
	Window time.Duration
}

func DefaultPace() Pace {
	return Pace{Chunk: PaceChunk, Window: time.Duration(PaceChunk)*time.Second/store.MinTransferRate + paceSlack}
}

func (p Pace) Reader(reader io.Reader, controller *http.ResponseController, overall time.Time) io.Reader {
	return &pacedReader{reader: reader, controller: controller, pace: p, overall: overall}
}

func (p Pace) Writer(writer io.Writer, controller *http.ResponseController, overall time.Time) io.Writer {
	return &pacedWriter{writer: writer, controller: controller, pace: p, overall: overall}
}

type pacedReader struct {
	reader     io.Reader
	controller *http.ResponseController
	pace       Pace
	overall    time.Time
	left       int
}

func (p *pacedReader) Read(buffer []byte) (int, error) {
	if p.left <= 0 {
		if err := p.controller.SetReadDeadline(earlier(time.Now().Add(p.pace.Window), p.overall)); err != nil {
			return 0, err
		}
		p.left = p.pace.Chunk
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
	pace       Pace
	overall    time.Time
}

func (p *pacedWriter) Write(data []byte) (int, error) {
	written := 0
	for written < len(data) {
		if err := p.controller.SetWriteDeadline(earlier(time.Now().Add(p.pace.Window), p.overall)); err != nil {
			return written, err
		}
		chunk := data[written:min(len(data), written+p.pace.Chunk)]
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
