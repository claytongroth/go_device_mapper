package api

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// type Middleware is a func taking http.Handler that returns http.Handler, so we can nest
type Middleware func(http.Handler) http.Handler

// Chain applies mws (middlewares) to h (http handler) in the order written, outermost first — so
// Chain(h, A, B, C) behaves like A(B(C(h)))
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// ipRateLimiter tracks one rate.Limiter per client IP, guarded by a mutex
// — same shape of problem as graph.Store's node map: a shared map that
// concurrent requests all need to read and write safely.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rps      rate.Limit
	burst    int
}

// getOrCreate method on the ipRateLimiter struct
func (l *ipRateLimiter) getOrCreate(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	// map lookup on this particular IP, if we find it, return it, else make a new one.
	lim, ok := l.limiters[ip]
	if !ok {
		// New rate.NewLimiter with the rate/s and a burst
		// The burst allows X upfront requests before they start to fill the limit
		lim = rate.NewLimiter(l.rps, l.burst)
		l.limiters[ip] = lim
	}
	return lim
}

// RateLimitMiddleware allows at most rps requests/second (with a burst of
// burst) per client IP, rejecting anything beyond that with 429 Too Many Requests instead of passing it through to next Middleware
// Known limitation: limiters map only ever grows — an IP that stops
// making requests still has an entry sitting in memory forever. Fine at
// this project's scale; a real long-running service would wanna get rid of these entries
func RateLimitMiddleware(rps float64, burst int) Middleware {

	// We need a reference here because this struct contains a mutex
	limiter := &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}

	// return a function that returns another handler
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			if !limiter.getOrCreate(ip).Allow() {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": "rate limit exceeded",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Simple middleware to log stuff
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}
