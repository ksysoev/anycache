package httpcache

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
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

// defaultKey returns "METHOD\nscheme\nhost\npath\nquery" for r.
// The scheme is "https" when the request arrived over TLS and "http" otherwise.
func defaultKey(r *http.Request) string {
	var b strings.Builder

	b.WriteString(r.Method)
	b.WriteByte('\n')

	if r.TLS != nil {
		b.WriteString("https")
	} else {
		b.WriteString("http")
	}

	b.WriteByte('\n')
	b.WriteString(r.Host)
	b.WriteByte('\n')
	b.WriteString(r.URL.EscapedPath())
	b.WriteByte('\n')
	writeQuery(&b, r.URL.RawQuery)

	return b.String()
}

// writeQuery writes the query sorted by key so that parameter order does not
// matter. A query that does not parse cleanly (invalid escapes, semicolons) is
// written verbatim, because parsing would silently drop the invalid pairs and
// make different queries share a key.
func writeQuery(b *strings.Builder, rawQuery string) {
	if rawQuery == "" {
		return
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		b.WriteString("raw:")
		b.WriteString(rawQuery)

		return
	}

	b.WriteString("q:")
	b.WriteString(values.Encode())
}
