package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"internship/server/internal/auth"
	appsqlite "internship/server/internal/sqlite"
)

const maxBearerTokenBytes = 512

type contextKey int

const (
	studentContextKey contextKey = iota + 1
	tokenHashContextKey
)

func studentFromContext(ctx context.Context) (appsqlite.Student, bool) {
	student, ok := ctx.Value(studentContextKey).(appsqlite.Student)
	return student, ok
}

func tokenHashFromContext(ctx context.Context) (string, bool) {
	tokenHash, ok := ctx.Value(tokenHashContextKey).(string)
	return tokenHash, ok
}

func (s *server) rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.limiter != nil && !s.limiter.AllowRequest(r) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next(w, r)
	}
}

func (s *server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawToken, ok := extractBearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		tokenHash := auth.HashSessionToken(rawToken)
		student, err := appsqlite.GetStudentBySessionTokenHash(r.Context(), s.db, tokenHash, s.now())
		if err != nil {
			if errors.Is(err, appsqlite.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			s.logger.Error("authenticate session failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		ctx := context.WithValue(r.Context(), studentContextKey, student)
		ctx = context.WithValue(ctx, tokenHashContextKey, tokenHash)
		next(w, r.WithContext(ctx))
	}
}

func extractBearerToken(authHeader string) (string, bool) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(authHeader), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(rest)
	if token == "" || len(token) > maxBearerTokenBytes || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// logRequests logs only safe request metadata (method, path, status, duration_ms).
// Request bodies, query strings, and headers (such as Authorization) are never logged.
func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(rec, r)

		s.logger.Info(
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
