package auth_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"internship/server/internal/auth"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "correct-horse-battery" {
		t.Fatal("HashPassword() returned plaintext password")
	}

	if err := auth.VerifyPassword(hash, "correct-horse-battery"); err != nil {
		t.Errorf("VerifyPassword() with matching password failed: %v", err)
	}
	if err := auth.VerifyPassword(hash, "wrong-password-123"); err == nil {
		t.Error("VerifyPassword() succeeded with wrong password")
	}
}

func TestPasswordLengthBounds(t *testing.T) {
	t.Parallel()

	if _, err := auth.HashPassword("1234567"); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("HashPassword(7 bytes) error = %v, want %v", err, auth.ErrPasswordTooShort)
	}

	if _, err := auth.HashPassword("12345678"); err != nil {
		t.Errorf("HashPassword(8 bytes) unexpected error: %v", err)
	}

	maxPW := strings.Repeat("a", 72)
	hash72, err := auth.HashPassword(maxPW)
	if err != nil {
		t.Fatalf("HashPassword(72 bytes) unexpected error: %v", err)
	}
	if err := auth.VerifyPassword(hash72, maxPW); err != nil {
		t.Errorf("VerifyPassword(72 bytes) unexpected error: %v", err)
	}
	// Ensure 73-byte password sharing the same 72-byte prefix does NOT match.
	if err := auth.VerifyPassword(hash72, maxPW+"b"); err == nil {
		t.Error("VerifyPassword(73 bytes) matched 72-byte prefix hash")
	}

	if _, err := auth.HashPassword(strings.Repeat("a", 73)); !errors.Is(err, auth.ErrPasswordTooLong) {
		t.Errorf("HashPassword(73 bytes) error = %v, want %v", err, auth.ErrPasswordTooLong)
	}
}

func TestVerifyDummyPasswordDoesNotPanic(t *testing.T) {
	t.Parallel()

	auth.VerifyDummyPassword("some-password")
	auth.VerifyDummyPassword("")
	auth.VerifyDummyPassword(strings.Repeat("x", 200))
}

func TestHashNationalID(t *testing.T) {
	t.Parallel()

	const pepper = "unit-test-pepper"
	const rawID = "39485712"

	got, err := auth.HashNationalID(pepper, "  39485712 \n\t ")
	if err != nil {
		t.Fatalf("HashNationalID() unexpected error: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(rawID))
	want := hex.EncodeToString(mac.Sum(nil))

	if got != want {
		t.Errorf("HashNationalID() = %q, want %q", got, want)
	}
	if strings.Contains(got, rawID) {
		t.Error("HashNationalID() output contains raw national ID")
	}

	// Different pepper produces different digest.
	other, err := auth.HashNationalID("different-pepper", rawID)
	if err != nil {
		t.Fatalf("HashNationalID() unexpected error: %v", err)
	}
	if other == got {
		t.Error("HashNationalID() produced identical output for different peppers")
	}

	if _, err := auth.HashNationalID("", rawID); !errors.Is(err, auth.ErrEmptyPepper) {
		t.Errorf("HashNationalID(empty pepper) error = %v, want %v", err, auth.ErrEmptyPepper)
	}
	if _, err := auth.HashNationalID("   ", rawID); !errors.Is(err, auth.ErrEmptyPepper) {
		t.Errorf("HashNationalID(whitespace pepper) error = %v, want %v", err, auth.ErrEmptyPepper)
	}
	if _, err := auth.HashNationalID(pepper, "   "); !errors.Is(err, auth.ErrEmptyNationalID) {
		t.Errorf("HashNationalID(whitespace ID) error = %v, want %v", err, auth.ErrEmptyNationalID)
	}
}

func TestNewSessionToken(t *testing.T) {
	t.Parallel()

	raw1, hash1, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken() error = %v", err)
	}
	raw2, hash2, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken() second call error = %v", err)
	}

	if raw1 == "" || hash1 == "" {
		t.Fatal("NewSessionToken() returned empty token or hash")
	}
	if raw1 == raw2 || hash1 == hash2 {
		t.Fatal("NewSessionToken() returned duplicate tokens")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw1)
	if err != nil {
		t.Fatalf("base64.RawURLEncoding.DecodeString() error = %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("decoded token length = %d, want 32", len(decoded))
	}

	wantHash := auth.HashSessionToken(raw1)
	if hash1 != wantHash {
		t.Errorf("tokenHash = %q, want %q", hash1, wantHash)
	}
	if len(hash1) != 64 {
		t.Errorf("tokenHash hex length = %d, want 64", len(hash1))
	}
}

func TestIPRateLimiterBurstRefillAndPruning(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}

	limiter := auth.NewIPRateLimiter(0.5, 3) // 1 token every 2s, burst 3
	limiter.SetClock(clock)
	limiter.SetMaxEntries(4)

	req := httptest.NewRequest("POST", "/sessions", nil)
	req.RemoteAddr = "203.0.113.10:54321"

	for i := range 3 {
		if !limiter.AllowRequest(req) {
			t.Fatalf("request %d blocked before burst exhausted", i+1)
		}
	}
	if limiter.AllowRequest(req) {
		t.Fatal("4th request allowed when burst exhausted")
	}

	// Different IP has an independent bucket.
	otherReq := httptest.NewRequest("POST", "/sessions", nil)
	otherReq.RemoteAddr = "198.51.100.7" // fallback without port
	if !limiter.AllowRequest(otherReq) {
		t.Fatal("request from distinct IP without port should be allowed")
	}

	// Advance 2 seconds -> 1 token refilled for first IP.
	advance(2 * time.Second)
	if !limiter.AllowRequest(req) {
		t.Fatal("request should be allowed after token refill")
	}
	if limiter.AllowRequest(req) {
		t.Fatal("second immediate request after 1-token refill should be blocked")
	}

	// Exercise bounded eviction and concurrent safety.
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/onboarding", nil)
			r.RemoteAddr = "192.0.2." + string(rune('0'+(idx%10))) + ":1234"
			_ = limiter.AllowRequest(r)
		}(i)
	}
	wg.Wait()
}
