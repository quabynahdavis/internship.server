package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	appsqlite "internship/server/internal/sqlite"
)

func TestOpenAppliesMigrationAndSettings(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	for _, table := range []string{
		"students",
		"sessions",
		"companies",
		"preferences",
		"schema_migrations",
	} {
		var count int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query table %q: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %q count = %d, want 1", table, count)
		}
	}

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := appsqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	db, err = appsqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close second database: %v", err)
		}
	})

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("migration count = %d, want 1", count)
	}
}

func TestStudentConstraints(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	insertStudent(t, db, "student@example.com", "national-hash")

	if _, err := db.Exec(`
		INSERT INTO students (
			email, password_hash, full_name, program, city, country, student_id, national_id_hash
		) VALUES (?, 'password-hash', 'Duplicate', 'Engineering', 'Nairobi', 'Kenya', 'S002', 'other-hash')
	`, "student@example.com"); err == nil {
		t.Error("duplicate student email succeeded")
	}

	if _, err := db.Exec(`
		INSERT INTO students (
			email, password_hash, full_name, program, city, country, student_id, national_id_hash
		) VALUES ('other@example.com', 'password-hash', 'Duplicate', 'Engineering', 'Nairobi', 'Kenya', 'S003', ?)
	`, "national-hash"); err == nil {
		t.Error("duplicate national ID hash succeeded")
	}

	if _, err := db.Exec(`
		INSERT INTO students (
			email, password_hash, full_name, program, city, country, student_id,
			national_id_hash, skills
		) VALUES (
			'json@example.com', 'password-hash', 'Invalid JSON', 'Engineering',
			'Nairobi', 'Kenya', 'S004', 'json-hash', 'not-json'
		)
	`); err == nil {
		t.Error("invalid skills JSON succeeded")
	}
}

func TestSessionForeignKeyAndCascade(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if _, err := db.Exec(`
		INSERT INTO sessions (token_hash, student_id, expires_at)
		VALUES ('orphan-token', 999, '2030-01-01T00:00:00Z')
	`); err == nil {
		t.Error("session with unknown student succeeded")
	}

	studentID := insertStudent(t, db, "session@example.com", "session-national-hash")
	if _, err := db.Exec(`
		INSERT INTO sessions (token_hash, student_id, expires_at)
		VALUES ('token-hash', ?, '2030-01-01T00:00:00Z')
	`, studentID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := db.Exec("DELETE FROM students WHERE id = ?", studentID); err != nil {
		t.Fatalf("delete student: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Errorf("session count after student deletion = %d, want 0", count)
	}
}

func TestPreferenceConstraints(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	studentID := insertStudent(t, db, "preference@example.com", "preference-national-hash")
	companyID := insertCompany(t, db)

	if _, err := db.Exec(`
		INSERT INTO preferences (student_id, company_id, signal, reason)
		VALUES (?, ?, 'maybe', NULL)
	`, studentID, companyID); err == nil {
		t.Error("invalid preference signal succeeded")
	}
	if _, err := db.Exec(`
		INSERT INTO preferences (student_id, company_id, signal, reason)
		VALUES (?, ?, 'dislike', 'salary')
	`, studentID, companyID); err == nil {
		t.Error("invalid preference reason succeeded")
	}
	if _, err := db.Exec(`
		INSERT INTO preferences (student_id, company_id, signal, reason)
		VALUES (?, ?, 'like', NULL)
	`, studentID, companyID); err != nil {
		t.Fatalf("insert preference: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO preferences (student_id, company_id, signal, reason)
		VALUES (?, ?, 'dislike', 'industry')
	`, studentID, companyID); err == nil {
		t.Error("duplicate student/company preference succeeded")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := appsqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	return db
}

func insertStudent(t *testing.T, db *sql.DB, email, nationalIDHash string) int64 {
	t.Helper()

	result, err := db.Exec(`
		INSERT INTO students (
			email, password_hash, full_name, program, city, country, student_id, national_id_hash
		) VALUES (?, 'password-hash', 'Test Student', 'Engineering', 'Nairobi', 'Kenya', 'S001', ?)
	`, email, nationalIDHash)
	if err != nil {
		t.Fatalf("insert student: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get student ID: %v", err)
	}

	return id
}

func insertCompany(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	result, err := db.Exec(`
		INSERT INTO companies (name, industry, city, country, summary)
		VALUES ('Test Company', 'Technology', 'Nairobi', 'Kenya', 'A test company')
	`)
	if err != nil {
		t.Fatalf("insert company: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get company ID: %v", err)
	}

	return id
}
