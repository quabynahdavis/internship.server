package config

import "os"

const (
	defaultHTTPAddress = ":8080"
	defaultDBPath      = "internship.db"
)

// Config contains the runtime configuration for the server.
type Config struct {
	HTTPAddress string
	DBPath      string
}

// FromEnv builds a Config from environment variables and applies local-safe defaults.
func FromEnv() Config {
	return Config{
		HTTPAddress: valueOrDefault("HTTP_ADDR", defaultHTTPAddress),
		DBPath:      valueOrDefault("DB_PATH", defaultDBPath),
	}
}

func valueOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}
