package httpcache

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// keyPrefix namespaces middleware keys inside a shared anycache.Cache.
const keyPrefix = "httpcache:"

// Key returns the storage key used for r and whether r is cacheable at all.
//
// The key can be passed to anycache.Cache.Invalidate to evict the cached response.
// Note that each combination of configured vary header values has its own key.
func (m *Middleware) Key(r *http.Request) (string, bool) {
	if m.cfg.bypassRequest(r) {
		return "", false
	}

	return m.key(r)
}

// key builds the storage key for r without applying the request bypass policy.
// The canonical key is hashed so that it is always short and contains only
// characters accepted by every storage backend (for example memcache).
func (m *Middleware) key(r *http.Request) (string, bool) {
	var canonical string

	if m.cfg.keyFunc != nil {
		k, ok := m.cfg.keyFunc(r)
		if !ok {
			return "", false
		}

		canonical = k
	} else {
		canonical = defaultKey(r)
	}

	var b strings.Builder

	b.WriteString(canonical)

	for _, name := range m.cfg.varyHeaders {
		b.WriteByte('\n')
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.Join(r.Header.Values(name), ","))
	}

	sum := sha256.Sum256([]byte(b.String()))

	return keyPrefix + hex.EncodeToString(sum[:]), true
}

// defaultKey returns "METHOD\nhost\npath\nsorted-query" for r.
func defaultKey(r *http.Request) string {
	var b strings.Builder

	b.WriteString(r.Method)
	b.WriteByte('\n')
	b.WriteString(r.Host)
	b.WriteByte('\n')
	b.WriteString(r.URL.EscapedPath())
	b.WriteByte('\n')

	if r.URL.RawQuery != "" {
		// Encode sorts by key, making the key independent of parameter order.
		b.WriteString(r.URL.Query().Encode())
	}

	return b.String()
}
