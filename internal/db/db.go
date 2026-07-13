// Package db manages binbash's SQLite connection and schema migrations.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DirMode is the permission new binbash data directories are created with:
// owner only, no group, no world.
const DirMode = 0o700

// Open opens the SQLite database at path, creating its parent directory if
// needed, applies any pending migrations, and makes sure neither the database
// nor its directory is readable by anyone but the user binbash runs as.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, DirMode); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
		// MkdirAll only applies the mode to directories it creates, so an
		// install that predates this still has a 0755 data directory.
		if err := RestrictPermissions(dir); err != nil {
			return nil, fmt.Errorf("secure db directory: %w", err)
		}
	}

	// Pragmas are applied through the DSN so that *every* pooled connection gets
	// them. foreign_keys and busy_timeout are per-connection settings, so running
	// them once via database.Exec would only configure a single connection in the
	// pool and leave the rest unprotected.
	//   - busy_timeout(5000): wait up to 5s for a lock instead of failing outright
	//                         with SQLITE_BUSY ("database is locked").
	//   - foreign_keys(1):    enforce the items -> bins relationship.
	//   - journal_mode(WAL):  let readers proceed alongside a writer; persisted in
	//                         the database file, so it only needs setting once.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"

	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite allows only one writer at a time. Funneling all access through a
	// single connection eliminates internal write contention entirely, which is
	// well within the performance envelope of a self-hosted home inventory app.
	database.SetMaxOpenConns(1)

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := migrate(database); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// After migrate, so the -wal and -shm sidecars WAL mode creates on first
	// write already exist and get tightened along with the database itself.
	if err := RestrictPermissions(path); err != nil {
		return nil, fmt.Errorf("secure database file: %w", err)
	}

	return database, nil
}

func migrate(database *sql.DB) error {
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied int
		err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		contents, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}

		if _, err := tx.Exec(string(contents)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}
