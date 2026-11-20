package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	appsqlite "internship/server/internal/sqlite"
)

func TestListCompaniesEmptyState(t *testing.T) {
	t.Parallel()

	db := openCompaniesTestDB(t)
	ctx := context.Background()

	all, err := appsqlite.ListCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListCompanies on empty db: %v", err)
	}
	if all == nil || len(all) != 0 {
		t.Fatalf("ListCompanies = %#v, want non-nil empty slice", all)
	}

	active, err := appsqlite.ListActiveCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListActiveCompanies on empty db: %v", err)
	}
	if active == nil || len(active) != 0 {
		t.Fatalf("ListActiveCompanies = %#v, want non-nil empty slice", active)
	}
}

func TestSeedAndListCompanies(t *testing.T) {
	t.Parallel()

	db := openCompaniesTestDB(t)
	ctx := context.Background()

	seed := []appsqlite.SeedCompany{
		{
			Name:     "Paystack",
			Industry: "Fintech",
			City:     "Lagos",
			Country:  "Nigeria",
			Summary:  "Online and offline payments across Africa.",
			Programs: []string{"Computer Science", "Finance"},
			Skills:   []string{"Go", "SQL", "TypeScript"},
			Remote:   true,
			Active:   true,
		},
		{
			Name:     "Zipline",
			Industry: "Logistics",
			City:     "Accra",
			Country:  "Ghana",
			Summary:  "Autonomous medical drone delivery network.",
			Programs: nil,
			Skills:   []string{},
			Remote:   false,
			Active:   true,
		},
		{
			Name:     "Legacy Labs",
			Industry: "Software",
			City:     "Nairobi",
			Country:  "Kenya",
			Summary:  "Paused internship host.",
			Programs: []string{"Software Engineering"},
			Skills:   nil,
			Remote:   false,
			Active:   false,
		},
	}

	inserted, updated, err := appsqlite.SeedCompanies(ctx, db, seed)
	if err != nil {
		t.Fatalf("SeedCompanies: %v", err)
	}
	if inserted != 3 || updated != 0 {
		t.Fatalf("SeedCompanies counts = (inserted=%d, updated=%d), want (3, 0)", inserted, updated)
	}

	all, err := appsqlite.ListCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(ListCompanies) = %d, want 3", len(all))
	}
	if all[0].ID >= all[1].ID || all[1].ID >= all[2].ID {
		t.Fatalf("ListCompanies IDs not ascending: %d, %d, %d", all[0].ID, all[1].ID, all[2].ID)
	}

	// Verify empty/nil slices are persisted as "[]" and unmarshaled as non-nil []string{}.
	zipline := all[1]
	if zipline.Programs == nil || len(zipline.Programs) != 0 {
		t.Errorf("Zipline.Programs = %#v, want non-nil []string{}", zipline.Programs)
	}
	if zipline.Skills == nil || len(zipline.Skills) != 0 {
		t.Errorf("Zipline.Skills = %#v, want non-nil []string{}", zipline.Skills)
	}

	var rawPrograms, rawSkills string
	if err := db.QueryRowContext(
		ctx,
		"SELECT programs, skills FROM companies WHERE id = ?",
		zipline.ID,
	).Scan(&rawPrograms, &rawSkills); err != nil {
		t.Fatalf("query raw JSON columns: %v", err)
	}
	if rawPrograms != "[]" || rawSkills != "[]" {
		t.Errorf("raw JSON = (%q, %q), want (\"[]\", \"[]\")", rawPrograms, rawSkills)
	}

	if all[2].Skills == nil || len(all[2].Skills) != 0 {
		t.Errorf("Legacy Labs Skills = %#v, want non-nil []string{}", all[2].Skills)
	}

	active, err := appsqlite.ListActiveCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("len(ListActiveCompanies) = %d, want 2", len(active))
	}
	if active[0].Name != "Paystack" || active[1].Name != "Zipline" {
		t.Errorf("active names = [%q, %q], want [\"Paystack\", \"Zipline\"]", active[0].Name, active[1].Name)
	}
	for _, company := range active {
		if !company.Active {
			t.Errorf("ListActiveCompanies returned inactive company %q", company.Name)
		}
		if company.CreatedAt == "" || company.UpdatedAt == "" {
			t.Errorf("company %q missing timestamps: created_at=%q updated_at=%q", company.Name, company.CreatedAt, company.UpdatedAt)
		}
	}
}

func TestGetCompanyByID(t *testing.T) {
	t.Parallel()

	db := openCompaniesTestDB(t)
	ctx := context.Background()

	seed := []appsqlite.SeedCompany{
		{
			Name:     "InstaDeep",
			Industry: "Data & AI",
			City:     "Kigali",
			Country:  "Rwanda",
			Summary:  "Decision-making AI and machine learning research.",
			Programs: []string{"Data Science", "Computer Science"},
			Skills:   []string{"Python", "Linux", "Docker"},
			Remote:   true,
			Active:   true,
		},
	}

	if _, _, err := appsqlite.SeedCompanies(ctx, db, seed); err != nil {
		t.Fatalf("SeedCompanies: %v", err)
	}

	all, err := appsqlite.ListCompanies(ctx, db)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListCompanies: len=%d err=%v", len(all), err)
	}

	got, err := appsqlite.GetCompanyByID(ctx, db, all[0].ID)
	if err != nil {
		t.Fatalf("GetCompanyByID(%d): %v", all[0].ID, err)
	}
	if got.ID != all[0].ID ||
		got.Name != "InstaDeep" ||
		got.Industry != "Data & AI" ||
		got.City != "Kigali" ||
		got.Country != "Rwanda" ||
		got.Summary != "Decision-making AI and machine learning research." ||
		!got.Remote ||
		!got.Active {
		t.Errorf("GetCompanyByID mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.Programs, []string{"Data Science", "Computer Science"}) {
		t.Errorf("got.Programs = %#v, want [\"Data Science\", \"Computer Science\"]", got.Programs)
	}
	if !reflect.DeepEqual(got.Skills, []string{"Python", "Linux", "Docker"}) {
		t.Errorf("got.Skills = %#v, want [\"Python\", \"Linux\", \"Docker\"]", got.Skills)
	}

	_, err = appsqlite.GetCompanyByID(ctx, db, 99999)
	if err == nil {
		t.Fatal("GetCompanyByID(99999) succeeded, want error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetCompanyByID(99999) error = %v, want errors.Is(..., sql.ErrNoRows)", err)
	}
}

func TestSeedCompaniesValidationErrors(t *testing.T) {
	t.Parallel()

	validBase := appsqlite.SeedCompany{
		Name:     "Hubtel",
		Industry: "Fintech",
		City:     "Accra",
		Country:  "Ghana",
		Summary:  "Payments and merchant messaging platform.",
		Programs: []string{"Computer Science"},
		Skills:   []string{"TypeScript", "SQL"},
		Remote:   false,
		Active:   true,
	}

	cases := []struct {
		name   string
		mutate func(*appsqlite.SeedCompany)
	}{
		{
			name:   "missing name",
			mutate: func(c *appsqlite.SeedCompany) { c.Name = "   " },
		},
		{
			name:   "missing industry",
			mutate: func(c *appsqlite.SeedCompany) { c.Industry = "" },
		},
		{
			name:   "missing city",
			mutate: func(c *appsqlite.SeedCompany) { c.City = "\t\n" },
		},
		{
			name:   "missing country",
			mutate: func(c *appsqlite.SeedCompany) { c.Country = "" },
		},
		{
			name:   "missing summary",
			mutate: func(c *appsqlite.SeedCompany) { c.Summary = " " },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openCompaniesTestDB(t)
			ctx := context.Background()

			invalid := validBase
			invalid.Name = "Second Company"
			tc.mutate(&invalid)

			inserted, updated, err := appsqlite.SeedCompanies(ctx, db, []appsqlite.SeedCompany{
				validBase,
				invalid,
			})
			if err == nil {
				t.Fatalf("SeedCompanies(%s) succeeded, want validation error", tc.name)
			}
			if inserted != 0 || updated != 0 {
				t.Errorf("SeedCompanies(%s) counts = (%d, %d), want (0, 0)", tc.name, inserted, updated)
			}

			// Validation must happen before inserting earlier items in the batch.
			all, listErr := appsqlite.ListCompanies(ctx, db)
			if listErr != nil {
				t.Fatalf("ListCompanies after failed seed: %v", listErr)
			}
			if len(all) != 0 {
				t.Errorf("expected 0 companies persisted after validation error, got %d", len(all))
			}
		})
	}
}

func TestSeedCompaniesIdempotencyAndForeignKeys(t *testing.T) {
	t.Parallel()

	db := openCompaniesTestDB(t)
	ctx := context.Background()

	initial := []appsqlite.SeedCompany{
		{
			Name:     "Paystack",
			Industry: "Fintech",
			City:     "Lagos",
			Country:  "Nigeria",
			Summary:  "Initial Paystack summary.",
			Programs: []string{"Computer Science"},
			Skills:   []string{"Go", "SQL"},
			Remote:   false,
			Active:   true,
		},
		{
			Name:     "mPharma",
			Industry: "Healthcare",
			City:     "Accra",
			Country:  "Ghana",
			Summary:  "Initial mPharma summary.",
			Programs: []string{"Biomedical Engineering"},
			Skills:   []string{"Python"},
			Remote:   false,
			Active:   true,
		},
	}

	inserted, updated, err := appsqlite.SeedCompanies(ctx, db, initial)
	if err != nil {
		t.Fatalf("first SeedCompanies: %v", err)
	}
	if inserted != len(initial) || updated != 0 {
		t.Fatalf("first SeedCompanies = (inserted=%d, updated=%d), want (%d, 0)", inserted, updated, len(initial))
	}

	firstList, err := appsqlite.ListCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListCompanies after first seed: %v", err)
	}
	originalIDs := []int64{firstList[0].ID, firstList[1].ID}

	// Attach a student preference referencing the first company ID to prove re-seeding preserves FKs.
	studentID := insertCompaniesTestStudent(t, db, "intern@example.com", "nat-hash-001")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO preferences (student_id, company_id, signal, reason)
		VALUES (?, ?, 'like', NULL)
	`, studentID, originalIDs[0]); err != nil {
		t.Fatalf("insert preference: %v", err)
	}

	// Second run with exact same items: inserted == 0, updated == len(items), IDs preserved.
	inserted, updated, err = appsqlite.SeedCompanies(ctx, db, initial)
	if err != nil {
		t.Fatalf("second SeedCompanies: %v", err)
	}
	if inserted != 0 || updated != len(initial) {
		t.Fatalf("second SeedCompanies = (inserted=%d, updated=%d), want (0, %d)", inserted, updated, len(initial))
	}

	// Third run with case-variant (name, city, country) and updated attributes: still matches in-place!
	modified := []appsqlite.SeedCompany{
		{
			Name:     "PAYSTACK",
			Industry: "Fintech",
			City:     "lagos",
			Country:  "NIGERIA",
			Summary:  "Updated Paystack summary.",
			Programs: []string{"Computer Science", "Finance"},
			Skills:   []string{"Go", "SQL", "Docker"},
			Remote:   true,
			Active:   true,
		},
		{
			Name:     "mpharma",
			Industry: "Healthcare",
			City:     "ACCRA",
			Country:  "ghana",
			Summary:  "Updated mPharma summary.",
			Programs: []string{"Biomedical Engineering", "Data Science"},
			Skills:   []string{},
			Remote:   true,
			Active:   false,
		},
	}

	inserted, updated, err = appsqlite.SeedCompanies(ctx, db, modified)
	if err != nil {
		t.Fatalf("third SeedCompanies (case-insensitive update): %v", err)
	}
	if inserted != 0 || updated != len(modified) {
		t.Fatalf("third SeedCompanies = (inserted=%d, updated=%d), want (0, %d)", inserted, updated, len(modified))
	}

	afterUpdate, err := appsqlite.ListCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListCompanies after third seed: %v", err)
	}
	if len(afterUpdate) != 2 {
		t.Fatalf("len(afterUpdate) = %d, want 2", len(afterUpdate))
	}
	if afterUpdate[0].ID != originalIDs[0] || afterUpdate[1].ID != originalIDs[1] {
		t.Fatalf("IDs changed from %v to [%d, %d]", originalIDs, afterUpdate[0].ID, afterUpdate[1].ID)
	}
	if afterUpdate[0].Summary != "Updated Paystack summary." || !afterUpdate[0].Remote {
		t.Errorf("Paystack not updated properly: %+v", afterUpdate[0])
	}
	if afterUpdate[1].Summary != "Updated mPharma summary." || afterUpdate[1].Active || !afterUpdate[1].Remote {
		t.Errorf("mPharma not updated properly: %+v", afterUpdate[1])
	}
	if afterUpdate[1].Skills == nil || len(afterUpdate[1].Skills) != 0 {
		t.Errorf("mPharma updated empty skills = %#v, want non-nil []string{}", afterUpdate[1].Skills)
	}

	var prefCount int
	if err := db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM preferences WHERE student_id = ? AND company_id = ?",
		studentID,
		originalIDs[0],
	).Scan(&prefCount); err != nil {
		t.Fatalf("count preferences after re-seed: %v", err)
	}
	if prefCount != 1 {
		t.Errorf("preference count = %d, want 1", prefCount)
	}
}

func openCompaniesTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := appsqlite.Open(context.Background(), filepath.Join(t.TempDir(), "companies_test.db"))
	if err != nil {
		t.Fatalf("open companies test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close companies test database: %v", err)
		}
	})

	return db
}

func insertCompaniesTestStudent(t *testing.T, db *sql.DB, email, nationalIDHash string) int64 {
	t.Helper()

	res, err := db.Exec(`
		INSERT INTO students (
			email, password_hash, full_name, program, city, country, student_id, national_id_hash
		) VALUES (?, 'pw-hash', 'Catalog Student', 'Computer Science', 'Accra', 'Ghana', 'STU-100', ?)
	`, email, nationalIDHash)
	if err != nil {
		t.Fatalf("insert test student: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("student LastInsertId: %v", err)
	}
	return id
}
