package httpcache

import (
	"net/http"
	"net/textproto"
	"strings"
)

// outcome describes what may be done with a recorded response.
type outcome int

const (
	// outcomeStore means the response is stored and shared with concurrent followers.
	outcomeStore outcome = iota
	// outcomeShare means the response is shared with concurrent followers but not stored
	// (for example a 500 or any status not configured as cacheable).
	outcomeShare
	// outcomePass means the response is private to the request that produced it:
	// it is neither stored nor shared, followers run the handler themselves.
	outcomePass
)

// hopByHopHeaders are connection-specific headers that must never be stored.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Connection",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// conditionalHeaders are stripped from the request passed to the handler so it
// always produces a full response that can be shared and stored.
var conditionalHeaders = []string{
	"If-Match",
	"If-None-Match",
	"If-Modified-Since",
	"If-Unmodified-Since",
	"If-Range",
}

// bypassRequest reports whether r must skip the cache entirely.
func (c *config) bypassRequest(r *http.Request) bool {
	switch {
	case r.Method != http.MethodGet:
		return true
	case r.Header.Get("Range") != "":
		return true
	case !c.allowAuthorized && r.Header.Get("Authorization") != "":
		return true
	case !c.allowCookies && r.Header.Get("Cookie") != "":
		return true
	case c.respectNoCache && requestsNoCache(r.Header):
		return true
	default:
		return false
	}
}

// requestsNoCache reports whether the request asks to bypass cached responses.
func requestsNoCache(h http.Header) bool {
	if hasDirective(h, "no-cache", "no-store") {
		return true
	}

	for _, v := range h.Values("Pragma") {
		if strings.Contains(strings.ToLower(v), "no-cache") {
			return true
		}
	}

	return false
}

// classify decides whether a recorded response may be stored and/or shared.
func (c *config) classify(resp *response) outcome {
	switch {
	case resp.overflow:
		return outcomePass
	case resp.status == http.StatusSwitchingProtocols:
		// An upgrade belongs to a single connection and cannot be replayed.
		return outcomePass
	case len(resp.header.Values("Set-Cookie")) > 0:
		return outcomePass
	case hasDirective(resp.header, "no-store", "private", "no-cache"):
		return outcomePass
	case !c.varySafe(resp.header):
		return outcomePass
	case !c.cacheableStatus[resp.status]:
		return outcomeShare
	default:
		return outcomeStore
	}
}

// varySafe reports whether every header named in the response Vary header
// is part of the cache key.
func (c *config) varySafe(h http.Header) bool {
	for _, v := range h.Values("Vary") {
		for name := range strings.SplitSeq(v, ",") {
			name = strings.TrimSpace(name)

			switch {
			case name == "":
				continue
			case name == "*":
				return false
			case !c.varySet[textproto.CanonicalMIMEHeaderKey(name)]:
				return false
			}
		}
	}

	return true
}

// hasDirective reports whether the Cache-Control header contains any of the given directives.
func hasDirective(h http.Header, directives ...string) bool {
	for _, v := range h.Values("Cache-Control") {
		for d := range strings.SplitSeq(v, ",") {
			d = strings.TrimSpace(d)
			if i := strings.IndexByte(d, '='); i >= 0 {
				d = d[:i]
			}

			for _, want := range directives {
				if strings.EqualFold(d, want) {
					return true
				}
			}
		}
	}

	return false
}

// storableHeaders returns a copy of h without hop-by-hop headers, headers
// listed in Connection, Set-Cookie, Content-Length and trailer declarations.
func storableHeaders(h http.Header) http.Header {
	out := h.Clone()

	for _, v := range h.Values("Connection") {
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				out.Del(name)
			}
		}
	}

	for _, name := range hopByHopHeaders {
		out.Del(name)
	}

	out.Del("Set-Cookie")
	out.Del("Content-Length")

	for name := range out {
		if isTrailer(name) {
			delete(out, name)
		}
	}

	return out
}

// prepareRequest returns a copy of r that is safe to use after r's handler has
// returned: it has no body and no conditional headers.
func prepareRequest(r *http.Request) *http.Request {
	clone := r.Clone(r.Context())
	clone.Body = http.NoBody
	clone.GetBody = nil
	clone.ContentLength = 0

	for _, name := range conditionalHeaders {
		clone.Header.Del(name)
	}

	return clone
}

// isTrailer reports whether name declares or carries a trailer. Trailers are
// not supported because the recorder snapshots headers when the status is written.
func isTrailer(name string) bool {
	return name == "Trailer" || strings.HasPrefix(name, http.TrailerPrefix)
}
