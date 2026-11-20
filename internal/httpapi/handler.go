// Package httpapi exposes the HTTP REST endpoints for health, onboarding,
// session authentication, and student profile management.
package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"internship/server/internal/auth"
)

const (
	// SessionTTL is the lifetime of newly issued bearer sessions (7 days).
	SessionTTL = 7 * 24 * time.Hour

	defaultRatePerSec = 5.0 / 60.0 // 5 requests per minute sustained refill
	defaultBurst      = 10
)

// Options configures optional dependencies for NewHandler.
type Options struct {
	RateLimiter *auth.IPRateLimiter
	Now         func() time.Time
	Logger      *slog.Logger
}

// Option customizes Options passed to NewHandler.
type Option func(*Options)

// WithRateLimiter overrides the per-IP rate limiter used on auth endpoints.
func WithRateLimiter(limiter *auth.IPRateLimiter) Option {
	return func(o *Options) {
		o.RateLimiter = limiter
	}
}

// WithClock overrides the clock used for session issuance and expiration checks.
func WithClock(now func() time.Time) Option {
	return func(o *Options) {
		o.Now = now
	}
}

// WithLogger overrides the structured logger used by request logging and error handlers.
func WithLogger(logger *slog.Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

// WithOptions applies a complete Options struct, overriding non-nil fields.
func WithOptions(custom Options) Option {
	return func(o *Options) {
		if custom.RateLimiter != nil {
			o.RateLimiter = custom.RateLimiter
		}
		if custom.Now != nil {
			o.Now = custom.Now
		}
		if custom.Logger != nil {
			o.Logger = custom.Logger
		}
	}
}

type server struct {
	db      *sql.DB
	pepper  string
	limiter *auth.IPRateLimiter
	now     func() time.Time
	logger  *slog.Logger
}

// NewHandler constructs the application's HTTP router and middleware stack.
func NewHandler(db *sql.DB, pepper string, opts ...Option) http.Handler {
	cfg := Options{
		RateLimiter: auth.NewIPRateLimiter(defaultRatePerSec, defaultBurst),
		Now:         time.Now,
		Logger:      slog.Default(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.RateLimiter == nil {
		cfg.RateLimiter = auth.NewIPRateLimiter(defaultRatePerSec, defaultBurst)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &server{
		db:      db,
		pepper:  pepper,
		limiter: cfg.RateLimiter,
		now:     cfg.Now,
		logger:  cfg.Logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /onboarding", s.rateLimit(s.handleOnboarding))
	mux.HandleFunc("POST /sessions", s.rateLimit(s.handleCreateSession))
	mux.HandleFunc("DELETE /sessions", s.requireAuth(s.handleDeleteSession))
	mux.HandleFunc("GET /me", s.requireAuth(s.handleGetMe))
	mux.HandleFunc("PATCH /me", s.requireAuth(s.handlePatchMe))

	return s.logRequests(mux)
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
