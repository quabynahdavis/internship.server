package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"internship/server/internal/auth"
	"internship/server/internal/httpapi"
	appsqlite "internship/server/internal/sqlite"
)

const testPepper = "test-httpapi-national-id-pepper"

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testEnv struct {
	db      *sql.DB
	dbPath  string
	handler http.Handler
	logs    *syncBuffer
	mu      sync.Mutex
	now     time.Time
}

func newTestEnv(t *testing.T, extraOpts ...httpapi.Option) *testEnv {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "httpapi_test.db")
	db, err := appsqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	env := &testEnv{
		db:     db,
		dbPath: dbPath,
		logs:   &syncBuffer{},
		now:    time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}

	logger := slog.New(slog.NewJSONHandler(env.logs, nil))
	opts := []httpapi.Option{
		httpapi.WithClock(env.clock),
		httpapi.WithLogger(logger),
		httpapi.WithRateLimiter(auth.NewIPRateLimiter(100, 100)),
	}
	opts = append(opts, extraOpts...)

	env.handler = httpapi.NewHandler(db, testPepper, opts...)
	return env
}

func (e *testEnv) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *testEnv) advanceClock(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

func doJSONRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	token string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()

	var rawBody []byte
	if body != nil {
		var err error
		rawBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(rawBody))
	req.RemoteAddr = "198.51.100.42:12345"
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	env.handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
}

func TestHealthRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	request := httptest.NewRequest(http.MethodPost, "/health", nil)
	response := httptest.NewRecorder()
	env.handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestSessionsLoginGenericAuthErrorsAndLogout(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	// Register student first via POST /onboarding.
	onboardRec := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", map[string]any{
		"email":       "Student.Login@Example.com",
		"password":    "valid-pass-9876",
		"full_name":   "Brian Mwangi",
		"program":     "Software Engineering",
		"city":        "Nairobi",
		"country":     "Kenya",
		"student_id":  "SE-2026-10",
		"national_id": "NATID-LOGIN-10",
	})
	if onboardRec.Code != http.StatusCreated {
		t.Fatalf("onboarding status = %d, body = %s", onboardRec.Code, onboardRec.Body.String())
	}

	// Wrong password -> 401 with generic error.
	wrongPWRec := doJSONRequest(t, env.handler, http.MethodPost, "/sessions", "", map[string]any{
		"email":    "student.login@example.com",
		"password": "wrong-password-111",
	})
	if wrongPWRec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", wrongPWRec.Code)
	}

	// Unknown email -> 401 with identical generic error payload.
	unknownEmailRec := doJSONRequest(t, env.handler, http.MethodPost, "/sessions", "", map[string]any{
		"email":    "nobody@example.com",
		"password": "wrong-password-111",
	})
	if unknownEmailRec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown email status = %d, want 401", unknownEmailRec.Code)
	}
	if wrongPWRec.Body.String() != unknownEmailRec.Body.String() {
		t.Errorf("auth failure payloads differ:\nwrong pw: %s\nunknown email: %s",
			wrongPWRec.Body.String(), unknownEmailRec.Body.String())
	}

	var errBody map[string]string
	if err := json.Unmarshal(wrongPWRec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal 401 body: %v", err)
	}
	if errBody["error"] != "invalid credentials" {
		t.Errorf("error message = %q, want %q", errBody["error"], "invalid credentials")
	}

	// Valid login (case-insensitive email) -> 200 OK.
	loginRec := doJSONRequest(t, env.handler, http.MethodPost, "/sessions", "", map[string]any{
		"email":    "  STUDENT.LOGIN@example.com ",
		"password": "valid-pass-9876",
	})
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body = %s", loginRec.Code, loginRec.Body.String())
	}

	var loginResp struct {
		Token     string            `json:"token"`
		ExpiresAt string            `json:"expires_at"`
		Student   appsqlite.Student `json:"student"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResp.Token == "" {
		t.Fatal("login returned empty bearer token")
	}
	if loginResp.Student.Email != "student.login@example.com" {
		t.Errorf("student.Email = %q, want %q", loginResp.Student.Email, "student.login@example.com")
	}

	// Verify GET /me succeeds with issued session token.
	meRec := doJSONRequest(t, env.handler, http.MethodGet, "/me", loginResp.Token, nil)
	if meRec.Code != http.StatusOK {
		t.Fatalf("GET /me status = %d, want 200", meRec.Code)
	}

	// Revoke token via DELETE /sessions -> 204 No Content.
	delRec := doJSONRequest(t, env.handler, http.MethodDelete, "/sessions", loginResp.Token, nil)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /sessions status = %d, want 204", delRec.Code)
	}

	// Subsequent GET /me and DELETE /sessions with revoked token -> 401 Unauthorized.
	afterRevokeMe := doJSONRequest(t, env.handler, http.MethodGet, "/me", loginResp.Token, nil)
	if afterRevokeMe.Code != http.StatusUnauthorized {
		t.Errorf("GET /me after logout status = %d, want 401", afterRevokeMe.Code)
	}
	afterRevokeDel := doJSONRequest(t, env.handler, http.MethodDelete, "/sessions", loginResp.Token, nil)
	if afterRevokeDel.Code != http.StatusUnauthorized {
		t.Errorf("DELETE /sessions after logout status = %d, want 401", afterRevokeDel.Code)
	}

	// Ensure logs never leaked the password or bearer token.
	logged := env.logs.String()
	for _, secret := range []string{"valid-pass-9876", "wrong-password-111", loginResp.Token, "NATID-LOGIN-10"} {
		if strings.Contains(logged, secret) {
			t.Errorf("structured logs leaked secret %q: %s", secret, logged)
		}
	}
}

func TestMeAuthFailuresExpiryAndProfilePatch(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	// Missing token -> 401.
	if rec := doJSONRequest(t, env.handler, http.MethodGet, "/me", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /me without token status = %d, want 401", rec.Code)
	}
	if rec := doJSONRequest(t, env.handler, http.MethodPatch, "/me", "", map[string]any{"city": "Kigali"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("PATCH /me without token status = %d, want 401", rec.Code)
	}

	// Invalid token -> 401.
	if rec := doJSONRequest(t, env.handler, http.MethodGet, "/me", "invalid-random-token", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /me invalid token status = %d, want 401", rec.Code)
	}

	// Malformed Authorization header -> 401.
	badAuthReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	badAuthReq.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	badAuthRec := httptest.NewRecorder()
	env.handler.ServeHTTP(badAuthRec, badAuthReq)
	if badAuthRec.Code != http.StatusUnauthorized {
		t.Errorf("GET /me with Basic auth status = %d, want 401", badAuthRec.Code)
	}

	// Onboard a student.
	onboardRec := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", map[string]any{
		"email":          "grace@example.com",
		"password":       "grace-password-123",
		"full_name":      "Grace Wanjiku",
		"program":        "Electrical Engineering",
		"year_of_study":  2,
		"institution":    "JKUAT",
		"city":           "Juja",
		"country":        "Kenya",
		"student_id":     "EE-042",
		"national_id":    "NATID-GRACE-042",
		"skills":         []string{"Python", "Embedded C"},
		"interests":      []string{"Robotics"},
	})
	if onboardRec.Code != http.StatusCreated {
		t.Fatalf("onboarding status = %d, body = %s", onboardRec.Code, onboardRec.Body.String())
	}

	var onboardResp struct {
		Token   string            `json:"token"`
		Student appsqlite.Student `json:"student"`
	}
	if err := json.Unmarshal(onboardRec.Body.Bytes(), &onboardResp); err != nil {
		t.Fatalf("decode onboarding response: %v", err)
	}

	// PATCH /me valid partial update + clearing nullable institution.
	patchRec := doJSONRequest(t, env.handler, http.MethodPatch, "/me", onboardResp.Token, map[string]any{
		"program":       "Computer Engineering",
		"year_of_study": 4,
		"institution":   nil,
		"city":          "Nairobi",
		"country":       "Kenya",
		"skills":        []string{"Go", "Rust", "Linux"},
		"interests":     []string{"Systems", "Cloud"},
	})
	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH /me status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}

	var patched appsqlite.Student
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode PATCH /me response: %v", err)
	}
	if patched.Program != "Computer Engineering" || patched.City != "Nairobi" {
		t.Errorf("patched profile mismatch: %+v", patched)
	}
	if patched.YearOfStudy == nil || *patched.YearOfStudy != 4 {
		t.Errorf("patched YearOfStudy = %v, want 4", patched.YearOfStudy)
	}
	if patched.Institution != nil {
		t.Errorf("patched Institution = %v, want nil", *patched.Institution)
	}
	if len(patched.Skills) != 3 || patched.Skills[0] != "Go" {
		t.Errorf("patched Skills = %#v, want [Go Rust Linux]", patched.Skills)
	}

	// PATCH /me rejects forbidden / unknown fields (national_id, email, full_name) and bad values.
	for name, payload := range map[string]map[string]any{
		"patch_national_id": {"national_id": "NEW-ID-999"},
		"patch_email":       {"email": "new@example.com"},
		"patch_full_name":   {"full_name": "Changed Name"},
		"empty_city":        {"city": "   "},
		"null_city":         {"city": nil},
		"invalid_year_zero": {"year_of_study": 0},
		"invalid_year_high": {"year_of_study": 11},
		"empty_skill_item":  {"skills": []string{"Go", "  "}},
		"oversized_string":  {"program": strings.Repeat("x", 201)},
	} {
		rec := doJSONRequest(t, env.handler, http.MethodPatch, "/me", onboardResp.Token, payload)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH /me case %q status = %d, want 400 (body=%s)", name, rec.Code, rec.Body.String())
		}
	}

	// Advance clock past 7-day TTL -> token expires -> 401 Unauthorized.
	env.advanceClock(httpapi.SessionTTL + time.Second)
	expiredRec := doJSONRequest(t, env.handler, http.MethodGet, "/me", onboardResp.Token, nil)
	if expiredRec.Code != http.StatusUnauthorized {
		t.Errorf("GET /me with expired token status = %d, want 401", expiredRec.Code)
	}
}

func TestPerIPRateLimitingReturns429(t *testing.T) {
	t.Parallel()

	limiter := auth.NewIPRateLimiter(0, 2) // burst of 2, no refill
	env := newTestEnv(t, httpapi.WithRateLimiter(limiter))

	for i := range 2 {
		rec := doJSONRequest(t, env.handler, http.MethodPost, "/sessions", "", map[string]any{
			"email":    "ratelimit@example.com",
			"password": "some-password-123",
		})
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d unexpectedly rate-limited", i+1)
		}
	}

	rec := doJSONRequest(t, env.handler, http.MethodPost, "/sessions", "", map[string]any{
		"email":    "ratelimit@example.com",
		"password": "some-password-123",
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}
