package httpcache

import (
	"bytes"
	"net/http"
	"net/textproto"
	"time"
)

const (
	minStatusCode = 100
	maxStatusCode = 999
)

// response is a fully buffered handler response.
type response struct {
	createdAt time.Time
	header    http.Header
	body      []byte
	status    int
	overflow  bool
}

// recorder is a buffering http.ResponseWriter used to capture a handler response.
//
// It snapshots headers when the status is written, ignores 1xx informational
// responses, treats Flush as a no-op and does not support Hijack or Unwrap, so
// handlers cannot reach the underlying connection.
type recorder struct {
	header   http.Header
	snapshot http.Header
	body     bytes.Buffer
	maxBody  int
	status   int
	// invalidStatus is the out-of-range code the handler passed to WriteHeader,
	// valid only when hasInvalidStatus is set (the code itself may be 0).
	invalidStatus    int
	overflow         bool
	hasInvalidStatus bool
}

// newRecorder creates a recorder that marks responses larger than maxBody as overflowed.
// A maxBody of zero or less disables the limit.
func newRecorder(maxBody int) *recorder {
	return &recorder{
		header:  make(http.Header),
		maxBody: maxBody,
	}
}

// Header returns the header map that will be sent by WriteHeader.
func (r *recorder) Header() http.Header {
	return r.header
}

// WriteHeader records the response status code and snapshots the headers with canonical keys.
// Informational (1xx) codes are ignored and only the first final code is kept.
// An out-of-range code is recorded as 500 and remembered so it can be reported:
// net/http would panic on it instead.
func (r *recorder) WriteHeader(code int) {
	if r.status != 0 {
		return
	}

	if code < minStatusCode || code > maxStatusCode {
		r.invalidStatus = code
		r.hasInvalidStatus = true
		code = http.StatusInternalServerError
	}

	if code < http.StatusOK && code != http.StatusSwitchingProtocols {
		return
	}

	r.status = code
	r.snapshot = canonicalHeader(r.header)
}

// canonicalHeader returns a copy of h with every key in canonical form.
// net/http sends keys exactly as the handler wrote them, so a handler may set
// h["set-cookie"] directly; canonical keys make sure the storage policy, which
// looks headers up by their canonical names, sees every header. Values of keys
// that differ only in case are merged, canonical key first.
func canonicalHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))

	for name, values := range h {
		if textproto.CanonicalMIMEHeaderKey(name) == name {
			out[name] = append(out[name], values...)
		}
	}

	for name, values := range h {
		if canonical := textproto.CanonicalMIMEHeaderKey(name); canonical != name {
			out[canonical] = append(out[canonical], values...)
		}
	}

	return out
}

// Write buffers the response body, writing an implicit 200 status first if needed.
// It keeps buffering past the size limit so that the leader still receives the
// full response, but marks the response as overflowed.
func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}

	if !bodyAllowed(r.status) {
		return 0, http.ErrBodyNotAllowed
	}

	n, err := r.body.Write(p)

	if r.maxBody > 0 && r.body.Len() > r.maxBody {
		r.overflow = true
	}

	return n, err
}

// Flush is a no-op: the response is buffered until the handler returns.
func (r *recorder) Flush() {}

// result returns the captured response, applying an implicit 200 status when
// the handler wrote nothing.
func (r *recorder) result(now time.Time) *response {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}

	return &response{
		status:    r.status,
		header:    r.snapshot,
		body:      r.body.Bytes(),
		createdAt: now,
		overflow:  r.overflow,
	}
}

// bodyAllowed reports whether a response with the given status may have a body.
func bodyAllowed(status int) bool {
	switch {
	case status < http.StatusOK:
		return false
	case status == http.StatusNoContent, status == http.StatusNotModified:
		return false
	default:
		return true
	}
}
