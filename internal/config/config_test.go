package config_test

import (
	"strings"
	"testing"

	"internship/server/internal/config"
)

func TestFromEnvRequiresNationalIDPepper(t *testing.T) {
	t.Setenv("NATIONAL_ID_PEPPER", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_PATH", "")

	_, err := config.FromEnv()
	if err == nil {
		t.Fatal("FromEnv() succeeded without NATIONAL_ID_PEPPER, want error")
	}
	if !strings.Contains(err.Error(), "NATIONAL_ID_PEPPER is required") {
		t.Errorf("error = %q, want substring %q", err.Error(), "NATIONAL_ID_PEPPER is required")
	}
}

func TestFromEnvRejectsWhitespaceOnlyPepper(t *testing.T) {
	t.Setenv("NATIONAL_ID_PEPPER", "   \t\n  ")

	_, err := config.FromEnv()
	if err == nil {
		t.Fatal("FromEnv() succeeded with whitespace-only NATIONAL_ID_PEPPER, want error")
	}
	if !strings.Contains(err.Error(), "NATIONAL_ID_PEPPER is required") {
		t.Errorf("error = %q, want substring %q", err.Error(), "NATIONAL_ID_PEPPER is required")
	}
}

func TestFromEnvAppliesDefaults(t *testing.T) {
	t.Setenv("NATIONAL_ID_PEPPER", "  super-secret-pepper  ")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_PATH", "")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() unexpected error: %v", err)
	}

	if cfg.HTTPAddress != ":8080" {
		t.Errorf("HTTPAddress = %q, want %q", cfg.HTTPAddress, ":8080")
	}
	if cfg.DBPath != "internship.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "internship.db")
	}
	if cfg.NationalIDPepper != "super-secret-pepper" {
		t.Errorf("NationalIDPepper = %q, want %q", cfg.NationalIDPepper, "super-secret-pepper")
	}
}

func TestFromEnvCustomValues(t *testing.T) {
	t.Setenv("NATIONAL_ID_PEPPER", "prod-pepper-123")
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("DB_PATH", "/tmp/custom.db")

	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() unexpected error: %v", err)
	}

	if cfg.HTTPAddress != "127.0.0.1:9090" {
		t.Errorf("HTTPAddress = %q, want %q", cfg.HTTPAddress, "127.0.0.1:9090")
	}
	if cfg.DBPath != "/tmp/custom.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/tmp/custom.db")
	}
	if cfg.NationalIDPepper != "prod-pepper-123" {
		t.Errorf("NationalIDPepper = %q, want %q", cfg.NationalIDPepper, "prod-pepper-123")
	}
}

func TestDBPathFromEnvDoesNotRequirePepper(t *testing.T) {
	t.Setenv("NATIONAL_ID_PEPPER", "")
	t.Setenv("DB_PATH", "")

	if got := config.DBPathFromEnv(); got != "internship.db" {
		t.Errorf("DBPathFromEnv() = %q, want %q", got, "internship.db")
	}

	t.Setenv("DB_PATH", "seed-data.db")
	if got := config.DBPathFromEnv(); got != "seed-data.db" {
		t.Errorf("DBPathFromEnv() = %q, want %q", got, "seed-data.db")
	}
}
