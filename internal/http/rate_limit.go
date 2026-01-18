package httpx

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type IPRateLimiter struct {
	rps   rate.Limit
	burst int
	mu    sync.Mutex
	ips   map[string]*ipLimiter
}

func NewIPRateLimiter(rps float64, burst int) *IPRateLimiter {
	l := &IPRateLimiter{
		rps:   rate.Limit(rps),
		burst: burst,
		ips:   make(map[string]*ipLimiter),
	}

	go l.cleanupLoop()
	return l
}

func (l *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry, ok := l.ips[ip]; ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	limiter := rate.NewLimiter(l.rps, l.burst)
	l.ips[ip] = &ipLimiter{limiter: limiter, lastSeen: time.Now()}
	return limiter
}

func (l *IPRateLimiter) cleanupLoop() {
	for {
		time.Sleep(2 * time.Minute)
		l.cleanup()
	}
}

func (l *IPRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for ip, entry := range l.ips {
		if time.Since(entry.lastSeen) > 10*time.Minute {
			delete(l.ips, ip)
		}
	}
}

func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		limiter := l.getLimiter(ip)
		if !limiter.Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited","retry_after_seconds":30}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *IPRateLimiter) Allow(ip string) bool {
	limiter := l.getLimiter(ip)
	return limiter.Allow()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
