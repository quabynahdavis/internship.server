package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	appsqlite "internship/server/internal/sqlite"
)

const (
	defaultSeedDBPath   = "internship.db"
	defaultSeedFilePath = "seed/companies.json"
)

func main() {
	dbPath, seedPath, err := parseFlags(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		slog.Error("parse seed flags", "error", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, _, err := run(ctx, dbPath, seedPath); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func parseFlags(args []string, getenv func(string) string) (dbPath, seedPath string, err error) {
	defaultDB := defaultSeedDBPath
	if getenv != nil {
		if envDB := strings.TrimSpace(getenv("DB_PATH")); envDB != "" {
			defaultDB = envDB
		}
	}

	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.StringVar(&dbPath, "db", defaultDB, "SQLite database file path (defaults to DB_PATH or internship.db)")
	fs.StringVar(&seedPath, "file", defaultSeedFilePath, "Path to companies seed JSON catalog")

	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() > 0 {
		return "", "", fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	return dbPath, seedPath, nil
}

func run(ctx context.Context, dbPath, seedPath string) (inserted, updated int, err error) {
	if strings.TrimSpace(dbPath) == "" {
		return 0, 0, errors.New("database path is empty")
	}
	if strings.TrimSpace(seedPath) == "" {
		return 0, 0, errors.New("seed file path is empty")
	}

	items, err := loadSeedCatalog(seedPath)
	if err != nil {
		return 0, 0, err
	}

	db, err := appsqlite.Open(ctx, dbPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close database: %w", closeErr)
		}
	}()

	inserted, updated, err = appsqlite.SeedCompanies(ctx, db, items)
	if err != nil {
		return 0, 0, fmt.Errorf("seed companies: %w", err)
	}

	slog.Info(
		"seeded companies catalog",
		"db_path", dbPath,
		"seed_file", seedPath,
		"inserted", inserted,
		"updated", updated,
		"total", len(items),
	)

	return inserted, updated, nil
}

func loadSeedCatalog(seedPath string) ([]appsqlite.SeedCompany, error) {
	file, err := os.Open(seedPath)
	if err != nil {
		return nil, fmt.Errorf("open seed file %q: %w", seedPath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var items []appsqlite.SeedCompany
	if err := decoder.Decode(&items); err != nil {
		return nil, fmt.Errorf("decode seed file %q: %w", seedPath, err)
	}
	if items == nil {
		return nil, fmt.Errorf("decode seed file %q: catalog must be a JSON array", seedPath)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("decode seed file %q: unexpected trailing JSON data", seedPath)
		}
		return nil, fmt.Errorf("decode seed file %q trailing content: %w", seedPath, err)
	}

	return items, nil
}
