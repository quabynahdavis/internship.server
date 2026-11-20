package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Company represents a persisted internship host company in the catalog.
type Company struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Industry  string   `json:"industry"`
	City      string   `json:"city"`
	Country   string   `json:"country"`
	Summary   string   `json:"summary"`
	Programs  []string `json:"programs"`
	Skills    []string `json:"skills"`
	Remote    bool     `json:"remote"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// SeedCompany represents a company record loaded from a seed catalog file.
type SeedCompany struct {
	Name     string   `json:"name"`
	Industry string   `json:"industry"`
	City     string   `json:"city"`
	Country  string   `json:"country"`
	Summary  string   `json:"summary"`
	Programs []string `json:"programs"`
	Skills   []string `json:"skills"`
	Remote   bool     `json:"remote"`
	Active   bool     `json:"active"`
}

const companySelectColumns = `
	id, name, industry, city, country, summary,
	programs, skills, remote, active, created_at, updated_at
`

// SeedCompanies validates and upserts seed company records inside a single transaction.
// Existing rows are matched case-insensitively by (name, city, country) so re-seeding
// is idempotent and preserves primary keys referenced by student preferences.
func SeedCompanies(ctx context.Context, db *sql.DB, items []SeedCompany) (inserted int, updated int, err error) {
	if db == nil {
		return 0, 0, errors.New("SQLite database is nil")
	}

	normalized := make([]SeedCompany, len(items))
	for i, item := range items {
		clean, err := normalizeAndValidateSeedCompany(i, item)
		if err != nil {
			return 0, 0, err
		}
		normalized[i] = clean
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin company seed transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	existingByKey, err := loadExistingCompanyIDsByKey(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	for _, item := range normalized {
		programsJSON, err := marshalCompanyStringSlice(item.Programs)
		if err != nil {
			return 0, 0, fmt.Errorf("marshal programs for %q: %w", item.Name, err)
		}
		skillsJSON, err := marshalCompanyStringSlice(item.Skills)
		if err != nil {
			return 0, 0, fmt.Errorf("marshal skills for %q: %w", item.Name, err)
		}

		key := companyNaturalKey(item.Name, item.City, item.Country)
		if existingID, exists := existingByKey[key]; exists {
			if _, err := tx.ExecContext(ctx, `
				UPDATE companies
				SET industry = ?,
				    summary = ?,
				    programs = ?,
				    skills = ?,
				    remote = ?,
				    active = ?,
				    updated_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`,
				item.Industry,
				item.Summary,
				programsJSON,
				skillsJSON,
				companyBoolToInt(item.Remote),
				companyBoolToInt(item.Active),
				existingID,
			); err != nil {
				return 0, 0, fmt.Errorf("update company %q (id=%d): %w", item.Name, existingID, err)
			}
			updated++
			continue
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO companies (
				name, industry, city, country, summary,
				programs, skills, remote, active
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			item.Name,
			item.Industry,
			item.City,
			item.Country,
			item.Summary,
			programsJSON,
			skillsJSON,
			companyBoolToInt(item.Remote),
			companyBoolToInt(item.Active),
		)
		if err != nil {
			return 0, 0, fmt.Errorf("insert company %q: %w", item.Name, err)
		}

		newID, err := result.LastInsertId()
		if err != nil {
			return 0, 0, fmt.Errorf("read inserted id for company %q: %w", item.Name, err)
		}
		existingByKey[key] = newID
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit company seed transaction: %w", err)
	}

	return inserted, updated, nil
}

// ListActiveCompanies returns all active companies ordered by ID ascending.
func ListActiveCompanies(ctx context.Context, db *sql.DB) ([]Company, error) {
	return queryCompanies(ctx, db, `
		SELECT `+companySelectColumns+`
		FROM companies
		WHERE active = 1
		ORDER BY id ASC
	`)
}

// ListCompanies returns all companies (active and inactive) ordered by ID ascending.
func ListCompanies(ctx context.Context, db *sql.DB) ([]Company, error) {
	return queryCompanies(ctx, db, `
		SELECT `+companySelectColumns+`
		FROM companies
		ORDER BY id ASC
	`)
}

// GetCompanyByID fetches a single company by ID or returns an error wrapping sql.ErrNoRows.
func GetCompanyByID(ctx context.Context, db *sql.DB, id int64) (Company, error) {
	if db == nil {
		return Company{}, errors.New("SQLite database is nil")
	}

	row := db.QueryRowContext(ctx, `
		SELECT `+companySelectColumns+`
		FROM companies
		WHERE id = ?
	`, id)

	company, err := scanCompany(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Company{}, fmt.Errorf("company %d not found: %w", id, err)
		}
		return Company{}, fmt.Errorf("query company %d: %w", id, err)
	}

	return company, nil
}

type companyRowScanner interface {
	Scan(dest ...any) error
}

func queryCompanies(ctx context.Context, db *sql.DB, query string, args ...any) ([]Company, error) {
	if db == nil {
		return nil, errors.New("SQLite database is nil")
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query companies: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	companies := make([]Company, 0)
	for rows.Next() {
		company, err := scanCompany(rows)
		if err != nil {
			return nil, fmt.Errorf("scan company row: %w", err)
		}
		companies = append(companies, company)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate company rows: %w", err)
	}

	return companies, nil
}

func scanCompany(scanner companyRowScanner) (Company, error) {
	var (
		company      Company
		programsJSON string
		skillsJSON   string
		remoteInt    int
		activeInt    int
	)

	if err := scanner.Scan(
		&company.ID,
		&company.Name,
		&company.Industry,
		&company.City,
		&company.Country,
		&company.Summary,
		&programsJSON,
		&skillsJSON,
		&remoteInt,
		&activeInt,
		&company.CreatedAt,
		&company.UpdatedAt,
	); err != nil {
		return Company{}, err
	}

	programs, err := unmarshalCompanyStringSlice(programsJSON)
	if err != nil {
		return Company{}, fmt.Errorf("decode programs JSON for company %d: %w", company.ID, err)
	}
	skills, err := unmarshalCompanyStringSlice(skillsJSON)
	if err != nil {
		return Company{}, fmt.Errorf("decode skills JSON for company %d: %w", company.ID, err)
	}

	company.Programs = programs
	company.Skills = skills
	company.Remote = remoteInt != 0
	company.Active = activeInt != 0

	return company, nil
}

func normalizeAndValidateSeedCompany(index int, item SeedCompany) (SeedCompany, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.Industry = strings.TrimSpace(item.Industry)
	item.City = strings.TrimSpace(item.City)
	item.Country = strings.TrimSpace(item.Country)
	item.Summary = strings.TrimSpace(item.Summary)

	if item.Name == "" {
		return SeedCompany{}, fmt.Errorf("seed company at index %d: name is required", index)
	}
	if item.Industry == "" {
		return SeedCompany{}, fmt.Errorf("seed company %q (index %d): industry is required", item.Name, index)
	}
	if item.City == "" {
		return SeedCompany{}, fmt.Errorf("seed company %q (index %d): city is required", item.Name, index)
	}
	if item.Country == "" {
		return SeedCompany{}, fmt.Errorf("seed company %q (index %d): country is required", item.Name, index)
	}
	if item.Summary == "" {
		return SeedCompany{}, fmt.Errorf("seed company %q (index %d): summary is required", item.Name, index)
	}

	if len(item.Programs) == 0 {
		item.Programs = []string{}
	} else {
		programs := make([]string, len(item.Programs))
		copy(programs, item.Programs)
		item.Programs = programs
	}

	if len(item.Skills) == 0 {
		item.Skills = []string{}
	} else {
		skills := make([]string, len(item.Skills))
		copy(skills, item.Skills)
		item.Skills = skills
	}

	return item, nil
}

func loadExistingCompanyIDsByKey(ctx context.Context, tx *sql.Tx) (map[[3]string]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, city, country
		FROM companies
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("load existing companies: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	existing := make(map[[3]string]int64)
	for rows.Next() {
		var (
			id      int64
			name    string
			city    string
			country string
		)
		if err := rows.Scan(&id, &name, &city, &country); err != nil {
			return nil, fmt.Errorf("scan existing company key: %w", err)
		}
		key := companyNaturalKey(name, city, country)
		if _, seen := existing[key]; !seen {
			existing[key] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing companies: %w", err)
	}

	return existing, nil
}

func companyNaturalKey(name, city, country string) [3]string {
	return [3]string{
		strings.ToLower(strings.TrimSpace(name)),
		strings.ToLower(strings.TrimSpace(city)),
		strings.ToLower(strings.TrimSpace(country)),
	}
}

func marshalCompanyStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func unmarshalCompanyStringSlice(raw string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []string{}
	}
	return values, nil
}

func companyBoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
