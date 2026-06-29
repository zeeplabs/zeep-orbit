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

// readBody reads r.Body without consuming the original (replaces it after read).
func readBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyCapture))
	r.Body = io.NopCloser(bytes.NewReader(body))
	return string(body)
}
