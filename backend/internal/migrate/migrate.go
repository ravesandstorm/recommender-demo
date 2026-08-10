package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Up(ctx context.Context, pool *pgxpool.Pool) error {
	dir, err := findMigrationsDir()
	if err != nil {
		return err
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		if err := applyOne(ctx, pool, filepath.Join(dir, name), name); err != nil {
			return err
		}
	}
	return nil
}

func findMigrationsDir() (string, error) {
	if d := os.Getenv("MIGRATIONS_DIR"); d != "" {
		return d, nil
	}
	candidates := []string{
		"migrations",
		filepath.Join("backend", "migrations"),
	}
	// Walk up from cwd a few levels
	cwd, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		for _, c := range []string{"migrations", filepath.Join("backend", "migrations")} {
			p := filepath.Join(cwd, c)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, nil
			}
		}
		cwd = filepath.Dir(cwd)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("migrations directory not found (set MIGRATIONS_DIR)")
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, path, name string) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename=$1)`, name).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}

	sql := extractUp(string(raw))
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(filename) VALUES ($1)`, name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func extractUp(sql string) string {
	const downMarker = "-- +migrate Down"
	idx := strings.Index(sql, downMarker)
	if idx >= 0 {
		sql = sql[:idx]
	}
	sql = strings.ReplaceAll(sql, "-- +migrate Up", "")
	return strings.TrimSpace(sql)
}
