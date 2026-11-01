package server

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// closeUnreadBodies closes an HTTP/1.x connection whose request body was not read to its end when the answer
// starts, because net/http would otherwise wait for up to 256 KiB of a body that may never arrive (spec §9).
func closeUnreadBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 || r.ContentLength == 0 || r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}
		body := &countedBody{ReadCloser: r.Body}
		r.Body = body
		next.ServeHTTP(&closingWriter{ResponseWriter: w, request: r, body: body}, r)
		if body.unread(r.ContentLength) {
			_ = http.NewResponseController(w).SetReadDeadline(time.Now())
		}
	})
}

type countedBody struct {
	io.ReadCloser

	read     atomic.Int64
	finished atomic.Bool
}

func (b *countedBody) Read(buffer []byte) (int, error) {
	read, err := b.ReadCloser.Read(buffer)
	b.read.Add(int64(read))
	if err == io.EOF {
		b.finished.Store(true)
	}
	return read, err
}

func (b *countedBody) unread(declared int64) bool {
	return !b.finished.Load() && (declared < 0 || b.read.Load() < declared)
}

type closingWriter struct {
	http.ResponseWriter

	request *http.Request
	body    *countedBody
	started bool
}

func (w *closingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *closingWriter) WriteHeader(code int) {
	w.start()
	w.ResponseWriter.WriteHeader(code)
}

func (w *closingWriter) Write(data []byte) (int, error) {
	w.start()
	return w.ResponseWriter.Write(data)
}

func (w *closingWriter) ReadFrom(source io.Reader) (int64, error) {
	w.start()
	return io.Copy(w.ResponseWriter, source)
}

func (w *closingWriter) start() {
	if !w.started && w.body.unread(w.request.ContentLength) {
		w.ResponseWriter.Header().Set("Connection", "close")
	}
	w.started = true
}
