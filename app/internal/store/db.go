package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

const DefaultDBPath = "./gpu_assistant.db"

// Store manages the SQLite database for GPU inventory and conversation history.
type Store struct {
	db     *sql.DB
	dbPath string
}

// OpenFromEnv opens the SQLite store using SQLITE_PATH (defaulting to ./gpu_assistant.db).
func OpenFromEnv(ctx context.Context) (*Store, error) {
	path := strings.TrimSpace(os.Getenv("SQLITE_PATH"))
	if path == "" {
		path = DefaultDBPath
	}
	return Open(ctx, path)
}

// Open opens or creates a SQLite database at the given path, runs migrations,
// and idempotently seeds the 5 required NVIDIA GPUs.
func Open(ctx context.Context, dbPath string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = DefaultDBPath
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}
	// SQLite concurrency best practices for single-file embedded usage.
	db.SetMaxOpenConns(1)

	s := &Store{
		db:     db,
		dbPath: dbPath,
	}

	if err := s.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	if err := s.SeedGPUs(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("seed gpus: %w", err)
	}

	return s, nil
}

// DB returns the underlying *sql.DB handle.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path returns the configured SQLite database file path.
func (s *Store) Path() string {
	return s.dbPath
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Migrate creates the messages and gpus tables if they do not exist.
func (s *Store) Migrate(ctx context.Context) error {
	const schemaSQL = `
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	tool_calls TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS gpus (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	architecture TEXT NOT NULL,
	memory_gb INTEGER NOT NULL,
	mig_support BOOLEAN NOT NULL,
	total_quantity INTEGER NOT NULL,
	in_use INTEGER NOT NULL,
	notes TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS gpu_loans (
	gpu_id TEXT PRIMARY KEY REFERENCES gpus(id) ON DELETE CASCADE,
	borrowed_by_user INTEGER NOT NULL DEFAULT 0 CHECK (borrowed_by_user >= 0),
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}
