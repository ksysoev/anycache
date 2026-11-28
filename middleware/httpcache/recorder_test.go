package httpcache

import (
	"bytes"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecorder(t *testing.T) {
	tests := []struct {
		handler      func(t *testing.T, w http.ResponseWriter)
		wantHeader   http.Header
		name         string
		wantBody     string
		maxBody      int
		wantStatus   int
		wantInvalid  int
		wantOverflow bool
		hasInvalid   bool
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
			name: "header keys are canonicalized and merged",
			handler: func(_ *testing.T, w http.ResponseWriter) {
				w.Header()["Set-Cookie"] = []string{"a=1"}
				w.Header()["set-cookie"] = []string{"b=2"}
				w.Header()["cache-control"] = []string{"private"}
				w.WriteHeader(http.StatusOK)
			},
			wantStatus: http.StatusOK,
			wantHeader: http.Header{"Set-Cookie": {"a=1", "b=2"}, "Cache-Control": {"private"}},
		},
		{
			name:       "zero status is invalid",
			handler:    func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(0) },
			wantStatus: http.StatusInternalServerError,
			hasInvalid: true,
			wantHeader: http.Header{},
		},
		{
			name:        "invalid status becomes 500",
			handler:     func(_ *testing.T, w http.ResponseWriter) { w.WriteHeader(42) },
			wantStatus:  http.StatusInternalServerError,
			wantInvalid: 42,
			hasInvalid:  true,
			wantHeader:  http.Header{},
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
			name: "overflow without sink is discarded",
			handler: func(t *testing.T, w http.ResponseWriter) {
				t.Helper()

				_, _ = w.Write([]byte("1234"))
				assert.NoError(t, http.NewResponseController(w).Flush())

				_, err := w.Write([]byte("5678"))
				assert.ErrorIs(t, err, errStreamDetached)
			},
			maxBody:      5,
			wantStatus:   http.StatusOK,
			wantBody:     "",
			wantHeader:   http.Header{},
			wantOverflow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newRecorder(tt.maxBody, nil)
			tt.handler(t, rec)

			resp := rec.result(time.Now())

			assert.Equal(t, tt.wantStatus, resp.status)
			assert.Equal(t, tt.wantHeader, resp.header)
			assert.Equal(t, tt.wantBody, string(resp.body))
			assert.Equal(t, tt.wantOverflow, resp.overflow)
			assert.Equal(t, tt.wantInvalid, rec.invalidStatus)
			assert.Equal(t, tt.hasInvalid, rec.hasInvalidStatus)
		})
	}
}

func TestRecorder_NoHijackOrUnwrap(t *testing.T) {
	var w http.ResponseWriter = newRecorder(0, nil)

	_, isHijacker := w.(http.Hijacker)
	_, isUnwrapper := w.(interface{ Unwrap() http.ResponseWriter })

	assert.False(t, isHijacker)
	assert.False(t, isUnwrapper)
}

// fakeSink records what a recorder streams.
type fakeSink struct {
	startErr  error
	writeErr  error
	header    http.Header
	buffered  []byte
	first     []byte
	written   bytes.Buffer
	status    int
	starts    int
	flushes   int
	startSize int
}

func (s *fakeSink) startStream(status int, header http.Header, buffered, p []byte) error {
	s.starts++
	s.status = status
	s.header = header
	s.startSize = len(buffered)
	s.buffered = append([]byte(nil), buffered...)
	s.first = append([]byte(nil), p...)

	return s.startErr
}

func (s *fakeSink) write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}

	return s.written.Write(p)
}

func (s *fakeSink) flush() error {
	s.flushes++
	return nil
}

var errClientGone = errors.New("client gone")

func TestRecorder_Streaming(t *testing.T) {
	tests := []struct {
		startErr     error
		writeErr     error
		name         string
		writes       []string
		wantBuffered string
		wantFirst    string
		wantStreamed string
		wantBody     string
		wantErrs     []error
		wantStarts   int
		wantFlushes  int
		flushAfter   int
		maxBody      int
		wantOverflow bool
		wantLost     bool
	}{
		{
			name:       "under the limit is buffered",
			maxBody:    8,
			writes:     []string{"1234", "5678"},
			flushAfter: 1,
			wantBody:   "12345678",
			wantErrs:   []error{nil, nil},
		},
		{
			name:       "no limit never streams",
			maxBody:    0,
			writes:     []string{"1234", "5678"},
			flushAfter: 1,
			wantBody:   "12345678",
			wantErrs:   []error{nil, nil},
		},
		{
			name:         "crossing the limit streams the rest",
			maxBody:      5,
			writes:       []string{"1234", "5678", "90"},
			flushAfter:   2,
			wantBuffered: "1234",
			wantFirst:    "5678",
			wantStreamed: "90",
			wantErrs:     []error{nil, nil, nil},
			wantStarts:   1,
			wantFlushes:  1,
			wantOverflow: true,
		},
		{
			name:         "failed stream start loses the client and discards the rest",
			maxBody:      5,
			startErr:     errClientGone,
			writes:       []string{"1234", "5678", "90"},
			flushAfter:   2,
			wantBuffered: "1234",
			wantFirst:    "5678",
			wantErrs:     []error{nil, errClientGone, errClientGone},
			wantStarts:   1,
			wantOverflow: true,
			wantLost:     true,
		},
		{
			name:         "client write failing mid-stream loses the client",
			maxBody:      5,
			writeErr:     errClientGone,
			writes:       []string{"1234", "5678", "90"},
			wantBuffered: "1234",
			wantFirst:    "5678",
			wantErrs:     []error{nil, nil, errClientGone},
			wantStarts:   1,
			wantOverflow: true,
			wantLost:     true,
		},
		{
			name:         "stream start without a client loses the client",
			maxBody:      5,
			startErr:     errStreamDetached,
			writes:       []string{"1234", "5678"},
			wantBuffered: "1234",
			wantFirst:    "5678",
			wantErrs:     []error{nil, errStreamDetached},
			wantStarts:   1,
			wantOverflow: true,
			wantLost:     true,
		},
		{
			name:         "client detached mid-stream loses the client",
			maxBody:      5,
			writeErr:     errStreamDetached,
			writes:       []string{"1234", "5678", "90"},
			wantBuffered: "1234",
			wantFirst:    "5678",
			wantErrs:     []error{nil, nil, errStreamDetached},
			wantStarts:   1,
			wantOverflow: true,
			wantLost:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &fakeSink{startErr: tt.startErr, writeErr: tt.writeErr}
			rec := newRecorder(tt.maxBody, sink)
			rec.Header().Set("X-Test", "1")

			for i, chunk := range tt.writes {
				n, err := rec.Write([]byte(chunk))

				if tt.wantErrs[i] != nil {
					assert.ErrorIs(t, err, tt.wantErrs[i])
					assert.Zero(t, n)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, len(chunk), n)
				}

				if i+1 == tt.flushAfter {
					rec.Flush()
				}
			}

			resp := rec.result(time.Now())

			assert.Equal(t, tt.wantStarts, sink.starts)
			assert.Equal(t, tt.wantFlushes, sink.flushes)
			assert.Equal(t, tt.wantBuffered, string(sink.buffered))
			assert.Equal(t, tt.wantFirst, string(sink.first))
			assert.Equal(t, tt.wantStreamed, sink.written.String())
			assert.Equal(t, tt.wantBody, string(resp.body))
			assert.Equal(t, tt.wantOverflow, resp.overflow)
			assert.Equal(t, tt.wantLost, rec.lostClient())
			assert.Equal(t, http.StatusOK, resp.status)

			if tt.wantStarts > 0 {
				assert.Equal(t, http.StatusOK, sink.status)
				assert.Equal(t, http.Header{"X-Test": {"1"}}, sink.header)
			}
		})
	}
}

func TestRecorder_MemoryIsBoundedByMaxBody(t *testing.T) {
	const (
		maxBody = 1 << 10
		chunk   = 100
	)

	sink := &fakeSink{}
	rec := newRecorder(maxBody, sink)
	p := bytes.Repeat([]byte("x"), chunk)

	const writes = 10 * maxBody / chunk

	for range writes {
		_, err := rec.Write(p)
		require.NoError(t, err)

		assert.LessOrEqual(t, rec.body.Len(), maxBody, "never more than maxBody is buffered")
		// bytes.Buffer at most doubles its capacity when it grows.
		assert.LessOrEqual(t, rec.body.Cap(), 2*maxBody, "the buffer must stay bounded by the limit")
	}

	assert.Equal(t, 1, sink.starts)
	assert.LessOrEqual(t, sink.startSize, maxBody)
	assert.Zero(t, rec.body.Cap(), "the buffer is released once streaming started")
	assert.Equal(t, writes*chunk, sink.startSize+len(sink.first)+sink.written.Len())
}
