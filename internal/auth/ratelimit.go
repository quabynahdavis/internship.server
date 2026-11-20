package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxLimiterEntries = 10000
	defaultLimiterEntryTTL   = 15 * time.Minute
	pruneEveryNOperations    = 256
)

type ipBucket struct {
	tokens   float64
	lastSeen time.Time
}

// IPRateLimiter enforces a thread-safe per-IP token-bucket rate limit with bounded memory.
type IPRateLimiter struct {
	mu         sync.Mutex
	ratePerSec float64
	burst      int
	maxEntries int
	entryTTL   time.Duration
	now        func() time.Time
	ops        uint64
	buckets    map[string]*ipBucket
}

// NewIPRateLimiter constructs a per-IP token-bucket limiter.
func NewIPRateLimiter(ratePerSec float64, burst int) *IPRateLimiter {
	if ratePerSec < 0 {
		ratePerSec = 0
	}
	if burst < 0 {
		burst = 0
	}

	return &IPRateLimiter{
		ratePerSec: ratePerSec,
		burst:      burst,
		maxEntries: defaultMaxLimiterEntries,
		entryTTL:   defaultLimiterEntryTTL,
		now:        time.Now,
		buckets:    make(map[string]*ipBucket),
	}
}

// SetClock overrides the clock used by the rate limiter (intended for tests).
func (l *IPRateLimiter) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

// SetMaxEntries overrides the maximum number of tracked IP buckets before eviction (intended for tests).
func (l *IPRateLimiter) SetMaxEntries(maxEntries int) {
	if maxEntries <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxEntries = maxEntries
}

// ClientIP extracts the client IP from r.RemoteAddr using net.SplitHostPort with fallback.
func ClientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		return host
	}
	return addr
}

// AllowRequest checks whether a request from r.RemoteAddr is allowed under the rate limit.
func (l *IPRateLimiter) AllowRequest(r *http.Request) bool {
	return l.Allow(ClientIP(r))
}

// Allow consumes one token from the bucket for ip and reports whether the request is permitted.
func (l *IPRateLimiter) Allow(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.burst <= 0 {
		return false
	}

	now := l.now()
	l.ops++
	if l.ops%pruneEveryNOperations == 0 {
		l.pruneExpiredLocked(now)
	}

	b, exists := l.buckets[ip]
	if !exists {
		if len(l.buckets) >= l.maxEntries {
			l.pruneExpiredLocked(now)
			if len(l.buckets) >= l.maxEntries {
				l.evictOldestLocked()
			}
		}
		l.buckets[ip] = &ipBucket{
			tokens:   float64(l.burst - 1),
			lastSeen: now,
		}
		return true
	}

	if elapsed := now.Sub(b.lastSeen).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.ratePerSec
		maxTokens := float64(l.burst)
		if b.tokens > maxTokens {
			b.tokens = maxTokens
		}
	}
	b.lastSeen = now

	if b.tokens < 1.0 {
		return false
	}

	b.tokens -= 1.0
	return true
}

func (l *IPRateLimiter) pruneExpiredLocked(now time.Time) {
	for ip, b := range l.buckets {
		if now.Sub(b.lastSeen) > l.entryTTL {
			delete(l.buckets, ip)
		}
	}
}

func (l *IPRateLimiter) evictOldestLocked() {
	var oldestIP string
	var oldestTime time.Time
	first := true

	for ip, b := range l.buckets {
		if first || b.lastSeen.Before(oldestTime) {
			oldestIP = ip
			oldestTime = b.lastSeen
			first = false
		}
	}
	if !first {
		delete(l.buckets, oldestIP)
	}
}
