// Package auth provides password hashing, national ID HMAC hashing,
// session token generation, and per-IP rate limiting.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordBytes is the minimum allowed password byte length.
	MinPasswordBytes = 8
	// MaxPasswordBytes is the maximum allowed password byte length enforced by bcrypt.
	MaxPasswordBytes = 72
	// sessionTokenBytes is the number of random bytes per opaque session token.
	sessionTokenBytes = 32
)

var (
	// ErrPasswordTooShort indicates a password shorter than MinPasswordBytes.
	ErrPasswordTooShort = errors.New("password must be at least 8 bytes")
	// ErrPasswordTooLong indicates a password longer than MaxPasswordBytes.
	ErrPasswordTooLong = errors.New("password must not exceed 72 bytes")
	// ErrInvalidCredentials indicates a password verification mismatch.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrEmptyPepper indicates a missing HMAC pepper for national ID hashing.
	ErrEmptyPepper = errors.New("national ID pepper is required")
	// ErrEmptyNationalID indicates an empty national ID after trimming whitespace.
	ErrEmptyNationalID = errors.New("national ID is required")
)

var dummyPasswordHash []byte

func init() {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy-timing-mitigation-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("generate dummy bcrypt hash: %v", err))
	}
	dummyPasswordHash = hash
}

// HashPassword validates password byte length (8..72 bytes) and returns a bcrypt hash.
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordBytes {
		return "", ErrPasswordTooShort
	}
	if len(password) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hashed), nil
}

// VerifyPassword checks password against a stored bcrypt hash.
// Passwords outside 8..72 bytes still run bcrypt comparison against a clamped slice
// to avoid length-based timing differences while always failing verification.
func VerifyPassword(hash, password string) error {
	pwBytes := []byte(password)
	invalidLen := len(pwBytes) < MinPasswordBytes || len(pwBytes) > MaxPasswordBytes
	if len(pwBytes) > MaxPasswordBytes {
		pwBytes = pwBytes[:MaxPasswordBytes]
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), pwBytes); err != nil {
		return ErrInvalidCredentials
	}
	if invalidLen {
		return ErrInvalidCredentials
	}

	return nil
}

// VerifyDummyPassword performs a constant-cost bcrypt comparison against a
// precomputed hash so unknown-email login attempts take similar time to real accounts.
func VerifyDummyPassword(password string) {
	pwBytes := []byte(password)
	if len(pwBytes) > MaxPasswordBytes {
		pwBytes = pwBytes[:MaxPasswordBytes]
	}
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, pwBytes)
}

// HashNationalID trims surrounding whitespace on rawNationalID and computes
// hex-encoded HMAC-SHA256(pepper, trimmedNationalID). Raw national IDs are never included in errors.
func HashNationalID(pepper, rawNationalID string) (string, error) {
	if strings.TrimSpace(pepper) == "" {
		return "", ErrEmptyPepper
	}
	trimmed := strings.TrimSpace(rawNationalID)
	if trimmed == "" {
		return "", ErrEmptyNationalID
	}

	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(trimmed))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// NewSessionToken generates 32 cryptographic random bytes, encodes rawToken using
// URL-safe unpadded base64, and returns both rawToken and its SHA-256 hex digest.
func NewSessionToken() (rawToken string, tokenHash string, err error) {
	var buf [sessionTokenBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", fmt.Errorf("generate random session token: %w", err)
	}

	rawToken = base64.RawURLEncoding.EncodeToString(buf[:])
	tokenHash = HashSessionToken(rawToken)
	return rawToken, tokenHash, nil
}

// HashSessionToken computes hex(sha256(rawToken)).
func HashSessionToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
