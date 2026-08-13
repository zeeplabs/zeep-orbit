package server

import (
	"bytes"
	"io"
	"net/http"
)

const maxBodyCapture = 2048

// captureResponseWriter wraps http.ResponseWriter to capture the response body.
type captureResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (w *captureResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureResponseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	if w.body.Len() < maxBodyCapture {
		remaining := maxBodyCapture - w.body.Len()
		if len(b) > remaining {
			w.body.Write(b[:remaining])
		} else {
			w.body.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *captureResponseWriter) Status() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

// readBody captures up to maxBodyCapture bytes of r.Body for logging, then
// restores r.Body so the handler still sees the full, untruncated stream —
// only the log entry is capped, never the request the handler acts on.
func readBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	var captured bytes.Buffer
	_, _ = io.CopyN(&captured, r.Body, maxBodyCapture)
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(captured.Bytes()), r.Body))
	return captured.String()
}
