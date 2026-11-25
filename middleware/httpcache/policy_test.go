package httpcache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_BypassRequest(t *testing.T) {
	tests := []struct {
		header http.Header
		name   string
		method string
		opts   []Option
		want   bool
	}{
		{name: "plain GET", method: http.MethodGet, want: false},
		{name: "HEAD", method: http.MethodHead, want: true},
		{name: "POST", method: http.MethodPost, want: true},
		{name: "range", method: http.MethodGet, header: http.Header{"Range": {"bytes=0-1"}}, want: true},
		{name: "authorization", method: http.MethodGet, header: http.Header{"Authorization": {"Bearer x"}}, want: true},
		{
			name: "authorization allowed", method: http.MethodGet, header: http.Header{"Authorization": {"Bearer x"}},
			opts: []Option{WithAllowAuthorized()}, want: false,
		},
		{name: "cookie", method: http.MethodGet, header: http.Header{"Cookie": {"a=b"}}, want: true},
		{
			name: "cookie allowed", method: http.MethodGet, header: http.Header{"Cookie": {"a=b"}},
			opts: []Option{WithAllowCookies()}, want: false,
		},
		{name: "no-cache ignored by default", method: http.MethodGet, header: http.Header{"Cache-Control": {"no-cache"}}, want: false},
		{
			name: "no-cache respected", method: http.MethodGet, header: http.Header{"Cache-Control": {"max-age=0, no-cache"}},
			opts: []Option{WithRespectRequestNoCache()}, want: true,
		},
		{
			name: "pragma respected", method: http.MethodGet, header: http.Header{"Pragma": {"no-cache"}},
			opts: []Option{WithRespectRequestNoCache()}, want: true,
		},
		{
			name: "max-age=0 is not no-cache", method: http.MethodGet, header: http.Header{"Cache-Control": {"max-age=0"}},
			opts: []Option{WithRespectRequestNoCache()}, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			for _, opt := range tt.opts {
				opt(&cfg)
			}

			r := httptest.NewRequest(tt.method, "/", http.NoBody)
			for k, v := range tt.header {
				r.Header[k] = v
			}

			assert.Equal(t, tt.want, cfg.bypassRequest(r))
		})
	}
}

func TestConfig_Classify(t *testing.T) {
	tests := []struct {
		header   http.Header
		name     string
		opts     []Option
		status   int
		want     outcome
		overflow bool
	}{
		{name: "200", status: http.StatusOK, want: outcomeStore},
		{name: "500 is shared only", status: http.StatusInternalServerError, want: outcomeShare},
		{name: "404 by default shared only", status: http.StatusNotFound, want: outcomeShare},
		{name: "404 configured", status: http.StatusNotFound, opts: []Option{WithCacheableStatus(http.StatusOK, http.StatusNotFound)}, want: outcomeStore},
		{name: "overflow", status: http.StatusOK, overflow: true, want: outcomePass},
		{
			name: "switching protocols even if configured", status: http.StatusSwitchingProtocols,
			opts: []Option{WithCacheableStatus(http.StatusSwitchingProtocols)}, want: outcomePass,
		},
		{name: "set-cookie", status: http.StatusOK, header: http.Header{"Set-Cookie": {"s=1"}}, want: outcomePass},
		{name: "set-cookie on 500", status: http.StatusInternalServerError, header: http.Header{"Set-Cookie": {"s=1"}}, want: outcomePass},
		{name: "no-store", status: http.StatusOK, header: http.Header{"Cache-Control": {"no-store"}}, want: outcomePass},
		{name: "private with fields", status: http.StatusOK, header: http.Header{"Cache-Control": {`max-age=60, Private="X"`}}, want: outcomePass},
		{name: "no-cache", status: http.StatusOK, header: http.Header{"Cache-Control": {"no-cache"}}, want: outcomePass},
		{name: "public", status: http.StatusOK, header: http.Header{"Cache-Control": {"public, max-age=60"}}, want: outcomeStore},
		{name: "vary star", status: http.StatusOK, header: http.Header{"Vary": {"*"}}, want: outcomePass},
		{name: "vary unknown", status: http.StatusOK, header: http.Header{"Vary": {"Accept-Language"}}, want: outcomePass},
		{
			name: "vary configured", status: http.StatusOK, header: http.Header{"Vary": {"accept, Accept-Encoding"}},
			opts: []Option{WithVaryHeaders("Accept", "accept-encoding")}, want: outcomeStore,
		},
		{
			name: "vary partly configured", status: http.StatusOK, header: http.Header{"Vary": {"Accept", "Origin"}},
			opts: []Option{WithVaryHeaders("Accept")}, want: outcomePass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultConfig()
			for _, opt := range tt.opts {
				opt(&cfg)
			}

			header := tt.header
			if header == nil {
				header = http.Header{}
			}

			assert.Equal(t, tt.want, cfg.classify(&response{status: tt.status, header: header, overflow: tt.overflow}))
		})
	}
}

func TestPrepareRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/a?b=c", strings.NewReader("body"))
	r.Header.Set("If-None-Match", `"v1"`)
	r.Header.Set("If-Modified-Since", "Mon, 02 Jan 2006 15:04:05 GMT")
	r.Header.Set("Accept", "text/plain")

	clone := prepareRequest(r)

	assert.Equal(t, http.NoBody, clone.Body)
	assert.Zero(t, clone.ContentLength)
	assert.Empty(t, clone.Header.Get("If-None-Match"))
	assert.Empty(t, clone.Header.Get("If-Modified-Since"))
	assert.Equal(t, "text/plain", clone.Header.Get("Accept"))
	assert.Equal(t, "/a?b=c", clone.URL.RequestURI())

	// the original request is untouched
	assert.Equal(t, `"v1"`, r.Header.Get("If-None-Match"))
}
