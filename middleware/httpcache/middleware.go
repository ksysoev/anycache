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
	gen := m.generator(src, next, slot)

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
func (m *Middleware) generator(src *requestSource, next http.Handler, slot *leaderSlot) anycache.CacheGeneratorC {
	return func(genCtx context.Context) ([]byte, bool, error) {
		base := src.request()
		if base == nil {
			return nil, false, errRequestReleased
		}

		ctx, cancel := detach(base.Context(), genCtx)
		defer cancel()

		rec := newRecorder(m.cfg.maxBodySize)

		if err := runHandler(next, rec, base.Clone(ctx)); err != nil {
			return nil, false, err
		}

		resp := rec.result(time.Now())
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

// handleError responds when the cache call failed.
func (m *Middleware) handleError(w http.ResponseWriter, r *http.Request, next http.Handler, slot *leaderSlot, key string, err error) {
	var pe *panicError

	switch {
	case errors.As(err, &pe):
		if pe.value == http.ErrAbortHandler {
			panic(http.ErrAbortHandler)
		}

		m.cfg.logger.Error("httpcache: handler panicked", "key", key, "error", pe.value, "stack", string(pe.stack))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	case r.Context().Err() != nil:
		// The client is gone, nothing to respond to.
	default:
		if resp := slot.get(); resp != nil {
			// The handler already ran for this request (e.g. storing failed), never run it twice.
			m.cfg.logger.Warn("httpcache: failed to store response", "key", key, "error", err)
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

// runHandler runs next, converting a panic into a *panicError.
func runHandler(next http.Handler, w http.ResponseWriter, r *http.Request) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &panicError{value: v, stack: debug.Stack()}
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

func (e *panicError) Error() string {
	return fmt.Sprintf("httpcache: handler panicked: %v", e.value)
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
