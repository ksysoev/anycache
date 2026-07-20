package http

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"time"

	"github.com/ksysoev/anycache"
)

type responseWriter struct {
	hasWritten bool
	header     http.Header
	statusCode int
	body       bytes.Buffer
}

type cachedResponse struct {
	Header     http.Header `json:"header"`
	StatusCode int         `json:"status_code"`
	Body       []byte      `json:"body"`
}

func newResponseWriter() *responseWriter {
	return &responseWriter{
		header: make(http.Header),
	}
}

func (rw *responseWriter) Header() http.Header {
	return rw.header
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if rw.hasWritten {
		return
	}

	rw.statusCode = statusCode
	rw.hasWritten = true
}

func (rw *responseWriter) Write(data []byte) (int, error) {
	if !rw.hasWritten {
		rw.WriteHeader(http.StatusOK)
	}

	return rw.body.Write(data)
}

func NewMiddleware(c *anycache.Cache) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := getRequestKey(r)

			if key == "" {
				// bypass caching for non-GET requests
				next.ServeHTTP(w, r)
				return
			}

			var res cachedResponse
			err := c.CacheStruct(r.Context(), key, time.Minute, func(ctx context.Context) (any, error) {
				resWriter := newResponseWriter()
				next.ServeHTTP(resWriter, r)

				statusCode := resWriter.statusCode
				if statusCode == 0 {
					statusCode = http.StatusOK
				}

				return cachedResponse{
					Header:     resWriter.header,
					StatusCode: statusCode,
					Body:       resWriter.body.Bytes(),
				}, nil
			}, &res)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			maps.Copy(w.Header(), res.Header)
			w.WriteHeader(res.StatusCode)

			if len(res.Body) != 0 {
				_, _ = w.Write(res.Body)
			}
		})
	}
}

func getRequestKey(req *http.Request) string {
	if req.Method != http.MethodGet {
		return ""
	}

	return req.URL.Path
}
