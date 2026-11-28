package httpcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	"github.com/ksysoev/anycache/storage/inmemory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const statusHeader = "X-Cache"

var errStorage = errors.New("storage down")

func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMemoryCache(tb testing.TB) *anycache.Cache {
	tb.Helper()

	store, err := inmemory.New(100)
	require.NoError(tb, err)

	cache := anycache.New(store)

	tb.Cleanup(func() {
		assert.NoError(tb, cache.Close())
		assert.NoError(tb, store.Close())
	})

	return cache
}

func newHandler(t *testing.T, cache Cache, next http.Handler, opts ...Option) http.Handler {
	t.Helper()

	opts = append([]Option{WithCacheStatusHeader(statusHeader), WithLogger(quietLogger())}, opts...)

	mw, err := New(cache, time.Minute, opts...)
	require.NoError(t, err)

	return mw.Handler(next)
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	return w
}

func get(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, http.NoBody)
}

// countingHandler responds with "<body>-<call number>".
type countingHandler struct {
	header http.Header
	body   string
	calls  atomic.Int32
	status int
}

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n := h.calls.Add(1)

	for k, v := range h.header {
		w.Header()[k] = v
	}

	if h.status != 0 {
		w.WriteHeader(h.status)
	}

	_, _ = fmt.Fprintf(w, "%s-%d", h.body, n)
}

func TestNew_InvalidTTL(t *testing.T) {
	_, err := New(nil, 0)
	assert.ErrorIs(t, err, ErrInvalidTTL)
}

func TestMiddleware_CachingBehaviour(t *testing.T) {
	tests := []struct {
		req           func() *http.Request
		handler       *countingHandler
		name          string
		wantStatus2   string
		opts          []Option
		wantCalls     int32
		wantSameBody  bool
		wantHTTPCode2 int
	}{
		{
			name:          "200 is cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "ok"},
			wantCalls:     1,
			wantSameBody:  true,
			wantStatus2:   StatusHit,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "500 is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "err", status: http.StatusInternalServerError},
			wantCalls:     2,
			wantStatus2:   StatusMiss,
			wantHTTPCode2: http.StatusInternalServerError,
		},
		{
			name:          "configured 404 is cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "nf", status: http.StatusNotFound},
			opts:          []Option{WithCacheableStatus(http.StatusOK, http.StatusNotFound)},
			wantCalls:     1,
			wantSameBody:  true,
			wantStatus2:   StatusHit,
			wantHTTPCode2: http.StatusNotFound,
		},
		{
			name:          "set-cookie is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"Set-Cookie": {"s=1"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "no-store is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"Cache-Control": {"no-store"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "unsafe vary is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "v", header: http.Header{"Vary": {"Accept-Language"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "lowercase set-cookie is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"set-cookie": {"s=1"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "lowercase private is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"cache-control": {"private"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "lowercase unsafe vary is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "v", header: http.Header{"vary": {"Cookie"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "oversized body is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "a-very-long-body"},
			opts:          []Option{WithMaxBodySize(4)},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name: "POST bypasses",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/p", http.NoBody)
			},
			handler:       &countingHandler{body: "post"},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name: "authorized request bypasses",
			req: func() *http.Request {
				r := get("/p")
				r.Header.Set("Authorization", "Bearer x")

				return r
			},
			handler:       &countingHandler{body: "auth"},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(t, newMemoryCache(t), tt.handler, tt.opts...)

			first := do(h, tt.req())
			second := do(h, tt.req())

			assert.Equal(t, tt.wantCalls, tt.handler.calls.Load())
			assert.Equal(t, tt.wantHTTPCode2, second.Code)
			assert.Equal(t, tt.wantStatus2, second.Header().Get(statusHeader))

			if tt.wantSameBody {
				assert.Equal(t, first.Body.String(), second.Body.String())
			} else {
				assert.NotEqual(t, first.Body.String(), second.Body.String())
			}
		})
	}
}

func TestMiddleware_HitHeaders(t *testing.T) {
	store := anycache.NewMockCacheStorage(t)

	data := encodeEntry(&response{
		status:    http.StatusOK,
		header:    http.Header{"Content-Type": {"text/plain"}},
		body:      []byte("cached"),
		createdAt: time.Now().Add(-10 * time.Second),
	})

	store.EXPECT().Get(mock.Anything, mock.Anything).Return(data, nil).Once()

	h := newHandler(t, anycache.New(store), http.NotFoundHandler())

	w := do(h, get("/p"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "cached", w.Body.String())
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.Equal(t, "6", w.Header().Get("Content-Length"))
	assert.Equal(t, StatusHit, w.Header().Get(statusHeader))
	assert.Equal(t, "10", w.Header().Get("Age"))
}

func TestMiddleware_MissHeaders(t *testing.T) {
	h := newHandler(t, newMemoryCache(t), &countingHandler{body: "x"})

	w := do(h, get("/p"))

	assert.Equal(t, StatusMiss, w.Header().Get(statusHeader))
	assert.Empty(t, w.Header().Get("Age"))
}

func TestMiddleware_FailOpen(t *testing.T) {
	tests := []struct {
		setup      func(store *anycache.MockCacheStorage)
		name       string
		wantStatus string
		opts       []Option
	}{
		{
			name: "get fails",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, errStorage).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			name: "set fails",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, anycache.ErrKeyNotExists).Once()
				store.EXPECT().Set(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errStorage).Once()
			},
			wantStatus: StatusMiss,
		},
		{
			name: "corrupt entry is invalidated",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return([]byte{entryVersion}, nil).Once()
				store.EXPECT().Del(mock.Anything, mock.Anything).Return(nil).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			name: "stored pass marker is invalidated",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return(passMarker, nil).Once()
				store.EXPECT().Del(mock.Anything, mock.Anything).Return(nil).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			// A newer middleware version may have written it, so it must not be deleted.
			name: "unsupported version is kept",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return([]byte{entryVersion + 1, 1, 2}, nil).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			name: "hanging invalidate is bounded by the timeout",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return([]byte{entryVersion}, nil).Once()
				store.EXPECT().Del(mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, _ string) error {
						<-ctx.Done()
						return ctx.Err()
					}).Once()
			},
			opts:       []Option{WithTimeout(50 * time.Millisecond)},
			wantStatus: StatusBypass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := anycache.NewMockCacheStorage(t)
			tt.setup(store)

			handler := &countingHandler{body: "fresh"}
			h := newHandler(t, anycache.New(store), handler, tt.opts...)

			w := do(h, get("/p"))

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "fresh-1", w.Body.String())
			assert.Equal(t, tt.wantStatus, w.Header().Get(statusHeader))
			assert.Equal(t, int32(1), handler.calls.Load(), "handler must run exactly once")
		})
	}
}

func TestMiddleware_Panics(t *testing.T) {
	t.Run("panic becomes 500", func(t *testing.T) {
		h := newHandler(t, newMemoryCache(t), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}))

		w := do(h, get("/p"))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("ErrAbortHandler becomes 502", func(t *testing.T) {
		h := newHandler(t, newMemoryCache(t), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("partial"))

			panic(http.ErrAbortHandler)
		}))

		var w *httptest.ResponseRecorder

		assert.NotPanics(t, func() { w = do(h, get("/p")) })
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.NotContains(t, w.Body.String(), "partial")
	})
}

func TestMiddleware_HandlerRequest(t *testing.T) {
	var (
		gotValue    any
		gotINM      string
		gotPath     string
		gotDeadline bool
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotValue = r.Context().Value(ctxKey{})
		gotINM = r.Header.Get("If-None-Match")
		gotPath = r.PathValue("id")
		_, gotDeadline = r.Context().Deadline()

		_, _ = w.Write([]byte("ok"))
	})

	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", newHandler(t, newMemoryCache(t), handler))

	r := get("/items/42")
	r.Header.Set("If-None-Match", `"v1"`)
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, "request-value"))

	w := do(mux, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "request-value", gotValue)
	assert.Empty(t, gotINM)
	assert.Equal(t, "42", gotPath)
	assert.True(t, gotDeadline, "default timeout must bound the handler")
}

// blockingHandler blocks every call until release is closed.
type blockingHandler struct {
	started   chan struct{}
	release   chan struct{}
	setCookie bool
	calls     atomic.Int32
}

func newBlockingHandler(setCookie bool) *blockingHandler {
	return &blockingHandler{
		started:   make(chan struct{}, 100),
		release:   make(chan struct{}),
		setCookie: setCookie,
	}
}

func (h *blockingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := h.calls.Add(1)
	h.started <- struct{}{}

	<-h.release

	// touch the request after the leader may have gone away
	_ = r.Header.Get("Accept")
	_ = r.URL.String()

	if h.setCookie {
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    fmt.Sprintf("user-%d", n),
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}

	_, _ = fmt.Fprintf(w, "body-%d", n)
}

// fetched is a client-side view of a response.
type fetched struct {
	body    string
	cookies []*http.Cookie
	status  int
}

// runConcurrent starts a leader, waits for it to enter the handler, then starts
// followers and releases the handler once they are waiting.
func runConcurrent(t *testing.T, h http.Handler, bh *blockingHandler, followers int) []*fetched {
	t.Helper()

	gate := &entryGate{}

	srv := httptest.NewServer(gate.wrap(h))
	t.Cleanup(srv.Close)

	results := make([]*fetched, followers+1)

	var wg sync.WaitGroup

	fetch := func(i int) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/p", http.NoBody)
		if !assert.NoError(t, err) {
			return
		}

		resp, err := srv.Client().Do(req)
		if !assert.NoError(t, err) {
			return
		}

		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		assert.NoError(t, err)

		results[i] = &fetched{body: string(body), cookies: resp.Cookies(), status: resp.StatusCode}
	}

	wg.Go(func() { fetch(0) })

	<-bh.started

	for i := 1; i <= followers; i++ {
		wg.Go(func() { fetch(i) })
	}

	gate.wait(t, followers+1)
	close(bh.release)
	wg.Wait()

	return results
}

func TestMiddleware_ConcurrentRequestsRunHandlerOnce(t *testing.T) {
	bh := newBlockingHandler(false)
	h := newHandler(t, newMemoryCache(t), bh)

	results := runConcurrent(t, h, bh, 9)

	assert.Equal(t, int32(1), bh.calls.Load())

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, "body-1", res.body)
	}
}

func TestMiddleware_PrivateResponsesAreNotShared(t *testing.T) {
	const followers = 4

	bh := newBlockingHandler(true)
	h := newHandler(t, newMemoryCache(t), bh)

	results := runConcurrent(t, h, bh, followers)

	assert.Equal(t, int32(followers+1), bh.calls.Load(), "every request must run the handler itself")

	seen := map[string]bool{}

	for _, res := range results {
		require.NotNil(t, res)

		cookies := res.cookies
		require.Len(t, cookies, 1)

		assert.Equal(t, "user-"+res.body[len("body-"):], cookies[0].Value, "cookie must belong to this response")
		assert.False(t, seen[cookies[0].Value], "cookie %s leaked to another request", cookies[0].Value)

		seen[cookies[0].Value] = true
	}
}

// joinGrace covers the few instructions between a request entering the
// middleware and joining the in-flight cache call: singleflight exposes no
// waiter count to wait on.
const joinGrace = 10 * time.Millisecond

// entryGate counts requests entering the handler under test.
type entryGate struct {
	entered atomic.Int32
}

func (g *entryGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.entered.Add(1)
		h.ServeHTTP(w, r)
	})
}

// wait blocks until n requests entered the handler and had time to join the in-flight call.
func (g *entryGate) wait(t *testing.T, n int) {
	t.Helper()

	require.Eventually(t, func() bool { return g.entered.Load() >= int32(n) }, 5*time.Second, time.Millisecond) //nolint:gosec // small test counts
	time.Sleep(joinGrace)
}

// serveWithCancelledLeader sends a leader request and, once the handler started,
// a follower. When the follower is waiting, the leader is cancelled, then release
// is called and both responses are returned.
func serveWithCancelledLeader(t *testing.T, h http.Handler, started <-chan struct{}, release func()) (leader, follower *httptest.ResponseRecorder) {
	t.Helper()

	gate := &entryGate{}
	h = gate.wrap(h)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	leaderDone := make(chan *httptest.ResponseRecorder)
	followerDone := make(chan *httptest.ResponseRecorder)

	go func() { leaderDone <- do(h, get("/p").WithContext(ctx)) }()

	<-started

	go func() { followerDone <- do(h, get("/p")) }()

	gate.wait(t, 2)
	cancel()

	leader = <-leaderDone

	release()

	return leader, <-followerDone
}

func TestMiddleware_LeaderCancellation(t *testing.T) {
	bh := newBlockingHandler(false)
	h := newHandler(t, newMemoryCache(t), bh)

	leader, follower := serveWithCancelledLeader(t, h, bh.started, func() { close(bh.release) })

	assert.Empty(t, leader.Body.String(), "cancelled leader gets no response")
	assert.Equal(t, "body-1", follower.Body.String())
	assert.Equal(t, int32(1), bh.calls.Load())
}

func TestMiddleware_FailureLoggedWhenLeaderIsGone(t *testing.T) {
	tests := []struct {
		handler  func(started chan struct{}, release chan struct{}) http.Handler
		name     string
		wantLog  string
		opts     []Option
		wantCode int
	}{
		{
			name: "panic",
			handler: func(started, release chan struct{}) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					close(started)
					<-release
					panic("boom")
				})
			},
			wantCode: http.StatusInternalServerError,
			wantLog:  "handler panicked",
		},
		{
			name: "timeout",
			handler: func(started, _ chan struct{}) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(started)
					<-r.Context().Done()

					_, _ = w.Write([]byte("partial"))
				})
			},
			opts:     []Option{WithTimeout(500 * time.Millisecond)},
			wantCode: http.StatusGatewayTimeout,
			wantLog:  "handler did not finish in time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &syncBuffer{}
			started := make(chan struct{})
			release := make(chan struct{})

			opts := append([]Option{WithLogger(slog.New(slog.NewTextHandler(logs, nil)))}, tt.opts...)
			h := newHandler(t, newMemoryCache(t), tt.handler(started, release), opts...)

			_, follower := serveWithCancelledLeader(t, h, started, func() { close(release) })

			assert.Equal(t, tt.wantCode, follower.Code)
			assert.Equal(t, 1, strings.Count(logs.String(), tt.wantLog), "failure must be logged exactly once")
		})
	}
}

func TestMiddleware_PanicAfterDeadlineIsTimeout(t *testing.T) {
	logs := &syncBuffer{}

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		panic("nil result")
	})

	h := newHandler(t, newMemoryCache(t), handler,
		WithTimeout(50*time.Millisecond),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
	)

	w := do(h, get("/p"))

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	// The timeout is reported as soon as the context ends; the panic that follows
	// in the abandoned handler is part of the same timeout and not logged again.
	assert.Equal(t, 1, strings.Count(logs.String(), "handler did not finish in time"))
	assert.NotContains(t, logs.String(), "handler panicked")
}

func TestMiddleware_WarmUp(t *testing.T) {
	handler := &countingHandler{body: "w"}
	cache := newMemoryCache(t)
	h := newHandler(t, cache, handler, WithWarmUpTTL(time.Minute))

	first := do(h, get("/p"))
	assert.Equal(t, StatusMiss, first.Header().Get(statusHeader))

	second := do(h, get("/p"))
	assert.Equal(t, StatusWarmUp, second.Header().Get(statusHeader))
	assert.Equal(t, "w-1", second.Body.String())

	assert.Eventually(t, func() bool { return handler.calls.Load() == 2 }, time.Second, time.Millisecond)
}

func TestMiddleware_KeepsCacheMetricHook(t *testing.T) {
	store, err := inmemory.New(10)
	require.NoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	var (
		mu     sync.Mutex
		states []anycache.State
	)

	cache := anycache.New(store, anycache.WithMetricHook(func(_ string, op anycache.State, _ time.Duration) {
		mu.Lock()
		defer mu.Unlock()

		states = append(states, op)
	}))

	h := newHandler(t, cache, &countingHandler{body: "m"})

	do(h, get("/p"))
	do(h, get("/p"))

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, []anycache.State{anycache.CacheMiss, anycache.CacheHit}, states)
}

func TestMiddleware_Invalidate(t *testing.T) {
	handler := &countingHandler{body: "i"}
	cache := newMemoryCache(t)

	mw, err := New(cache, time.Minute, WithLogger(quietLogger()))
	require.NoError(t, err)

	h := mw.Handler(handler)

	do(h, get("/p"))

	key, ok := mw.Key(get("/p"))
	require.True(t, ok)
	require.NoError(t, cache.Invalidate(t.Context(), key))

	w := do(h, get("/p"))

	assert.Equal(t, "i-2", w.Body.String())
}

func TestMiddleware_TTLFunc(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantTTL time.Duration
	}{
		{name: "custom ttl", path: "/short", wantTTL: time.Second},
		{name: "non-positive falls back to default", path: "/default", wantTTL: time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMockCache(t)
			cache.EXPECT().
				CacheC(mock.Anything, mock.Anything, tt.wantTTL, mock.Anything, mock.Anything).
				RunAndReturn(func(ctx context.Context, _ string, _ time.Duration, gen anycache.CacheGeneratorC, _ ...anycache.CacheItemOptions) ([]byte, error) {
					data, _, err := gen(ctx)
					return data, err
				}).
				Once()

			mw, err := New(cache, time.Minute, WithTTLFunc(func(r *http.Request) time.Duration {
				if r.URL.Path == "/short" {
					return time.Second
				}

				return 0
			}))
			require.NoError(t, err)

			w := do(mw.Handler(http.NotFoundHandler()), get(tt.path))

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestMiddleware_SetFailureKeepsDeDuplication(t *testing.T) {
	store := anycache.NewMockCacheStorage(t)
	store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, anycache.ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errStorage).Once()

	bh := newBlockingHandler(false)
	h := newHandler(t, anycache.New(store), bh)

	results := runConcurrent(t, h, bh, 4)

	assert.Equal(t, int32(1), bh.calls.Load(), "followers must not rerun the handler when storing fails")

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusOK, res.status)
		assert.Equal(t, "body-1", res.body)
	}
}

func TestMiddleware_TimedOutResponseIsDiscarded(t *testing.T) {
	store := anycache.NewMockCacheStorage(t)
	store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, anycache.ErrKeyNotExists).Once()
	// no Set expectation: a timed out response must never be stored

	var calls atomic.Int32

	started := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)

		<-r.Context().Done()

		_, _ = w.Write([]byte(`{"partial":`))
	})

	h := newHandler(t, anycache.New(store), handler, WithTimeout(100*time.Millisecond))

	leader := make(chan *httptest.ResponseRecorder)
	follower := make(chan *httptest.ResponseRecorder)

	go func() { leader <- do(h, get("/p")) }()

	<-started

	go func() { follower <- do(h, get("/p")) }()

	for _, w := range []*httptest.ResponseRecorder{<-leader, <-follower} {
		assert.Equal(t, http.StatusGatewayTimeout, w.Code)
		assert.NotContains(t, w.Body.String(), "partial")
	}

	assert.Equal(t, int32(1), calls.Load())
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestMiddleware_PanicIsLoggedOnce(t *testing.T) {
	logs := &syncBuffer{}
	bh := newBlockingHandler(false)

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bh.ServeHTTP(w, r)
		panic("boom")
	})

	h := newHandler(t, newMemoryCache(t), panicking, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

	results := runConcurrent(t, h, bh, 4)

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusInternalServerError, res.status)
	}

	assert.Equal(t, int32(1), bh.calls.Load())
	assert.Equal(t, 1, strings.Count(logs.String(), "handler panicked"))
}

func TestMiddleware_LeaderResponseDropsTrailers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Checksum")
		w.Header().Set("Cache-Control", "private")
		_, _ = w.Write([]byte("body"))
		w.Header().Set("X-Checksum", "abc")
		w.Header().Set(http.TrailerPrefix+"X-Late", "1")
	})

	w := do(newHandler(t, newMemoryCache(t), handler), get("/p"))

	assert.Equal(t, "body", w.Body.String())
	assert.Equal(t, "private", w.Header().Get("Cache-Control"))
	assert.Empty(t, w.Header().Values("Trailer"), "trailers are never sent, so they must not be announced")
	assert.Empty(t, w.Result().Trailer)
}

func TestMiddleware_InvalidStatusIsLogged(t *testing.T) {
	for _, code := range []int{0, 42, 1000} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			logs := &syncBuffer{}

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			})

			h := newHandler(t, newMemoryCache(t), handler, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

			w := do(h, get("/p"))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, logs.String(), "invalid status code")
			assert.Contains(t, logs.String(), "status="+strconv.Itoa(code))
		})
	}
}

func TestMayRunLater(t *testing.T) {
	tests := []struct {
		state anycache.State
		want  bool
	}{
		{state: anycache.CacheHit, want: false},
		{state: anycache.CacheMiss, want: false},
		{state: anycache.CacheWarmUp, want: true},
		{state: anycache.CacheError, want: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.want, mayRunLater(tt.state))
		})
	}
}

func TestMiddleware_TimeoutDoesNotWaitForHandler(t *testing.T) {
	release := make(chan struct{})

	var calls atomic.Int32

	// The first call ignores its context, like a blocking call made without it.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			<-release
		}

		_, _ = w.Write([]byte("late"))
	})

	h := newHandler(t, newMemoryCache(t), handler, WithTimeout(50*time.Millisecond))

	start := time.Now()
	w := do(h, get("/p"))

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.Less(t, time.Since(start), time.Second, "the timeout must not wait for the handler")

	close(release)

	w = do(h, get("/p"))

	assert.Equal(t, StatusMiss, w.Header().Get(statusHeader), "the late response must not be stored")
	assert.Equal(t, "late", w.Body.String())
	assert.Equal(t, int32(2), calls.Load())
}

func TestMiddleware_AbortIsSharedAs502(t *testing.T) {
	logs := &syncBuffer{}
	bh := newBlockingHandler(false)

	aborting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bh.ServeHTTP(w, r)
		panic(http.ErrAbortHandler)
	})

	h := newHandler(t, newMemoryCache(t), aborting, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

	results := runConcurrent(t, h, bh, 4)

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusBadGateway, res.status)
	}

	assert.Equal(t, int32(1), bh.calls.Load())
	assert.Equal(t, 1, strings.Count(logs.String(), "handler aborted the response"))
}

func TestMiddleware_Finish(t *testing.T) {
	cancelled, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()

	// lostClient returns a recorder whose oversized response had no client to stream to.
	lostClient := func(t *testing.T) *recorder {
		t.Helper()

		rec := newRecorder(1, nil)
		_, err := rec.Write([]byte("xx"))
		require.ErrorIs(t, err, errStreamDetached)

		return rec
	}

	tests := []struct {
		ctx      context.Context
		pe       *panicError
		rec      func(t *testing.T) *recorder
		check    func(t *testing.T, err error)
		name     string
		wantLog  string
		wantResp bool
	}{
		{
			name: "abort after the deadline is a timeout",
			ctx:  cancelled,
			pe:   newPanicError(http.ErrAbortHandler, nil),
			check: func(t *testing.T, err error) {
				t.Helper()

				var te *timeoutError

				assert.ErrorAs(t, err, &te)
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			},
			wantLog: "handler did not finish in time",
		},
		{
			name: "abort in time is an abort",
			ctx:  t.Context(),
			pe:   newPanicError(http.ErrAbortHandler, nil),
			check: func(t *testing.T, err error) {
				t.Helper()

				var ae *abortError

				assert.ErrorAs(t, err, &ae)
			},
			wantLog: "handler aborted the response",
		},
		{
			name: "abort without a client to stream to is a pass",
			ctx:  t.Context(),
			pe:   newPanicError(http.ErrAbortHandler, nil),
			rec:  lostClient,
			check: func(t *testing.T, err error) {
				t.Helper()

				assert.NoError(t, err)
			},
			wantLog:  "no client to stream to",
			wantResp: true,
		},
		{
			name: "panic in time is a panic",
			ctx:  t.Context(),
			pe:   newPanicError("boom", nil),
			check: func(t *testing.T, err error) {
				t.Helper()

				var pe *panicError

				assert.ErrorAs(t, err, &pe)
			},
			wantLog: "handler panicked",
		},
		{
			name: "panic without a client to stream to is a panic",
			ctx:  t.Context(),
			pe:   newPanicError("boom", nil),
			rec:  lostClient,
			check: func(t *testing.T, err error) {
				t.Helper()

				var pe *panicError

				assert.ErrorAs(t, err, &pe)
			},
			wantLog: "handler panicked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &syncBuffer{}
			mw := newTestMiddleware(t, WithLogger(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))

			rec := newRecorder(0, nil)
			if tt.rec != nil {
				rec = tt.rec(t)
			}

			resp, err := mw.finish(context.Cause(tt.ctx), rec, tt.pe, "key")

			if tt.wantResp {
				require.NotNil(t, resp)
				assert.True(t, resp.overflow, "the response is passed, never stored or shared")
			} else {
				assert.Nil(t, resp)
			}

			tt.check(t, err)
			assert.Contains(t, logs.String(), tt.wantLog)
		})
	}
}

func TestMiddleware_PrivateResponseIsReportedAsBypass(t *testing.T) {
	h := newHandler(t, newMemoryCache(t), &countingHandler{body: "c", header: http.Header{"Set-Cookie": {"s=1"}}})

	w := do(h, get("/p"))

	assert.Equal(t, "c-1", w.Body.String())
	assert.Equal(t, StatusBypass, w.Header().Get(statusHeader), "a private response is neither stored nor shared")
}

// streamLimit is the WithMaxBodySize used by the streaming tests.
const streamLimit = 1 << 10

// chunk returns n bytes of filler.
func chunk(n int) []byte {
	return bytes.Repeat([]byte("x"), n)
}

// guardWriter is a ResponseWriter that fails the test when it is used after
// the ServeHTTP call it was passed to returned.
type guardWriter struct {
	t        *testing.T
	header   http.Header
	body     bytes.Buffer
	status   int
	flushes  int
	returned atomic.Bool
}

func newGuardWriter(t *testing.T) *guardWriter {
	t.Helper()

	return &guardWriter{t: t, header: make(http.Header)}
}

func (w *guardWriter) check(method string) {
	if w.returned.Load() {
		w.t.Errorf("%s called on the leader's writer after ServeHTTP returned", method)
	}
}

func (w *guardWriter) Header() http.Header {
	w.check("Header")
	return w.header
}

func (w *guardWriter) WriteHeader(code int) {
	w.check("WriteHeader")

	if w.status == 0 {
		w.status = code
	}
}

func (w *guardWriter) Write(p []byte) (int, error) {
	w.check("Write")

	if w.status == 0 {
		w.status = http.StatusOK
	}

	return w.body.Write(p)
}

func (w *guardWriter) Flush() {
	w.check("Flush")
	w.flushes++
}

// serveGuarded serves r on w, marks w as returned and returns the recovered panic, if any.
func serveGuarded(h http.Handler, w *guardWriter, r *http.Request) (recovered any) {
	defer func() {
		recovered = recover()

		w.returned.Store(true)
	}()

	h.ServeHTTP(w, r)

	return nil
}

func TestMiddleware_StreamsOversizedResponse(t *testing.T) {
	const size = 64 << 10

	tests := []struct {
		name              string
		wantEncoding      []string
		setContentLength  bool
		wantContentLength int64
	}{
		{
			name:              "content length is kept",
			setContentLength:  true,
			wantContentLength: size,
		},
		{
			name:              "chunked without content length",
			wantContentLength: -1,
			wantEncoding:      []string{"chunked"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)

				w.Header().Set("Content-Type", "application/octet-stream")

				if tt.setContentLength {
					w.Header().Set("Content-Length", strconv.Itoa(size))
				}

				w.WriteHeader(http.StatusCreated)

				for range size / 512 {
					_, err := w.Write(chunk(512))
					assert.NoError(t, err)
				}
			})

			srv := httptest.NewServer(newHandler(t, newMemoryCache(t), handler, WithMaxBodySize(streamLimit)))
			t.Cleanup(srv.Close)

			for range 2 {
				resp, err := srv.Client().Get(srv.URL + "/p")
				require.NoError(t, err)

				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())

				assert.Equal(t, http.StatusCreated, resp.StatusCode)
				assert.Equal(t, size, len(body))
				assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
				assert.Equal(t, StatusBypass, resp.Header.Get(statusHeader))
				assert.Equal(t, tt.wantContentLength, resp.ContentLength)
				assert.Equal(t, tt.wantEncoding, resp.TransferEncoding)
			}

			assert.Equal(t, int32(2), calls.Load(), "an oversized response is never stored")
		})
	}
}

func TestMiddleware_StreamedResponseIsFlushed(t *testing.T) {
	first := chunk(2 * streamLimit)
	release := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(first)
		assert.NoError(t, http.NewResponseController(w).Flush())

		<-release

		_, _ = w.Write([]byte("rest"))
	})

	srv := httptest.NewServer(newHandler(t, newMemoryCache(t), handler, WithMaxBodySize(streamLimit)))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/p")
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	read := make(chan error, 1)

	go func() {
		_, err := io.ReadFull(resp.Body, make([]byte, len(first)))
		read <- err
	}()

	select {
	case err := <-read:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the streamed part was not flushed to the client while the handler runs")
	}

	close(release)

	rest, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "rest", string(rest))
}

func TestMiddleware_NoWriteAfterLeaderIsGone(t *testing.T) {
	streamed := make(chan struct{})
	release := make(chan struct{})
	handlerErr := make(chan error, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(chunk(2 * streamLimit))
		_ = http.NewResponseController(w).Flush()

		close(streamed)
		<-release

		var err error
		for range 10 {
			if _, err = w.Write(chunk(64)); err != nil {
				break
			}
		}

		_ = http.NewResponseController(w).Flush() // a no-op once detached

		handlerErr <- err
	})

	h := newHandler(t, newMemoryCache(t), handler, WithMaxBodySize(streamLimit))

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	w := newGuardWriter(t)
	recovered := make(chan any, 1)

	go func() { recovered <- serveGuarded(h, w, get("/p").WithContext(ctx)) }()

	<-streamed
	cancel()

	assert.Equal(t, http.ErrAbortHandler, <-recovered, "a cancelled streamed response is aborted")

	close(release)

	require.ErrorIs(t, <-handlerErr, errStreamDetached)
	assert.Equal(t, http.StatusOK, w.status)
	assert.Equal(t, 2*streamLimit, w.body.Len())
	assert.Equal(t, 1, w.flushes)
}

func TestMiddleware_ClientCancelsMidStream(t *testing.T) {
	first := chunk(2 * streamLimit)
	release := make(chan struct{})
	returned := make(chan struct{})
	handlerErr := make(chan error, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(first)
		_ = http.NewResponseController(w).Flush()

		<-release

		var err error
		for range 10 {
			if _, err = w.Write(chunk(64)); err != nil {
				break
			}
		}

		handlerErr <- err
	})

	h := newHandler(t, newMemoryCache(t), handler, WithMaxBodySize(streamLimit))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/p", http.NoBody)
	require.NoError(t, err)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	_, err = io.ReadFull(resp.Body, make([]byte, len(first)))
	require.NoError(t, err)

	cancel()
	<-returned
	close(release)

	assert.ErrorIs(t, <-handlerErr, errStreamDetached, "writes after ServeHTTP returned are refused")
}

func TestMiddleware_AbortsStreamedResponseOnFailure(t *testing.T) {
	tests := []struct {
		after func(w http.ResponseWriter, r *http.Request)
		name  string
		opts  []Option
	}{
		{
			name:  "panic",
			after: func(http.ResponseWriter, *http.Request) { panic("boom") },
		},
		{
			name:  "abort",
			after: func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) },
		},
		{
			name: "timeout",
			after: func(w http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()

				_, _ = w.Write([]byte("late"))
			},
			opts: []Option{WithTimeout(50 * time.Millisecond)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done := make(chan struct{})

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)

				_, _ = w.Write(chunk(2 * streamLimit))
				tt.after(w, r)
			})

			opts := append([]Option{WithMaxBodySize(streamLimit)}, tt.opts...)
			h := newHandler(t, newMemoryCache(t), handler, opts...)

			w := newGuardWriter(t)

			assert.Equal(t, http.ErrAbortHandler, serveGuarded(h, w, get("/p")))
			<-done

			assert.Equal(t, http.StatusOK, w.status, "no error status is sent once the response started")
			assert.NotContains(t, w.body.String(), http.StatusText(http.StatusInternalServerError))
			assert.NotContains(t, w.body.String(), http.StatusText(http.StatusBadGateway))
			assert.NotContains(t, w.body.String(), http.StatusText(http.StatusGatewayTimeout))
		})
	}
}

func TestMiddleware_FollowersStreamOversizedResponses(t *testing.T) {
	const followers = 4

	bh := newBlockingHandler(false)
	h := newHandler(t, newMemoryCache(t), bh, WithMaxBodySize(len("body-")))

	results := runConcurrent(t, h, bh, followers)

	assert.Equal(t, int32(followers+1), bh.calls.Load(), "oversized responses are not shared")

	seen := map[string]bool{}

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusOK, res.status)
		assert.Regexp(t, `^body-\d+$`, res.body)
		assert.False(t, seen[res.body], "response %s served twice", res.body)

		seen[res.body] = true
	}
}

// staleStore returns a storage mock holding a stale entry due for a warm-up.
// Set is not expected: an oversized response must never be stored.
func staleStore(t *testing.T) *anycache.MockCacheStorage {
	t.Helper()

	store := anycache.NewMockCacheStorage(t)

	data := encodeEntry(&response{
		status:    http.StatusOK,
		header:    http.Header{},
		body:      []byte("stale"),
		createdAt: time.Now(),
	})

	store.EXPECT().GetWithTTL(mock.Anything, mock.Anything).Return(data, time.Second, nil).Once()

	return store
}

func TestMiddleware_WarmUpOversizedResponseWritesNowhere(t *testing.T) {
	served := make(chan struct{})
	handlerErr := make(chan error, 1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-served

		_, err := w.Write(chunk(2 * streamLimit))
		handlerErr <- err
	})

	cache := anycache.New(staleStore(t))
	h := newHandler(t, cache, handler, WithMaxBodySize(streamLimit), WithWarmUpTTL(time.Minute))

	w := newGuardWriter(t)

	assert.Nil(t, serveGuarded(h, w, get("/p")))
	close(served)

	assert.ErrorIs(t, <-handlerErr, errStreamDetached)
	assert.NoError(t, cache.Close())

	assert.Equal(t, StatusWarmUp, w.header.Get(statusHeader))
	assert.Equal(t, "stale", w.body.String())
}

func TestMiddleware_WarmUpStreamingBeforeDetach(t *testing.T) {
	tests := []struct {
		name string
		// rest finishes the response once the handler is released.
		rest      func(w http.ResponseWriter) error
		wantPanic any
		wantBody  string
	}{
		{
			name: "completed response is kept",
			rest: func(w http.ResponseWriter) error {
				_, err := w.Write([]byte("rest"))
				return err
			},
			wantBody: string(chunk(2*streamLimit)) + "rest",
		},
		{
			name:      "failed response is aborted",
			rest:      func(http.ResponseWriter) error { panic("boom") },
			wantPanic: http.ErrAbortHandler,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overflowed := make(chan struct{})
			release := make(chan struct{})
			handlerErr := make(chan error, 1)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(chunk(2 * streamLimit))

				close(overflowed)
				<-release

				handlerErr <- tt.rest(w)
			})

			// The hook runs before CacheC returns: holding it until the warm-up run
			// streamed makes the race between the warm-up and detach deterministic.
			cache := anycache.New(staleStore(t), anycache.WithMetricHook(func(_ string, op anycache.State, _ time.Duration) {
				if op == anycache.CacheWarmUp {
					<-overflowed
				}
			}))

			h := newHandler(t, cache, handler, WithMaxBodySize(streamLimit), WithWarmUpTTL(time.Minute))

			w := newGuardWriter(t)
			served := make(chan any, 1)

			go func() { served <- serveGuarded(h, w, get("/p")) }()

			select {
			case <-served:
				t.Fatal("the request returned while the warm-up run was still streaming to it")
			case <-time.After(50 * time.Millisecond):
			}

			close(release)

			assert.Equal(t, tt.wantPanic, <-served)
			assert.NoError(t, cache.Close())
			assert.Equal(t, http.StatusOK, w.status)
			assert.NotContains(t, w.body.String(), "stale")

			if tt.wantPanic == nil {
				assert.NoError(t, <-handlerErr)
				assert.Equal(t, tt.wantBody, w.body.String())
			}
		})
	}
}

func TestMiddleware_FollowersAreReleasedAtOverflow(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	hold := make(chan struct{})

	var calls atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) > 1 {
			_, _ = w.Write(chunk(2 * streamLimit))
			_, _ = w.Write([]byte("follower"))

			return
		}

		close(started)
		<-release

		_, _ = w.Write(chunk(2 * streamLimit))

		// The leader's stream stays open until the follower got its response.
		<-hold

		_, _ = w.Write([]byte("leader"))
	})

	gate := &entryGate{}
	h := gate.wrap(newHandler(t, newMemoryCache(t), handler, WithMaxBodySize(streamLimit)))

	leader := newGuardWriter(t)
	leaderDone := make(chan any, 1)

	go func() { leaderDone <- serveGuarded(h, leader, get("/p")) }()

	<-started

	followerDone := make(chan *httptest.ResponseRecorder, 1)

	go func() { followerDone <- do(h, get("/p")) }()

	gate.wait(t, 2)
	close(release)

	select {
	case follower := <-followerDone:
		assert.Equal(t, http.StatusOK, follower.Code)
		assert.Equal(t, string(chunk(2*streamLimit))+"follower", follower.Body.String())
		assert.Equal(t, StatusBypass, follower.Header().Get(statusHeader))
	case <-time.After(5 * time.Second):
		close(hold)
		t.Fatal("the follower waited for the leader's stream instead of being released at overflow")
	}

	close(hold)

	assert.Nil(t, <-leaderDone, "a completed streamed response is not aborted")
	assert.Equal(t, string(chunk(2*streamLimit))+"leader", leader.body.String())
	assert.Equal(t, int32(2), calls.Load())
}

func TestMiddleware_AbortWithoutClientToStreamToIsPassed(t *testing.T) {
	logs := &syncBuffer{}
	started := make(chan struct{})
	release := make(chan struct{})

	var calls atomic.Int32

	// Like httputil.ReverseProxy, the handler aborts when writing the body fails.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			<-release
		}

		if _, err := w.Write(chunk(2 * streamLimit)); err != nil {
			panic(http.ErrAbortHandler)
		}

		_, _ = fmt.Fprintf(w, "-%d", n)
	})

	h := newHandler(t, newMemoryCache(t), handler,
		WithMaxBodySize(streamLimit),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
	)

	_, follower := serveWithCancelledLeader(t, h, started, func() { close(release) })

	assert.Equal(t, http.StatusOK, follower.Code, "the follower reruns the handler instead of getting a 502")
	assert.Equal(t, string(chunk(2*streamLimit))+"-2", follower.Body.String())
	assert.NotContains(t, logs.String(), "handler aborted the response")
}

func TestMiddleware_TimeoutWhileClientDoesNotRead(t *testing.T) {
	returned := make(chan struct{})
	handlerErr := make(chan error, 1)

	// The handler ignores its context and writes until a write fails.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for {
			if _, err := w.Write(chunk(32 << 10)); err != nil {
				handlerErr <- err
				return
			}
		}
	})

	h := newHandler(t, newMemoryCache(t), handler, WithMaxBodySize(streamLimit), WithTimeout(200*time.Millisecond))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	if tcp, ok := conn.(*net.TCPConn); ok {
		require.NoError(t, tcp.SetReadBuffer(4<<10))
	}

	_, err = io.WriteString(conn, "GET /p HTTP/1.1\r\nHost: example.com\r\n\r\n")
	require.NoError(t, err)

	// The client reads nothing until the request is over.
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("ServeHTTP did not return after the timeout while its client was not reading")
	}

	assert.Error(t, <-handlerErr, "the stalled write fails")

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))

	data, err := io.ReadAll(conn)

	var ne net.Error
	if errors.As(err, &ne) {
		assert.False(t, ne.Timeout(), "the connection is closed, not left open")
	}

	assert.True(t, bytes.HasPrefix(data, []byte("HTTP/1.1 200 OK")))
	assert.False(t, bytes.HasSuffix(data, []byte("0\r\n\r\n")), "the aborted response is not terminated")
}
