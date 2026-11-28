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

// response is a recorded handler response. The body of an overflowed response
// is empty: it was streamed to the leader or discarded.
type response struct {
	createdAt time.Time
	header    http.Header
	body      []byte
	status    int
	overflow  bool
}

// streamSink receives a response that outgrew the size limit, so that it can be
// streamed to the client instead of buffered. It is implemented by *leaderSlot.
// All methods are called from the handler goroutine.
type streamSink interface {
	// startStream sends the status, the header and the bytes recorded so far,
	// followed by p. It returns errStreamDetached when there is no client to
	// stream to, or the error of writing to the client.
	startStream(status int, header http.Header, buffered, p []byte) error
	// write sends p to the client once the stream started.
	write(p []byte) (int, error)
	// flush flushes the data streamed so far to the client.
	flush() error
}

// recorder is an http.ResponseWriter used to capture a handler response.
//
// It buffers the response up to maxBody bytes. A response growing past maxBody
// is marked as overflowed and, from then on, streamed to sink instead of
// buffered, so memory stays bounded by maxBody plus the write in progress.
//
// It snapshots headers when the status is written and ignores 1xx informational
// responses. Flush is a no-op until the response is streamed. Hijack and Unwrap
// are not supported, so handlers cannot reach the underlying connection.
type recorder struct {
	sink     streamSink
	header   http.Header
	snapshot http.Header
	// streamErr is the error that prevented streaming the overflowed response;
	// further writes are discarded and fail with it.
	streamErr error
	body      bytes.Buffer
	maxBody   int
	status    int
	// invalidStatus is the out-of-range code the handler passed to WriteHeader,
	// valid only when hasInvalidStatus is set (the code itself may be 0).
	invalidStatus    int
	overflow         bool
	streaming        bool
	hasInvalidStatus bool
}

// newRecorder creates a recorder that buffers responses up to maxBody bytes and
// streams larger ones to sink, marking them as overflowed. A maxBody of zero or
// less disables the limit. A nil sink discards overflowed responses.
func newRecorder(maxBody int, sink streamSink) *recorder {
	return &recorder{
		header:  make(http.Header),
		maxBody: maxBody,
		sink:    sink,
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

// Write records the response body, writing an implicit 200 status first if needed.
//
// The body is buffered while it fits maxBody. The write that makes it outgrow
// maxBody marks the response as overflowed, starts streaming it to the sink and
// releases the buffer; later writes go straight to the sink. When streaming is
// not possible (no client to stream to, or the client write failed) the error
// is returned and later writes are discarded with the same error, so the
// handler can stop early. An overflowed response is never stored or shared.
func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}

	if !bodyAllowed(r.status) {
		return 0, http.ErrBodyNotAllowed
	}

	switch {
	case r.streaming:
		return r.sink.write(p)
	case r.streamErr != nil:
		return 0, r.streamErr
	case r.maxBody > 0 && r.body.Len()+len(p) > r.maxBody:
		return r.startStream(p)
	default:
		return r.body.Write(p)
	}
}

// startStream marks the response as overflowed and hands it to the sink,
// releasing the buffered body.
func (r *recorder) startStream(p []byte) (int, error) {
	r.overflow = true

	err := errStreamDetached
	if r.sink != nil {
		err = r.sink.startStream(r.status, r.snapshot, r.body.Bytes(), p)
	}

	r.body = bytes.Buffer{}

	if err != nil {
		r.streamErr = err
		return 0, err
	}

	r.streaming = true

	return len(p), nil
}

// Flush flushes the streamed response to the client. It is a no-op while the
// response is buffered, since nothing was sent yet.
func (r *recorder) Flush() {
	if r.streaming {
		// Flush has no way to report the error; the next Write reports a broken client.
		_ = r.sink.flush()
	}
}

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
