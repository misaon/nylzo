package transport

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var errDeadline = errors.New("deadline refused")

type deadlineWriter struct {
	http.ResponseWriter

	refusedDeadline time.Time
	deadlines       []time.Time
	writeErr        error
}

func (w *deadlineWriter) SetReadDeadline(deadline time.Time) error {
	return w.setDeadline(deadline)
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	return w.setDeadline(deadline)
}

func (w *deadlineWriter) Write(data []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.ResponseWriter.Write(data)
}

func (w *deadlineWriter) setDeadline(deadline time.Time) error {
	if deadline.Equal(w.refusedDeadline) {
		return errDeadline
	}
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestPacedTransfersFailWhenNoDeadlineCanBeSet(t *testing.T) {
	t.Parallel()
	controller := http.NewResponseController(httptest.NewRecorder())
	overall := time.Now().Add(time.Hour)
	if _, err := PacedReader(strings.NewReader("body"), controller, overall).Read(make([]byte, 4)); err == nil {
		t.Error("a read went on without a deadline")
	}
	if _, err := PacedWriter(io.Discard, controller, overall).Write([]byte("body")); err == nil {
		t.Error("a write went on without a deadline")
	}
}

func TestPacedReaderRestoresTheOverallDeadlineAtTheEnd(t *testing.T) {
	t.Parallel()
	overall := time.Now().Add(time.Hour)
	writer := &deadlineWriter{ResponseWriter: httptest.NewRecorder(), refusedDeadline: overall}
	reader := PacedReader(strings.NewReader("body"), http.NewResponseController(writer), overall)
	read, err := io.ReadAll(reader)
	if !errors.Is(err, errDeadline) || string(read) != "body" {
		t.Fatalf("read %q, %v", read, err)
	}
}

func TestPacedWriterNeverSetsADeadlinePastTheOverallOne(t *testing.T) {
	t.Parallel()
	overall := time.Now().Add(time.Second)
	writer := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	written, err := PacedWriter(writer, http.NewResponseController(writer), overall).Write(make([]byte, PaceChunk+1))
	if err != nil || written != PaceChunk+1 {
		t.Fatalf("wrote %d, %v", written, err)
	}
	if len(writer.deadlines) != 2 || !writer.deadlines[0].Equal(overall) || !writer.deadlines[1].Equal(overall) {
		t.Fatalf("deadlines %v, want the overall %v twice", writer.deadlines, overall)
	}
	writer.writeErr = io.ErrClosedPipe
	if written, err = PacedWriter(
		writer,
		http.NewResponseController(writer),
		overall,
	).Write([]byte("body")); !errors.Is(
		err,
		io.ErrClosedPipe,
	) ||
		written != 0 {
		t.Fatalf("a failed write gave %d, %v", written, err)
	}
}

func TestACopiedAnswerClosesAConnectionWithAnUnreadBody(t *testing.T) {
	t.Parallel()
	handler := CloseUnreadBodies(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.Copy(w, io.LimitReader(strings.NewReader("answer"), 6)); err != nil {
			t.Error(err)
		}
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(make([]byte, 64))))
	if response.Body.String() != "answer" || response.Header().Get("Connection") != "close" {
		t.Fatalf("answer %q with Connection %q", response.Body.String(), response.Header().Get("Connection"))
	}
}
