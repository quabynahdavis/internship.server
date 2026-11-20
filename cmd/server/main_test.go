package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunFailsWhenNationalIDPepperIsUnset(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "should_not_exist.db")
	t.Setenv("NATIONAL_ID_PEPPER", "")
	t.Setenv("DB_PATH", dbPath)
	t.Setenv("HTTP_ADDR", "127.0.0.1:0")

	err := run(context.Background())
	if err == nil {
		t.Fatal("run() succeeded with unset NATIONAL_ID_PEPPER, want error")
	}
	if !strings.Contains(err.Error(), "NATIONAL_ID_PEPPER is required") {
		t.Errorf("run() error = %q, want substring %q", err.Error(), "NATIONAL_ID_PEPPER is required")
	}

	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Errorf("database file %q should not have been created on config failure", dbPath)
	}
}

func TestRunStartsAndShutsDownCleanlyWithValidConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "server_startup.db")
	t.Setenv("NATIONAL_ID_PEPPER", "valid-startup-test-pepper")
	t.Setenv("DB_PATH", dbPath)
	t.Setenv("HTTP_ADDR", "127.0.0.1:0")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := run(ctx); err != nil {
		t.Fatalf("run() with valid config failed: %v", err)
	}
}
