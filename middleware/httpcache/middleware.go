package httpcache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/ksysoev/anycache"
)

// Values reported in the cache status header (see WithCacheStatusHeader).
const (
	// StatusHit reports a response served from the cache.
	StatusHit = "HIT"
	// StatusMiss reports a response from the handler that was stored or shared
	// with concurrent requests.
	StatusMiss = "MISS"
	// StatusWarmUp reports a response served from the cache while it is refreshed
	// in the background.
	StatusWarmUp = "WARM_UP"
	// StatusBypass reports a response that was neither stored nor shared: the
	// request was not cacheable, the response was private, or storage could not be read.
	StatusBypass = "BYPASS"
)

var (
	// ErrInvalidTTL is returned by New when the TTL is not positive.
	ErrInvalidTTL = errors.New("httpcache: ttl must be greater than zero")

	errRequestReleased = errors.New("httpcache: request released before the handler ran")
	errStreamDetached  = errors.New("httpcache: response writer detached")
)

// Cache is the subset of *anycache.Cache used by the middleware.
type Cache interface {
	// CacheC returns the cached value for key or generates it, storing it only
	// when the generator reports it as cacheable. Concurrent calls for the same
	// key share a single generator execution.
	CacheC(ctx context.Context, key string, ttl time.Duration, gen anycache.CacheGeneratorC, opts ...anycache.CacheItemOptions) ([]byte, error)
	// Invalidate removes key from the cache.
	Invalidate(ctx context.Context, key string) error
}

// Middleware caches responses of wrapped HTTP handlers.
type Middleware struct {
	cache    Cache
	baseOpts []anycache.CacheItemOptions
	cfg      config
	ttl      time.Duration
}

// New creates a caching middleware storing responses in cache for ttl.
// It returns ErrInvalidTTL when ttl is not positive.
func New(cache Cache, ttl time.Duration, opts ...Option) (*Middleware, error) {
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	baseOpts := make([]anycache.CacheItemOptions, 0, len(cfg.cacheOpts)+1)
	if cfg.timeout > 0 {
		baseOpts = append(baseOpts, anycache.WithTimeout(cfg.timeout))
	}

	baseOpts = append(baseOpts, cfg.cacheOpts...)

	return &Middleware{
		cache:    cache,
		ttl:      ttl,
		cfg:      cfg,
		baseOpts: baseOpts,
	}, nil
}

// Handler wraps next with response caching.
//
// Concurrent identical requests run next once and share its response, unless
// the response is private (Set-Cookie, Cache-Control private/no-store/no-cache,
// an unsafe Vary or an oversized body), in which case other requests run next
// themselves. A response larger than WithMaxBodySize is streamed to the request
// that ran next once it outgrows the limit, and is neither stored nor shared.
// The handler runs on a copy of the request that has no body and no
// conditional headers, with the request context values but without its
// cancellation, so SSE and websockets are not supported.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.serve(w, r, next)
	})
}

func (m *Middleware) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	key, ok := m.Key(r)
	if !ok {
		m.passThrough(w, r, next)
		return
	}

	var state anycache.State

	src := &requestSource{orig: r}
	defer func() { src.release(mayRunLater(state)) }()

	slot := &leaderSlot{w: w, statusHeader: m.cfg.statusHeader}
	gen := m.generator(src, next, slot, key)

	data, err := m.cache.CacheC(r.Context(), key, m.ttlFor(r), gen, m.cacheOptions(&state)...)

	// Detach before touching w: the generator may still run (warm-up, timeout,
	// leader cancellation) and must never write to w after this point. A
	// warm-up run that already started streaming to w keeps it until it ends.
	streamed, complete := slot.detach(r.Context())
	if streamed {
		// The response already started on w. Unless it completed (panic, abort,
		// timeout, cancellation), abort the connection so the client cannot take
		// the partial body for a complete one.
		if err != nil || !complete {
			panic(http.ErrAbortHandler)
		}

		return
	}

	if err != nil {
		m.handleError(w, r, next, slot, key, err)
		return
	}

	// The pass marker only ever comes from a generator run, never from storage:
	// on a hit it is corrupt data, which writeEntry removes.
	if state == anycache.CacheMiss && isPassMarker(data) {
		// Only the leader holds the private response; followers run the handler themselves.
		// An overflowed response that was not streamed has no body to write.
		if resp := slot.get(); resp != nil && !resp.overflow {
			m.writeRecorded(w, resp, StatusBypass)
			return
		}

		m.passThrough(w, r, next)

		return
	}

	m.writeEntry(w, r, next, key, data, state)
}

// generator returns the cache generator running next on a detached copy of the request.
func (m *Middleware) generator(src *requestSource, next http.Handler, slot *leaderSlot, key string) anycache.CacheGeneratorC {
	return func(genCtx context.Context) (_ []byte, _ bool, err error) {
		base := src.request()
		if base == nil {
			return nil, false, errRequestReleased
		}

		defer func() { slot.finish(err) }()

		ctx, cancel := detach(base.Context(), genCtx)
		defer cancel()

		resp, err := m.execute(ctx, next, base.Clone(ctx), key, slot)
		if err != nil {
			return nil, false, err
		}

		slot.set(resp)

		switch m.cfg.classify(resp) {
		case outcomePass:
			return passMarker, false, nil
		case outcomeShare:
			return encodeEntry(resp), false, nil
		default:
			return encodeEntry(resp), true, nil
		}
	}
}

// execute runs next on r and records its response, streaming it to sink once it
// outgrows the configured maximum body size.
//
// When ctx ends before the handler returns, execute stops waiting and returns a
// timeout error at once; the handler goroutine runs on until it returns.
//
// Panics, aborts and handler overruns are returned as errors and logged here.
// This deliberately both logs and returns the error: execute is the only place
// that runs exactly once per handler execution, every request waiting for the
// response receives the same error, and the request that triggered the
// execution may already be gone. For a WithWarmUpTTL refresh, anycache also
// logs the returned error itself.
func (m *Middleware) execute(ctx context.Context, next http.Handler, r *http.Request, key string, sink streamSink) (*response, error) {
	rec := newRecorder(m.cfg.maxBodySize, sink)

	// The handler runs in its own goroutine so that a handler ignoring its context
	// cannot hold the waiting requests past the timeout.
	done := make(chan *panicError, 1)

	go func() { done <- runHandler(next, rec, r) }()

	var pe *panicError

	select {
	case pe = <-done:
	case <-ctx.Done():
		// The handler keeps running in the background until it returns. Only its
		// goroutine still touches rec, and its output is discarded: a streamed
		// response is aborted and the leader's writer detached.
		te := newTimeoutError(context.Cause(ctx))
		m.cfg.logger.Warn("httpcache: handler did not finish in time", "key", key, "error", te.cause)

		return nil, te
	}

	return m.finish(ctx, rec, pe, key)
}

// finish turns the outcome of a handler execution into its result. A context
// that ended takes precedence over a panic or abort, which it may have caused.
func (m *Middleware) finish(ctx context.Context, rec *recorder, pe *panicError, key string) (*response, error) {
	switch {
	case ctx.Err() != nil:
		// A handler that outlived its context may have written a partial or error
		// response, or panicked or aborted because of the cancellation (for example
		// httputil.ReverseProxy when its upstream copy is cancelled): never serve,
		// share or store it, and report a timeout.
		te := newTimeoutError(context.Cause(ctx))

		attrs := []any{"key", key, "error", te.cause}
		if pe != nil {
			attrs = append(attrs, "panic", pe.value)
		}

		m.cfg.logger.Warn("httpcache: handler did not finish in time", attrs...)

		return nil, te
	case pe != nil && pe.value == http.ErrAbortHandler:
		// The handler aborted its response, for example httputil.ReverseProxy when
		// copying the upstream body fails. The waiting requests get a clean 502
		// instead, unless the response was already streamed: then the leader's
		// connection is aborted.
		m.cfg.logger.Warn("httpcache: handler aborted the response", "key", key)
		return nil, newAbortError()
	case pe != nil:
		m.cfg.logger.Error("httpcache: handler panicked", "key", key, "error", pe.value, "stack", string(pe.stack))
		return nil, pe
	default:
		if rec.hasInvalidStatus {
			m.cfg.logger.Warn("httpcache: handler wrote an invalid status code, responding 500", "key", key, "status", rec.invalidStatus)
		}

		return rec.result(time.Now()), nil
	}
}

// handleError responds when the cache call failed.
func (m *Middleware) handleError(w http.ResponseWriter, r *http.Request, next http.Handler, slot *leaderSlot, key string, err error) {
	var (
		pe *panicError
		ae *abortError
		te *timeoutError
	)

	switch {
	case errors.As(err, &pe):
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	case errors.As(err, &ae):
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	case errors.As(err, &te):
		code := http.StatusServiceUnavailable
		if errors.Is(te.cause, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		}

		http.Error(w, http.StatusText(code), code)
	case r.Context().Err() != nil:
		// The client is gone, nothing to respond to.
	default:
		if resp := slot.get(); resp != nil && !resp.overflow {
			// Safety net: CacheC does not report storage write failures, but should
			// the cache fail after the handler ran, never run it twice.
			m.cfg.logger.Warn("httpcache: cache error after the handler ran", "key", key, "error", err)
			m.writeRecorded(w, resp, StatusMiss)

			return
		}

		m.cfg.logger.Warn("httpcache: cache unavailable, bypassing", "key", key, "error", err)
		m.passThrough(w, r, next)
	}
}

// writeEntry writes an encoded entry, failing open when it cannot be decoded.
func (m *Middleware) writeEntry(w http.ResponseWriter, r *http.Request, next http.Handler, key string, data []byte, state anycache.State) {
	e, err := decodeEntry(data)
	if err != nil {
		m.cfg.logger.Warn("httpcache: invalid cache entry, bypassing", "key", key, "error", err)

		// An entry in an unsupported format may be valid for a newer version of
		// the middleware (for example during a rolling deploy), so it is kept.
		if errors.Is(err, ErrCorruptEntry) {
			m.invalidate(r, key)
		}

		m.passThrough(w, r, next)

		return
	}

	h := w.Header()
	for name, values := range e.header {
		h[name] = values
	}

	status := statusFor(state)
	if status == StatusHit || status == StatusWarmUp {
		h.Set("Age", strconv.Itoa(age(e.createdAt)))
	}

	m.setStatus(w, status)
	m.writeBody(w, e.status, e.body)
}

// invalidate removes the entry stored under key. It is not cancelled when the
// client disconnects but is bounded by the configured timeout.
func (m *Middleware) invalidate(r *http.Request, key string) {
	ctx := context.WithoutCancel(r.Context())

	if m.cfg.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, m.cfg.timeout)
		defer cancel()
	}

	if err := m.cache.Invalidate(ctx, key); err != nil {
		m.cfg.logger.Warn("httpcache: failed to invalidate invalid entry", "key", key, "error", err)
	}
}

// writeRecorded writes the response recorded for this very request, reporting
// status in the cache status header: BYPASS for a private response that was
// neither stored nor shared, MISS otherwise.
// Trailers are dropped: their values were never recorded.
func (m *Middleware) writeRecorded(w http.ResponseWriter, resp *response, status string) {
	h := w.Header()

	for name, values := range resp.header {
		if isTrailer(name) {
			continue
		}

		h[name] = values
	}

	m.setStatus(w, status)
	m.writeBody(w, resp.status, resp.body)
}

func (m *Middleware) writeBody(w http.ResponseWriter, status int, body []byte) {
	if !bodyAllowed(status) {
		w.WriteHeader(status)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)

	// A write error means the client went away; there is nobody to report it to.
	_, _ = w.Write(body)
}

func (m *Middleware) passThrough(w http.ResponseWriter, r *http.Request, next http.Handler) {
	m.setStatus(w, StatusBypass)
	next.ServeHTTP(w, r)
}

func (m *Middleware) setStatus(w http.ResponseWriter, status string) {
	if m.cfg.statusHeader != "" {
		w.Header().Set(m.cfg.statusHeader, status)
	}
}

func (m *Middleware) ttlFor(r *http.Request) time.Duration {
	if m.cfg.ttlFunc != nil {
		if ttl := m.cfg.ttlFunc(r); ttl > 0 {
			return ttl
		}
	}

	return m.ttl
}

// cacheOptions returns the per-request cache options, capturing the cache state into state.
func (m *Middleware) cacheOptions(state *anycache.State) []anycache.CacheItemOptions {
	opts := make([]anycache.CacheItemOptions, 0, len(m.baseOpts)+1)
	opts = append(opts, m.baseOpts...)

	return append(opts, captureState(state))
}

// captureState records the cache state while keeping the configured metric hook.
// The hook runs in the caller goroutine before CacheC returns.
func captureState(state *anycache.State) anycache.CacheItemOptions {
	return func(req *anycache.Request) {
		prev := req.MetricHook
		req.MetricHook = func(key string, op anycache.State, latency time.Duration) {
			*state = op

			if prev != nil {
				prev(key, op, latency)
			}
		}
	}
}

// mayRunLater reports whether the generator of a request that ended in state may
// still run after the request completed: as a background warm-up, or after a
// failure such as the client disconnecting while the shared execution goes on.
// After a hit or a miss the generator either already ran or never will, so the
// request copy it would need is not prepared.
func mayRunLater(state anycache.State) bool {
	return state != anycache.CacheHit && state != anycache.CacheMiss
}

func statusFor(state anycache.State) string {
	switch state {
	case anycache.CacheHit:
		return StatusHit
	case anycache.CacheWarmUp:
		return StatusWarmUp
	default:
		return StatusMiss
	}
}

func age(createdAt time.Time) int {
	return max(0, int(time.Since(createdAt)/time.Second))
}

// runHandler runs next, returning a *panicError if it panicked and nil otherwise.
func runHandler(next http.Handler, w http.ResponseWriter, r *http.Request) (pe *panicError) {
	defer func() {
		if v := recover(); v != nil {
			pe = newPanicError(v, debug.Stack())
		}
	}()

	next.ServeHTTP(w, r)

	return nil
}

// panicError carries a recovered handler panic through the cache.
type panicError struct {
	value any
	stack []byte
}

// newPanicError creates a panicError for a recovered value and its stack trace.
func newPanicError(value any, stack []byte) *panicError {
	return &panicError{value: value, stack: stack}
}

func (e *panicError) Error() string {
	return fmt.Sprintf("httpcache: handler panicked: %v", e.value)
}

// abortError reports that the handler aborted its response with http.ErrAbortHandler.
type abortError struct{}

// newAbortError creates an abortError.
func newAbortError() *abortError {
	return &abortError{}
}

func (e *abortError) Error() string {
	return "httpcache: handler aborted the response"
}

// timeoutError reports that the handler context ended before the handler returned.
type timeoutError struct {
	cause error
}

// newTimeoutError creates a timeoutError for the cause of the handler context cancellation.
func newTimeoutError(cause error) *timeoutError {
	return &timeoutError{cause: cause}
}

func (e *timeoutError) Error() string {
	return fmt.Sprintf("httpcache: handler did not finish in time: %v", e.cause)
}

func (e *timeoutError) Unwrap() error {
	return e.cause
}

// requestSource lazily prepares the request copy used by the generator.
// The copy must be made while the original request is still valid, that is
// before the middleware returns from ServeHTTP; this avoids cloning on hits.
type requestSource struct {
	orig     *http.Request
	prepared *http.Request
	mu       sync.Mutex
}

// request returns the prepared copy, creating it if the original is still available.
func (s *requestSource) request() *http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.prepared == nil && s.orig != nil {
		s.prepared = prepareRequest(s.orig)
	}

	return s.prepared
}

// release drops the original request. When keep is true the copy is prepared
// first, so a generator running later can still use it.
func (s *requestSource) release(keep bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if keep && s.prepared == nil {
		s.prepared = prepareRequest(s.orig)
	}

	s.orig = nil
}

// leaderSlot connects this request's own generator, if it runs, to the request:
// it holds the recorded response and streams an oversized response to the
// request's writer.
//
// The generator and the handler run in other goroutines, hence the mutex. Every
// write to w happens under the mutex and is refused once detach ran, so nothing
// is written to w after the request's ServeHTTP returned.
type leaderSlot struct {
	// w is the request's writer; nil once detached.
	w    http.ResponseWriter
	resp *response
	// genErr is the error the generator returned, valid once finished is set.
	genErr error
	// streamEnd is created when a stream starts and closed when the generator
	// returns, so the request can wait for a stream in progress. It is only
	// allocated for streamed responses, keeping the hit path allocation-free.
	streamEnd    chan struct{}
	statusHeader string
	mu           sync.Mutex
	detached     bool
	streaming    bool
	finished     bool
}

func (s *leaderSlot) set(resp *response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resp = resp
}

func (s *leaderSlot) get() *response {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.resp
}

// finish records that the generator returned with err, ending a stream in progress.
func (s *leaderSlot) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.finished = true
	s.genErr = err

	if s.streamEnd != nil {
		close(s.streamEnd)
	}
}

// detach stops all further writes to the request's writer, waiting for a write
// in progress. When a stream started but the generator has not returned yet (a
// warm-up run that started streaming before the request got its cached
// response), it first waits for the generator to return or for ctx to end, since
// the response on the writer can no longer be replaced.
//
// It reports whether a response was streamed to the writer and whether that
// response is complete: the generator returned without error.
func (s *leaderSlot) detach(ctx context.Context) (streamed, complete bool) {
	s.mu.Lock()

	if s.streaming && !s.finished {
		end := s.streamEnd
		s.mu.Unlock()

		select {
		case <-end:
		case <-ctx.Done():
		}

		s.mu.Lock()
	}

	defer s.mu.Unlock()

	s.detached = true
	s.w = nil

	return s.streaming, s.finished && s.genErr == nil
}

// startStream writes the status, the header (without trailers, whose values were
// never recorded) and the response body so far to the request's writer, reporting
// it as BYPASS. It returns errStreamDetached once the request no longer waits for
// the response.
func (s *leaderSlot) startStream(status int, header http.Header, buffered, p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.detached {
		return errStreamDetached
	}

	h := s.w.Header()

	for name, values := range header {
		if isTrailer(name) {
			continue
		}

		h[name] = values
	}

	if s.statusHeader != "" {
		h.Set(s.statusHeader, StatusBypass)
	}

	s.w.WriteHeader(status)
	s.streaming = true
	s.streamEnd = make(chan struct{})

	if _, err := s.w.Write(buffered); err != nil {
		return fmt.Errorf("httpcache: stream response: %w", err)
	}

	if _, err := s.w.Write(p); err != nil {
		return fmt.Errorf("httpcache: stream response: %w", err)
	}

	return nil
}

// write writes p to the request's writer, or returns errStreamDetached.
func (s *leaderSlot) write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.detached {
		return 0, errStreamDetached
	}

	return s.w.Write(p)
}

// flush flushes the request's writer, or returns errStreamDetached.
func (s *leaderSlot) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.detached {
		return errStreamDetached
	}

	if err := http.NewResponseController(s.w).Flush(); err != nil {
		return fmt.Errorf("httpcache: flush response: %w", err)
	}

	return nil
}
