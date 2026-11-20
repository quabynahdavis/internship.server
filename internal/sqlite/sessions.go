package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session represents an authenticated bearer token session stored as a SHA-256 hash.
type Session struct {
	TokenHash string
	StudentID int64
	ExpiresAt time.Time
	CreatedAt string
}

// CreateSession inserts a new session row with expiresAt formatted as UTC RFC3339.
func CreateSession(ctx context.Context, db *sql.DB, studentID int64, tokenHash string, expiresAt time.Time) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}
	if tokenHash == "" {
		return errors.New("session token hash is empty")
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, student_id, expires_at)
		VALUES (?, ?, ?)
	`, tokenHash, studentID, expiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	return nil
}

// GetStudentBySessionTokenHash joins sessions and students, verifies expires_at > now,
// and returns the authenticated Student or ErrNotFound if missing or expired.
func GetStudentBySessionTokenHash(ctx context.Context, db *sql.DB, tokenHash string, now time.Time) (Student, error) {
	if db == nil {
		return Student{}, errors.New("SQLite database is nil")
	}
	if tokenHash == "" {
		return Student{}, ErrNotFound
	}

	nowUTC := now.UTC()
	var (
		student       Student
		nullYear      sql.NullInt64
		nullInst      sql.NullString
		skillsJSON    string
		interestsJSON string
		expiresAtRaw  string
	)

	err := db.QueryRowContext(ctx, `
		SELECT
			st.id, st.email, st.full_name, st.program, st.year_of_study,
			st.institution, st.city, st.country, st.student_id,
			st.skills, st.interests, st.created_at, st.updated_at,
			se.expires_at
		FROM sessions se
		INNER JOIN students st ON st.id = se.student_id
		WHERE se.token_hash = ? AND se.expires_at > ?
	`, tokenHash, nowUTC.Format(time.RFC3339)).Scan(
		&student.ID,
		&student.Email,
		&student.FullName,
		&student.Program,
		&nullYear,
		&nullInst,
		&student.City,
		&student.Country,
		&student.StudentID,
		&skillsJSON,
		&interestsJSON,
		&student.CreatedAt,
		&student.UpdatedAt,
		&expiresAtRaw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Student{}, ErrNotFound
		}
		return Student{}, fmt.Errorf("query session student: %w", err)
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtRaw)
	if err != nil || !expiresAt.After(nowUTC) {
		return Student{}, ErrNotFound
	}

	if err := populateStudentFields(&student, nullYear, nullInst, skillsJSON, interestsJSON); err != nil {
		return Student{}, err
	}

	return student, nil
}

// DeleteSession revokes a session by its token hash.
func DeleteSession(ctx context.Context, db *sql.DB, tokenHash string) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

// DeleteExpiredSessions removes all session rows with expires_at <= now.
func DeleteExpiredSessions(ctx context.Context, db *sql.DB, now time.Time) error {
	if db == nil {
		return errors.New("SQLite database is nil")
	}

	if _, err := db.ExecContext(
		ctx,
		"DELETE FROM sessions WHERE expires_at <= ?",
		now.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}

	return nil
}
