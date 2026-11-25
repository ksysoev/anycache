package httpcache

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRecorder(t *testing.T) {
	tests := []struct {
		handler      func(t *testing.T, w http.ResponseWriter)
		wantHeader   http.Header
		name         string
		wantBody     string
		maxBody      int
		wantStatus   int
		wantOverflow bool
	}{
		{
			name:       "implicit 200 on write",
			handler:    func(_ *testing.T, w http.ResponseWriter) { _, _ = w.Write([]byte("hi")) },
			wantStatus: http.StatusOK,
			wantBody:   "hi",
			wantHeader: http.Header{},
		},
		{
			name:       "implicit 200 without write",
			handler:    func(*testing.T, http.ResponseWriter) {},
			wantStatus: http.StatusOK,
			wantHeader: http.Header{},
		},
		{
			name: "headers snapshotted at WriteHeader",
			handler: func(_ *testing.T, w http.ResponseWriter) {
				w.Header().Set("X-Before", "1")
				w.WriteHeader(http.StatusCreated)
				w.Header().Set("X-After", "1")
			},
			wantStatus: http.StatusCreated,
			wantHeader: http.Header{"X-Before": {"1"}},
		},
		{
			name: "informational status ignored",
			handler: func(_ *testing.T, w http.ResponseWriter) {
				w.Header().Set("Link", "</a.css>")
				w.WriteHeader(http.StatusEarlyHints)
				w.WriteHeader(http.StatusAccepted)
				w.WriteHeader(http.StatusTeapot)
			},
			wantStatus: http.StatusAccepted,
			wantHeader: http.Header{"Link": {"</a.css>"}},
		},
		{
			name:       "invalid status becomes 500",
			handler:    func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(42) },
			wantStatus: http.StatusInternalServerError,
			wantHeader: http.Header{},
		},
		{
			name: "body not allowed for 204",
			handler: func(_ *testing.T, w http.ResponseWriter) {
				w.WriteHeader(http.StatusNoContent)
				_, _ = w.Write([]byte("ignored"))
			},
			wantStatus: http.StatusNoContent,
			wantHeader: http.Header{},
		},
		{
			name: "overflow keeps full body",
			handler: func(t *testing.T, w http.ResponseWriter) {
				t.Helper()

				_, _ = w.Write([]byte("1234"))
				assert.NoError(t, http.NewResponseController(w).Flush())
				_, _ = w.Write([]byte("5678"))
			},
			maxBody:      5,
			wantStatus:   http.StatusOK,
			wantBody:     "12345678",
			wantHeader:   http.Header{},
			wantOverflow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder(tt.maxBody)
			tt.handler(t, rec)

			resp := rec.result(time.Now())

			assert.Equal(t, tt.wantStatus, resp.status)
			assert.Equal(t, tt.wantHeader, resp.header)
			assert.Equal(t, tt.wantBody, string(resp.body))
			assert.Equal(t, tt.wantOverflow, resp.overflow)
		})
	}
}

func TestRecorder_NoHijackOrUnwrap(t *testing.T) {
	var w http.ResponseWriter = newRecorder(0)

	_, isHijacker := w.(http.Hijacker)
	_, isUnwrapper := w.(interface{ Unwrap() http.ResponseWriter })

	assert.False(t, isHijacker)
	assert.False(t, isUnwrapper)
}
