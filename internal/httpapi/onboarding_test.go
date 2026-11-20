package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	appsqlite "internship/server/internal/sqlite"
)

func TestOnboardingSuccessAndZeroRawNationalIDLeakage(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	const rawNationalID = "KE-SECRET-ID-77441122"
	const rawPassword = "onboarding-strong-pass!"

	rec := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", map[string]any{
		"email":         "  faith.njeri@example.com ",
		"password":      rawPassword,
		"full_name":     "Faith Njeri",
		"program":       "Computer Science",
		"year_of_study": 3,
		"institution":   "Strathmore University",
		"city":          "Nairobi",
		"country":       "Kenya",
		"student_id":    "SU-109283",
		"national_id":   "  " + rawNationalID + " \n",
		"skills":        []string{" Go ", "PostgreSQL"},
		"interests":     []string{"Payments", "AI"},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /onboarding status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}

	respBytes := rec.Body.Bytes()
	respStr := string(respBytes)

	// Verify neither raw national_id nor raw password nor national_id key appears in HTTP response.
	for _, forbidden := range []string{rawNationalID, rawPassword, "national_id", "password_hash"} {
		if strings.Contains(respStr, forbidden) {
			t.Errorf("onboarding HTTP response leaked %q: %s", forbidden, respStr)
		}
	}

	var resp struct {
		Token     string            `json:"token"`
		ExpiresAt string            `json:"expires_at"`
		Student   appsqlite.Student `json:"student"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("decode onboarding response: %v", err)
	}
	if resp.Token == "" || resp.ExpiresAt == "" {
		t.Fatalf("expected non-empty token and expires_at, got %+v", resp)
	}
	if resp.Student.Email != "faith.njeri@example.com" {
		t.Errorf("student.Email = %q, want %q", resp.Student.Email, "faith.njeri@example.com")
	}
	if len(resp.Student.Skills) != 2 || resp.Student.Skills[0] != "Go" {
		t.Errorf("student.Skills = %#v, want [Go PostgreSQL]", resp.Student.Skills)
	}

	// Verify captured structured logs contain neither raw national_id, password, nor bearer token.
	logStr := env.logs.String()
	for _, forbidden := range []string{rawNationalID, rawPassword, resp.Token} {
		if strings.Contains(logStr, forbidden) {
			t.Errorf("captured log output leaked %q: %s", forbidden, logStr)
		}
	}
	if !strings.Contains(logStr, `"/onboarding"`) {
		t.Errorf("expected request log for /onboarding, got: %s", logStr)
	}

	// Verify raw SQLite rows and raw SQLite file bytes on disk do NOT contain rawNationalID or rawPassword.
	_, _ = env.db.ExecContext(context.Background(), "PRAGMA wal_checkpoint(FULL)")
	fileBytes, err := os.ReadFile(env.dbPath)
	if err != nil {
		t.Fatalf("read SQLite file: %v", err)
	}
	if bytes.Contains(fileBytes, []byte(rawNationalID)) {
		t.Fatal("SQLite database file contains raw national_id")
	}
	if bytes.Contains(fileBytes, []byte(rawPassword)) {
		t.Fatal("SQLite database file contains raw password")
	}
}

func TestOnboardingDuplicateEmailAndDuplicateNationalID(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	basePayload := func() map[string]any {
		return map[string]any{
			"email":       "first@example.com",
			"password":    "first-password-123",
			"full_name":   "First Student",
			"program":     "Information Technology",
			"city":        "Nairobi",
			"country":     "Kenya",
			"student_id":  "IT-001",
			"national_id": "SHARED-NATID-5555",
		}
	}

	first := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", basePayload())
	if first.Code != http.StatusCreated {
		t.Fatalf("first onboarding status = %d, body = %s", first.Code, first.Body.String())
	}

	// Duplicate email (with different national_id) -> 409 Conflict.
	dupEmail := basePayload()
	dupEmail["email"] = "FIRST@example.com"
	dupEmail["national_id"] = "DIFFERENT-NATID-9999"
	dupEmailRec := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", dupEmail)
	if dupEmailRec.Code != http.StatusConflict {
		t.Errorf("duplicate email status = %d, want 409 (body=%s)", dupEmailRec.Code, dupEmailRec.Body.String())
	}

	// Second registration with same national_id (even with whitespace padding & different email) -> 409 Conflict.
	dupNatID := basePayload()
	dupNatID["email"] = "second@example.com"
	dupNatID["national_id"] = "  SHARED-NATID-5555  "
	dupNatIDRec := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", dupNatID)
	if dupNatIDRec.Code != http.StatusConflict {
		t.Errorf("duplicate national_id status = %d, want 409 (body=%s)", dupNatIDRec.Code, dupNatIDRec.Body.String())
	}
	if strings.Contains(dupNatIDRec.Body.String(), "SHARED-NATID-5555") {
		t.Errorf("conflict response echoed raw national_id: %s", dupNatIDRec.Body.String())
	}
}

func TestOnboardingValidationAndRequestSecurityControls(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	// 1. Wrong Content-Type -> 415 Unsupported Media Type.
	wrongCTReq := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader(`{}`))
	wrongCTReq.Header.Set("Content-Type", "text/plain")
	wrongCTRec := httptest.NewRecorder()
	env.handler.ServeHTTP(wrongCTRec, wrongCTReq)
	if wrongCTRec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("wrong Content-Type status = %d, want 415", wrongCTRec.Code)
	}

	// 2. Missing Content-Type -> 415 Unsupported Media Type.
	noCTReq := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader(`{}`))
	noCTRec := httptest.NewRecorder()
	env.handler.ServeHTTP(noCTRec, noCTReq)
	if noCTRec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("missing Content-Type status = %d, want 415", noCTRec.Code)
	}

	// 3. Oversized body (> 64 KiB) -> 413 Request Entity Too Large.
	largeJSON := `{"email":"big@example.com","full_name":"` + strings.Repeat("a", 70*1024) + `"}`
	largeReq := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader(largeJSON))
	largeReq.Header.Set("Content-Type", "application/json")
	largeRec := httptest.NewRecorder()
	env.handler.ServeHTTP(largeRec, largeReq)
	if largeRec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want 413", largeRec.Code)
	}

	// 4. Malformed JSON, empty body, top-level array/null, and trailing JSON tokens -> 400 Bad Request.
	for name, raw := range map[string]string{
		"empty":         "",
		"malformed":     `{"email":`,
		"null_root":     `null`,
		"array_root":    `[]`,
		"trailing_json": `{"email":"a@b.com"}{"extra":true}`,
		"trailing_junk": `{"email":"a@b.com"} trailing`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/onboarding", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("raw case %q status = %d, want 400", name, rec.Code)
		}
	}

	// 5. Unknown fields and field validation failures -> 400 Bad Request.
	validBase := func() map[string]any {
		return map[string]any{
			"email":       "valid@example.com",
			"password":    "valid-password-123",
			"full_name":   "Valid User",
			"program":     "Computer Science",
			"city":        "Nairobi",
			"country":     "Kenya",
			"student_id":  "S-100",
			"national_id": "NAT-100",
		}
	}

	tooManySkills := make([]string, 51)
	for i := range tooManySkills {
		tooManySkills[i] = "skill"
	}

	cases := map[string]func(map[string]any){
		"unknown_field":       func(m map[string]any) { m["is_admin"] = true },
		"missing_email":       func(m map[string]any) { delete(m, "email") },
		"invalid_email":       func(m map[string]any) { m["email"] = "not-an-email" },
		"missing_password":    func(m map[string]any) { delete(m, "password") },
		"short_password":      func(m map[string]any) { m["password"] = "short7!" },
		"long_password":       func(m map[string]any) { m["password"] = strings.Repeat("p", 73) },
		"missing_full_name":   func(m map[string]any) { m["full_name"] = "   " },
		"missing_program":     func(m map[string]any) { delete(m, "program") },
		"missing_city":        func(m map[string]any) { delete(m, "city") },
		"missing_country":     func(m map[string]any) { delete(m, "country") },
		"missing_student_id":  func(m map[string]any) { delete(m, "student_id") },
		"missing_national_id": func(m map[string]any) { m["national_id"] = "   " },
		"year_out_of_range":   func(m map[string]any) { m["year_of_study"] = 12 },
		"empty_institution":   func(m map[string]any) { m["institution"] = "   " },
		"too_many_skills":     func(m map[string]any) { m["skills"] = tooManySkills },
		"oversized_skill":     func(m map[string]any) { m["skills"] = []string{strings.Repeat("s", 101)} },
	}

	for name, mutate := range cases {
		payload := validBase()
		mutate(payload)
		rec := doJSONRequest(t, env.handler, http.MethodPost, "/onboarding", "", payload)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("validation case %q status = %d, want 400 (body=%s)", name, rec.Code, rec.Body.String())
		}
	}
}
