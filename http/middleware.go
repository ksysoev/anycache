package http

import (
	"context"
	"net/http"
	"time"

	"github.com/ksysoev/anycache"
)

type Cache interface {
	CacheStructcontext.Context, string, time.Duration, func(context.Context) (any, error), any, ...anycache.CacheItemOptions) error
}

type Middleware struct {
	isCacheable func (*http.Request) bool
}

func NewCacheMiddleware(cache Cache, next http.Handler) http.Handler {
	 m := Middleware{
		isCacheable: defaultCheckerIsCacheable,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.isCacheable(r) {
			next.ServeHTTP(w, r)

			return
		}

		
		
	})
}


func defaultCheckerIsCacheable(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}

	return true
}
