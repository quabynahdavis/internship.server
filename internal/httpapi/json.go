package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	appsqlite "internship/server/internal/sqlite"
)

const (
	maxRequestBodyBytes = 64 << 10 // 64 KiB
	maxStringChars      = 200
	maxEmailChars       = 254
	maxArrayItems       = 50
	maxArrayItemChars   = 100
	minYearOfStudy      = 1
	maxYearOfStudy      = 10
)

type authResponse struct {
	Token     string            `json:"token"`
	ExpiresAt string            `json:"expires_at"`
	Student   appsqlite.Student `json:"student"`
}

// optional distinguishes an omitted JSON field from an explicit JSON null or value.
type optional[T any] struct {
	Set   bool
	Valid bool
	Value T
}

func (o *optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Valid = false
		return nil
	}
	if err := json.Unmarshal(data, &o.Value); err != nil {
		return err
	}
	o.Valid = true
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeJSONBody enforces Content-Type: application/json, a 64 KiB body cap,
// unknown-field rejection, and single-JSON-object validation.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}

	limited := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds 64 KiB limit")
			return false
		}
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return false
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		writeError(w, http.StatusBadRequest, "request body must not be empty")
		return false
	}
	if trimmed[0] != '{' {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return false
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			writeError(w, http.StatusBadRequest, "request body contains unknown field")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return false
	}

	return true
}

func validateEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", errors.New("email is required")
	}
	if len(email) > maxEmailChars {
		return "", errors.New("email must not exceed 254 characters")
	}
	if strings.ContainsAny(email, " \t\r\n") {
		return "", errors.New("invalid email format")
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || !strings.EqualFold(addr.Address, email) {
		return "", errors.New("invalid email format")
	}

	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", errors.New("invalid email format")
	}

	return email, nil
}

func validateRequiredString(field, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(trimmed) > maxStringChars {
		return "", fmt.Errorf("%s must not exceed %d characters", field, maxStringChars)
	}
	return trimmed, nil
}

func validateStringList(field string, items []string) ([]string, error) {
	if len(items) > maxArrayItems {
		return nil, fmt.Errorf("%s must not exceed %d items", field, maxArrayItems)
	}
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			return nil, fmt.Errorf("%s items must not be empty", field)
		}
		if utf8.RuneCountInString(trimmed) > maxArrayItemChars {
			return nil, fmt.Errorf("%s items must not exceed %d characters", field, maxArrayItemChars)
		}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned, nil
}

func validateYearOfStudy(year int) error {
	if year < minYearOfStudy || year > maxYearOfStudy {
		return fmt.Errorf("year_of_study must be between %d and %d", minYearOfStudy, maxYearOfStudy)
	}
	return nil
}
