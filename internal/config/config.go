package config

import (
	"errors"
	"os"
	"strings"
)

const (
	defaultHTTPAddress = ":8080"
	defaultDBPath      = "internship.db"
)

// Config contains the runtime configuration for the server.
type Config struct {
	HTTPAddress      string
	DBPath           string
	NationalIDPepper string
}

// FromEnv builds a Config from environment variables, applies local-safe defaults
// for optional settings, and requires NATIONAL_ID_PEPPER to be set.
func FromEnv() (Config, error) {
	pepper := strings.TrimSpace(os.Getenv("NATIONAL_ID_PEPPER"))
	if pepper == "" {
		return Config{}, errors.New("NATIONAL_ID_PEPPER is required")
	}

	return Config{
		HTTPAddress:      valueOrDefault("HTTP_ADDR", defaultHTTPAddress),
		DBPath:           DBPathFromEnv(),
		NationalIDPepper: pepper,
	}, nil
}

// DBPathFromEnv returns DB_PATH or the default SQLite path ("internship.db")
// without requiring server-only secrets like NATIONAL_ID_PEPPER.
func DBPathFromEnv() string {
	return valueOrDefault("DB_PATH", defaultDBPath)
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}

	return fallback
}
