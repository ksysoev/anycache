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
	StatusHit    = "HIT"
	StatusMiss   = "MISS"
	StatusWarmUp = "WARM_UP"
	StatusBypass = "BYPASS"
)

var (
	// ErrInvalidTTL is returned by New when the TTL is not positive.
	ErrInvalidTTL = errors.New("httpcache: ttl must be greater than zero")

	errRequestReleased = errors.New("httpcache: request released before the handler ran")
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
// themselves. The handler runs on a copy of the request that has no body and no
// conditional headers, with the request context values but without its
// cancellation, so streaming (SSE, websockets) is not supported.
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
	// A plain hit never runs the generator later; anything else (miss, warm-up,
	// error) may still run it after this request completes.
	defer func() { src.release(state != anycache.CacheHit) }()

	slot := &leaderSlot{}
	gen := m.generator(src, next, slot, key)

	data, err := m.cache.CacheC(r.Context(), key, m.ttlFor(r), gen, m.cacheOptions(&state)...)
	if err != nil {
		m.handleError(w, r, next, slot, key, err)
		return
	}

	if isPassMarker(data) {
		// Only the leader holds the private response; followers run the handler themselves.
		if resp := slot.get(); resp != nil {
			m.writeRecorded(w, resp)
			return
		}

		m.passThrough(w, r, next)

		return
	}

	m.writeEntry(w, r, next, key, data, state)
}

// generator returns the cache generator running next on a detached copy of the request.
func (m *Middleware) generator(src *requestSource, next http.Handler, slot *leaderSlot, key string) anycache.CacheGeneratorC {
	return func(genCtx context.Context) ([]byte, bool, error) {
		base := src.request()
		if base == nil {
			return nil, false, errRequestReleased
		}

		ctx, cancel := detach(base.Context(), genCtx)
		defer cancel()

		resp, err := m.execute(ctx, next, base.Clone(ctx), key)
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

// execute runs next on r and records its response.
//
// Panics and handler overruns are returned as errors and logged here, exactly
// once per handler execution: every request waiting for the response receives
// the same error, and the request that triggered the execution may be gone.
func (m *Middleware) execute(ctx context.Context, next http.Handler, r *http.Request, key string) (*response, error) {
	rec := newRecorder(m.cfg.maxBodySize)
	pe := runHandler(next, rec, r)

	switch {
	case pe != nil && pe.value == http.ErrAbortHandler:
		// Deliberate abort, handled like net/http does: not logged, re-panicked by every waiting request.
		return nil, pe
	case ctx.Err() != nil:
		// A handler that outlived its context may have written a partial or error
		// response, or panicked because of the cancellation: never serve, share or store it.
		te := newTimeoutError(context.Cause(ctx))

		attrs := []any{"key", key, "error", te.cause}
		if pe != nil {
			attrs = append(attrs, "panic", pe.value)
		}

		m.cfg.logger.Warn("httpcache: handler did not finish in time", attrs...)

		return nil, te
	case pe != nil:
		m.cfg.logger.Error("httpcache: handler panicked", "key", key, "error", pe.value, "stack", string(pe.stack))
		return nil, pe
	default:
		return rec.result(time.Now()), nil
	}
}

// handleError responds when the cache call failed.
func (m *Middleware) handleError(w http.ResponseWriter, r *http.Request, next http.Handler, slot *leaderSlot, key string, err error) {
	var (
		pe *panicError
		te *timeoutError
	)

	switch {
	case errors.As(err, &pe):
		// An abort is shared on purpose: it signals a broken response (for example a
		// failed upstream body copy in httputil.ReverseProxy) that every follower would get.
		if pe.value == http.ErrAbortHandler {
			panic(http.ErrAbortHandler)
		}

		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	case errors.As(err, &te):
		code := http.StatusServiceUnavailable
		if errors.Is(te.cause, context.DeadlineExceeded) {
			code = http.StatusGatewayTimeout
		}

		http.Error(w, http.StatusText(code), code)
	case r.Context().Err() != nil:
		// The client is gone, nothing to respond to.
	default:
		if resp := slot.get(); resp != nil {
			// Safety net: CacheC does not report storage write failures, but should
			// the cache fail after the handler ran, never run it twice.
			m.cfg.logger.Warn("httpcache: cache error after the handler ran", "key", key, "error", err)
			m.writeRecorded(w, resp)

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

		if err := m.cache.Invalidate(context.WithoutCancel(r.Context()), key); err != nil {
			m.cfg.logger.Warn("httpcache: failed to invalidate invalid entry", "key", key, "error", err)
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

// writeRecorded writes the response recorded for this very request.
func (m *Middleware) writeRecorded(w http.ResponseWriter, resp *response) {
	h := w.Header()
	for name, values := range resp.header {
		h[name] = values
	}

	m.setStatus(w, StatusMiss)
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

// leaderSlot holds the response recorded by this request's own generator, if it ran.
// It is written from the cache worker goroutine, hence the mutex.
type leaderSlot struct {
	resp *response
	mu   sync.Mutex
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
