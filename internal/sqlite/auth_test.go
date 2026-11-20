package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"internship/server/internal/auth"
	appsqlite "internship/server/internal/sqlite"
)

func TestCreateGetAndUpdateStudentAndRawNationalIDAbsence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "auth_test.db")
	db, err := appsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	const rawNationalID = "KE-NATID-99887766"
	const rawPassword = "super-secret-password"
	nationalIDHash, err := auth.HashNationalID("test-pepper", rawNationalID)
	if err != nil {
		t.Fatalf("HashNationalID() error = %v", err)
	}
	passwordHash, err := auth.HashPassword(rawPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	year := 3
	inst := "University of Nairobi"
	created, err := appsqlite.CreateStudent(ctx, db, appsqlite.CreateStudentParams{
		Email:          "amina@example.com",
		PasswordHash:   passwordHash,
		FullName:       "Amina Otieno",
		Program:        "Computer Science",
		YearOfStudy:    &year,
		Institution:    &inst,
		City:           "Nairobi",
		Country:        "Kenya",
		StudentID:      "CS/2024/001",
		NationalIDHash: nationalIDHash,
		Skills:         nil, // should be marshaled as []
		Interests:      []string{"Backend", "Fintech"},
	})
	if err != nil {
		t.Fatalf("CreateStudent() error = %v", err)
	}

	if created.ID <= 0 {
		t.Errorf("created.ID = %d, want > 0", created.ID)
	}
	if created.Skills == nil || len(created.Skills) != 0 {
		t.Errorf("created.Skills = %#v, want empty non-nil slice", created.Skills)
	}
	if len(created.Interests) != 2 || created.Interests[0] != "Backend" {
		t.Errorf("created.Interests = %#v, want [Backend Fintech]", created.Interests)
	}

	// Ensure Student JSON serialization never exposes password_hash, national_id, or national_id_hash.
	encodedJSON, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("json.Marshal(Student) error = %v", err)
	}
	jsonStr := string(encodedJSON)
	for _, forbidden := range []string{rawNationalID, nationalIDHash, rawPassword, passwordHash, "national_id", "password_hash"} {
		if strings.Contains(jsonStr, forbidden) {
			t.Errorf("serialized Student JSON leaked %q: %s", forbidden, jsonStr)
		}
	}

	// Inspect every column of students table directly to guarantee rawNationalID does NOT appear anywhere.
	var colEmail, colPWHash, colName, colProgram, colCity, colCountry, colStudentID, colNatHash, colSkills, colInterests string
	var colInst *string
	err = db.QueryRowContext(ctx, `
		SELECT email, password_hash, full_name, program, institution,
		       city, country, student_id, national_id_hash, skills, interests
		FROM students WHERE id = ?
	`, created.ID).Scan(
		&colEmail, &colPWHash, &colName, &colProgram, &colInst,
		&colCity, &colCountry, &colStudentID, &colNatHash, &colSkills, &colInterests,
	)
	if err != nil {
		t.Fatalf("scan raw student columns: %v", err)
	}
	allCols := strings.Join([]string{
		colEmail, colPWHash, colName, colProgram, colCity, colCountry,
		colStudentID, colNatHash, colSkills, colInterests,
	}, "|")
	if colInst != nil {
		allCols += "|" + *colInst
	}
	if strings.Contains(allCols, rawNationalID) {
		t.Fatal("students table contains raw national ID")
	}
	if strings.Contains(allCols, rawPassword) {
		t.Fatal("students table contains raw password")
	}

	// Also verify raw database file on disk does not contain rawNationalID after WAL checkpoint.
	_, _ = db.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)")
	dbBytes, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read SQLite file: %v", err)
	}
	if strings.Contains(string(dbBytes), rawNationalID) {
		t.Fatal("raw SQLite file bytes contain raw national ID")
	}

	// Verify duplicate email maps to ErrDuplicateEmail.
	otherNatHash, _ := auth.HashNationalID("test-pepper", "OTHER-NATID-111")
	_, err = appsqlite.CreateStudent(ctx, db, appsqlite.CreateStudentParams{
		Email:          "amina@example.com",
		PasswordHash:   passwordHash,
		FullName:       "Duplicate Email",
		Program:        "Engineering",
		City:           "Nairobi",
		Country:        "Kenya",
		StudentID:      "CS/2024/002",
		NationalIDHash: otherNatHash,
	})
	if !errors.Is(err, appsqlite.ErrDuplicateEmail) {
		t.Errorf("duplicate email error = %v, want %v", err, appsqlite.ErrDuplicateEmail)
	}

	// Verify duplicate national_id_hash maps to ErrDuplicateNationalID.
	_, err = appsqlite.CreateStudent(ctx, db, appsqlite.CreateStudentParams{
		Email:          "different@example.com",
		PasswordHash:   passwordHash,
		FullName:       "Same Person",
		Program:        "Engineering",
		City:           "Nairobi",
		Country:        "Kenya",
		StudentID:      "CS/2024/003",
		NationalIDHash: nationalIDHash,
	})
	if !errors.Is(err, appsqlite.ErrDuplicateNationalID) {
		t.Errorf("duplicate national_id_hash error = %v, want %v", err, appsqlite.ErrDuplicateNationalID)
	}

	// Verify GetStudentByEmailForAuth and GetStudentByID.
	authedStudent, gotHash, err := appsqlite.GetStudentByEmailForAuth(ctx, db, "amina@example.com")
	if err != nil {
		t.Fatalf("GetStudentByEmailForAuth() error = %v", err)
	}
	if gotHash != passwordHash || authedStudent.ID != created.ID {
		t.Errorf("GetStudentByEmailForAuth() mismatch: id=%d, hash=%q", authedStudent.ID, gotHash)
	}

	if _, _, err := appsqlite.GetStudentByEmailForAuth(ctx, db, "missing@example.com"); !errors.Is(err, appsqlite.ErrNotFound) {
		t.Errorf("GetStudentByEmailForAuth(missing) error = %v, want ErrNotFound", err)
	}
	if _, err := appsqlite.GetStudentByID(ctx, db, 99999); !errors.Is(err, appsqlite.ErrNotFound) {
		t.Errorf("GetStudentByID(missing) error = %v, want ErrNotFound", err)
	}

	// Verify UpdateStudent partial updates and clearing nullable fields.
	newCity := "Mombasa"
	newSkills := []string{"Go", "SQLite"}
	updated, err := appsqlite.UpdateStudent(ctx, db, created.ID, appsqlite.UpdateStudentParams{
		City:             &newCity,
		ClearYearOfStudy: true,
		Skills:           &newSkills,
	})
	if err != nil {
		t.Fatalf("UpdateStudent() error = %v", err)
	}
	if updated.City != "Mombasa" {
		t.Errorf("updated.City = %q, want %q", updated.City, "Mombasa")
	}
	if updated.YearOfStudy != nil {
		t.Errorf("updated.YearOfStudy = %v, want nil", *updated.YearOfStudy)
	}
	if len(updated.Skills) != 2 || updated.Skills[0] != "Go" {
		t.Errorf("updated.Skills = %#v, want [Go SQLite]", updated.Skills)
	}
	if updated.Institution == nil || *updated.Institution != inst {
		t.Errorf("updated.Institution = %v, want %q", updated.Institution, inst)
	}
}

func TestSessionLifecycleLookupExpiryAndRevocation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t)

	student, err := appsqlite.CreateStudent(ctx, db, appsqlite.CreateStudentParams{
		Email:          "session-user@example.com",
		PasswordHash:   "hashed-pw",
		FullName:       "Session User",
		Program:        "Data Science",
		City:           "Kampala",
		Country:        "Uganda",
		StudentID:      "DS-100",
		NationalIDHash: "unique-nat-hash-100",
	})
	if err != nil {
		t.Fatalf("CreateStudent() error = %v", err)
	}

	baseNow := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	expiresAt := baseNow.Add(7 * 24 * time.Hour)

	_, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken() error = %v", err)
	}

	if err := appsqlite.CreateSession(ctx, db, student.ID, tokenHash, expiresAt); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// Valid before expiry.
	got, err := appsqlite.GetStudentBySessionTokenHash(ctx, db, tokenHash, baseNow.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetStudentBySessionTokenHash(valid) error = %v", err)
	}
	if got.ID != student.ID || got.Email != student.Email {
		t.Errorf("GetStudentBySessionTokenHash() got %+v, want %+v", got, student)
	}

	// Unknown token returns ErrNotFound.
	if _, err := appsqlite.GetStudentBySessionTokenHash(ctx, db, "nonexistent-hash", baseNow); !errors.Is(err, appsqlite.ErrNotFound) {
		t.Errorf("GetStudentBySessionTokenHash(unknown) error = %v, want ErrNotFound", err)
	}

	// Exact expiry timestamp and past expiry both return ErrNotFound.
	if _, err := appsqlite.GetStudentBySessionTokenHash(ctx, db, tokenHash, expiresAt); !errors.Is(err, appsqlite.ErrNotFound) {
		t.Errorf("GetStudentBySessionTokenHash(at expiry) error = %v, want ErrNotFound", err)
	}
	if _, err := appsqlite.GetStudentBySessionTokenHash(ctx, db, tokenHash, expiresAt.Add(time.Second)); !errors.Is(err, appsqlite.ErrNotFound) {
		t.Errorf("GetStudentBySessionTokenHash(after expiry) error = %v, want ErrNotFound", err)
	}

	// Revoking session makes it unavailable even before expiry.
	if err := appsqlite.DeleteSession(ctx, db, tokenHash); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := appsqlite.GetStudentBySessionTokenHash(ctx, db, tokenHash, baseNow); !errors.Is(err, appsqlite.ErrNotFound) {
		t.Errorf("GetStudentBySessionTokenHash(revoked) error = %v, want ErrNotFound", err)
	}

	// DeleteExpiredSessions cleans expired rows while keeping active ones.
	if err := appsqlite.CreateSession(ctx, db, student.ID, "expired-hash", baseNow.Add(-time.Minute)); err != nil {
		t.Fatalf("CreateSession(expired) error = %v", err)
	}
	if err := appsqlite.CreateSession(ctx, db, student.ID, "active-hash", baseNow.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(active) error = %v", err)
	}
	if err := appsqlite.DeleteExpiredSessions(ctx, db, baseNow); err != nil {
		t.Fatalf("DeleteExpiredSessions() error = %v", err)
	}
	if _, err := appsqlite.GetStudentBySessionTokenHash(ctx, db, "active-hash", baseNow); err != nil {
		t.Errorf("active session removed by DeleteExpiredSessions: %v", err)
	}
}
