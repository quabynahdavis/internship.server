package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appsqlite "internship/server/internal/sqlite"
)

func TestRunSeedsRealCatalogIdempotently(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "seeded.db")
	seedPath := filepath.Join("..", "..", "seed", "companies.json")

	inserted, updated, err := run(ctx, dbPath, seedPath)
	if err != nil {
		t.Fatalf("first run(%q, %q): %v", dbPath, seedPath, err)
	}
	if inserted != 24 || updated != 0 {
		t.Fatalf("first run counts = (inserted=%d, updated=%d), want (24, 0)", inserted, updated)
	}

	db, err := appsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open seeded db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close seeded db: %v", err)
		}
	})

	allBefore, err := appsqlite.ListCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListCompanies: %v", err)
	}
	if len(allBefore) != 24 {
		t.Fatalf("len(ListCompanies) = %d, want 24", len(allBefore))
	}

	activeBefore, err := appsqlite.ListActiveCompanies(ctx, db)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(activeBefore) != 23 {
		t.Fatalf("len(ListActiveCompanies) = %d, want 23 (23 active, 1 inactive)", len(activeBefore))
	}

	inactiveCount := 0
	for _, company := range allBefore {
		if !company.Active {
			inactiveCount++
		}
		if company.Programs == nil || len(company.Programs) == 0 {
			t.Errorf("company %q has empty programs", company.Name)
		}
		if company.Skills == nil || len(company.Skills) == 0 {
			t.Errorf("company %q has empty skills", company.Name)
		}
	}
	if inactiveCount != 1 {
		t.Errorf("inactive company count = %d, want 1", inactiveCount)
	}

	// Close handle before running CLI again against the same SQLite file, then re-verify IDs.
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db before second run: %v", err)
	}

	inserted, updated, err = run(ctx, dbPath, seedPath)
	if err != nil {
		t.Fatalf("second run(%q, %q): %v", dbPath, seedPath, err)
	}
	if inserted != 0 || updated != 24 {
		t.Fatalf("second run counts = (inserted=%d, updated=%d), want (0, 24)", inserted, updated)
	}

	db2, err := appsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen seeded db: %v", err)
	}
	defer func() {
		_ = db2.Close()
	}()

	allAfter, err := appsqlite.ListCompanies(ctx, db2)
	if err != nil {
		t.Fatalf("ListCompanies after second run: %v", err)
	}
	if len(allAfter) != 24 {
		t.Fatalf("len(allAfter) = %d, want 24", len(allAfter))
	}
	for i := range allBefore {
		if allAfter[i].ID != allBefore[i].ID {
			t.Errorf("company index %d ID changed from %d to %d", i, allBefore[i].ID, allAfter[i].ID)
		}
	}
}

func TestRunRejectsUnknownJSONFieldsAndInvalidFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "invalid.db")

	unknownFieldFile := filepath.Join(tempDir, "unknown.json")
	if err := os.WriteFile(unknownFieldFile, []byte(`[
		{
			"name": "Acme",
			"industry": "Software",
			"city": "Accra",
			"country": "Ghana",
			"summary": "Cloud tooling.",
			"programs": ["Computer Science"],
			"skills": ["Go"],
			"remote": true,
			"active": true,
			"unexpected_field": "not allowed"
		}
	]`), 0o600); err != nil {
		t.Fatalf("write unknownFieldFile: %v", err)
	}

	if _, _, err := run(ctx, dbPath, unknownFieldFile); err == nil {
		t.Fatal("run with unknown JSON field succeeded, want DisallowUnknownFields error")
	}

	trailingFile := filepath.Join(tempDir, "trailing.json")
	if err := os.WriteFile(trailingFile, []byte(`[] {"extra": true}`), 0o600); err != nil {
		t.Fatalf("write trailingFile: %v", err)
	}
	if _, _, err := run(ctx, dbPath, trailingFile); err == nil {
		t.Fatal("run with trailing JSON succeeded, want error")
	}

	if _, _, err := run(ctx, dbPath, filepath.Join(tempDir, "missing.json")); err == nil {
		t.Fatal("run with missing file succeeded, want error")
	}
}

func TestParseFlagsAndEnvFallback(t *testing.T) {
	t.Parallel()

	dbPath, seedPath, err := parseFlags(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("parseFlags defaults: %v", err)
	}
	if dbPath != "internship.db" || seedPath != "seed/companies.json" {
		t.Errorf("defaults = (%q, %q), want (\"internship.db\", \"seed/companies.json\")", dbPath, seedPath)
	}

	dbPath, seedPath, err = parseFlags(nil, func(key string) string {
		if key == "DB_PATH" {
			return "/tmp/env-internship.db"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("parseFlags env fallback: %v", err)
	}
	if dbPath != "/tmp/env-internship.db" || seedPath != "seed/companies.json" {
		t.Errorf("env fallback = (%q, %q), want (\"/tmp/env-internship.db\", \"seed/companies.json\")", dbPath, seedPath)
	}

	dbPath, seedPath, err = parseFlags(
		[]string{"-db", "/tmp/flag.db", "-file", "/tmp/custom.json"},
		func(string) string { return "/tmp/env-internship.db" },
	)
	if err != nil {
		t.Fatalf("parseFlags explicit flags: %v", err)
	}
	if dbPath != "/tmp/flag.db" || seedPath != "/tmp/custom.json" {
		t.Errorf("explicit flags = (%q, %q), want (\"/tmp/flag.db\", \"/tmp/custom.json\")", dbPath, seedPath)
	}
}
