package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrDuplicateEmail indicates a UNIQUE constraint violation on students.email.
	ErrDuplicateEmail = errors.New("duplicate student email")
	// ErrDuplicateNationalID indicates a UNIQUE constraint violation on students.national_id_hash.
	ErrDuplicateNationalID = errors.New("duplicate student national ID")
	// ErrNotFound indicates the requested student or session does not exist or is expired.
	ErrNotFound = errors.New("record not found")
)

// Student represents the public/authenticated student profile.
// Sensitive columns (password_hash, national_id_hash) are intentionally excluded.
type Student struct {
	ID          int64    `json:"id"`
	Email       string   `json:"email"`
	FullName    string   `json:"full_name"`
	Program     string   `json:"program"`
	YearOfStudy *int     `json:"year_of_study"`
	Institution *string  `json:"institution"`
	City        string   `json:"city"`
	Country     string   `json:"country"`
	StudentID   string   `json:"student_id"`
	Skills      []string `json:"skills"`
	Interests   []string `json:"interests"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// CreateStudentParams holds validated fields required to insert a new student record.
type CreateStudentParams struct {
	Email          string
	PasswordHash   string
	FullName       string
	Program        string
	YearOfStudy    *int
	Institution    *string
	City           string
	Country        string
	StudentID      string
	NationalIDHash string
	Skills         []string
	Interests      []string
}

// UpdateStudentParams holds optional partial-update fields for a student's profile.
type UpdateStudentParams struct {
	Program          *string
	YearOfStudy      *int
	ClearYearOfStudy bool
	Institution      *string
	ClearInstitution bool
	City             *string
	Country          *string
	Skills           *[]string
	Interests        *[]string
}

const studentSelectColumns = `
	id, email, full_name, program, year_of_study,
	institution, city, country, student_id,
	skills, interests, created_at, updated_at
`

// CreateStudent inserts a new student record and returns the persisted Student.
// Unique violations on email or national_id_hash map to ErrDuplicateEmail / ErrDuplicateNationalID.
func CreateStudent(ctx context.Context, db *sql.DB, params CreateStudentParams) (Student, error) {
	if db == nil {
		return Student{}, errors.New("SQLite database is nil")
	}

	skillsJSON, err := marshalStudentStringSlice(params.Skills)
	if err != nil {
		return Student{}, fmt.Errorf("marshal student skills: %w", err)
	}
	interestsJSON, err := marshalStudentStringSlice(params.Interests)
	if err != nil {
		return Student{}, fmt.Errorf("marshal student interests: %w", err)
	}

	var yearArg any
	if params.YearOfStudy != nil {
		yearArg = *params.YearOfStudy
	}
	var instArg any
	if params.Institution != nil {
		instArg = *params.Institution
	}

	row := db.QueryRowContext(ctx, `
		INSERT INTO students (
			email, password_hash, full_name, program, year_of_study,
			institution, city, country, student_id, national_id_hash,
			skills, interests
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING `+studentSelectColumns,
		params.Email,
		params.PasswordHash,
		params.FullName,
		params.Program,
		yearArg,
		instArg,
		params.City,
		params.Country,
		params.StudentID,
		params.NationalIDHash,
		skillsJSON,
		interestsJSON,
	)

	student, err := scanStudent(row)
	if err != nil {
		return Student{}, mapStudentConstraintError(err)
	}

	return student, nil
}

// GetStudentByEmailForAuth looks up a student and their stored password hash by email.
func GetStudentByEmailForAuth(ctx context.Context, db *sql.DB, email string) (Student, string, error) {
	if db == nil {
		return Student{}, "", errors.New("SQLite database is nil")
	}

	var (
		student       Student
		passwordHash  string
		nullYear      sql.NullInt64
		nullInst      sql.NullString
		skillsJSON    string
		interestsJSON string
	)

	err := db.QueryRowContext(ctx, `
		SELECT
			id, email, password_hash, full_name, program, year_of_study,
			institution, city, country, student_id,
			skills, interests, created_at, updated_at
		FROM students
		WHERE email = ?
	`, email).Scan(
		&student.ID,
		&student.Email,
		&passwordHash,
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
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Student{}, "", ErrNotFound
		}
		return Student{}, "", fmt.Errorf("query student by email: %w", err)
	}

	if err := populateStudentFields(&student, nullYear, nullInst, skillsJSON, interestsJSON); err != nil {
		return Student{}, "", err
	}

	return student, passwordHash, nil
}

// GetStudentByID fetches a student by primary key or returns ErrNotFound.
func GetStudentByID(ctx context.Context, db *sql.DB, id int64) (Student, error) {
	if db == nil {
		return Student{}, errors.New("SQLite database is nil")
	}

	row := db.QueryRowContext(ctx, `
		SELECT `+studentSelectColumns+`
		FROM students
		WHERE id = ?
	`, id)

	student, err := scanStudent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Student{}, ErrNotFound
		}
		return Student{}, fmt.Errorf("query student %d: %w", id, err)
	}

	return student, nil
}

// UpdateStudent applies partial profile updates for studentID, refreshes updated_at,
// and returns the updated Student or ErrNotFound.
func UpdateStudent(ctx context.Context, db *sql.DB, studentID int64, params UpdateStudentParams) (Student, error) {
	if db == nil {
		return Student{}, errors.New("SQLite database is nil")
	}

	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := make([]any, 0, 8)

	if params.Program != nil {
		setClauses = append(setClauses, "program = ?")
		args = append(args, *params.Program)
	}
	if params.ClearYearOfStudy {
		setClauses = append(setClauses, "year_of_study = NULL")
	} else if params.YearOfStudy != nil {
		setClauses = append(setClauses, "year_of_study = ?")
		args = append(args, *params.YearOfStudy)
	}
	if params.ClearInstitution {
		setClauses = append(setClauses, "institution = NULL")
	} else if params.Institution != nil {
		setClauses = append(setClauses, "institution = ?")
		args = append(args, *params.Institution)
	}
	if params.City != nil {
		setClauses = append(setClauses, "city = ?")
		args = append(args, *params.City)
	}
	if params.Country != nil {
		setClauses = append(setClauses, "country = ?")
		args = append(args, *params.Country)
	}
	if params.Skills != nil {
		skillsJSON, err := marshalStudentStringSlice(*params.Skills)
		if err != nil {
			return Student{}, fmt.Errorf("marshal updated skills: %w", err)
		}
		setClauses = append(setClauses, "skills = ?")
		args = append(args, skillsJSON)
	}
	if params.Interests != nil {
		interestsJSON, err := marshalStudentStringSlice(*params.Interests)
		if err != nil {
			return Student{}, fmt.Errorf("marshal updated interests: %w", err)
		}
		setClauses = append(setClauses, "interests = ?")
		args = append(args, interestsJSON)
	}

	args = append(args, studentID)
	query := `
		UPDATE students
		SET ` + strings.Join(setClauses, ", ") + `
		WHERE id = ?
		RETURNING ` + studentSelectColumns

	row := db.QueryRowContext(ctx, query, args...)
	student, err := scanStudent(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Student{}, ErrNotFound
		}
		return Student{}, fmt.Errorf("update student %d: %w", studentID, err)
	}

	return student, nil
}

type studentRowScanner interface {
	Scan(dest ...any) error
}

func scanStudent(scanner studentRowScanner) (Student, error) {
	var (
		student       Student
		nullYear      sql.NullInt64
		nullInst      sql.NullString
		skillsJSON    string
		interestsJSON string
	)

	if err := scanner.Scan(
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
	); err != nil {
		return Student{}, err
	}

	if err := populateStudentFields(&student, nullYear, nullInst, skillsJSON, interestsJSON); err != nil {
		return Student{}, err
	}

	return student, nil
}

func populateStudentFields(
	student *Student,
	nullYear sql.NullInt64,
	nullInst sql.NullString,
	skillsJSON string,
	interestsJSON string,
) error {
	if nullYear.Valid {
		year := int(nullYear.Int64)
		student.YearOfStudy = &year
	}
	if nullInst.Valid {
		inst := nullInst.String
		student.Institution = &inst
	}

	skills, err := unmarshalStudentStringSlice(skillsJSON)
	if err != nil {
		return fmt.Errorf("decode skills JSON for student %d: %w", student.ID, err)
	}
	interests, err := unmarshalStudentStringSlice(interestsJSON)
	if err != nil {
		return fmt.Errorf("decode interests JSON for student %d: %w", student.ID, err)
	}

	student.Skills = skills
	student.Interests = interests
	return nil
}

func mapStudentConstraintError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed: students.email") {
		return ErrDuplicateEmail
	}
	if strings.Contains(msg, "UNIQUE constraint failed: students.national_id_hash") {
		return ErrDuplicateNationalID
	}
	return fmt.Errorf("insert student: %w", err)
}

func marshalStudentStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalStudentStringSlice(raw string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []string{}
	}
	return values, nil
}
