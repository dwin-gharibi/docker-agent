package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	atomic "github.com/natefinch/atomic"

	"github.com/docker/docker-agent/pkg/sqliteutil"
)

// ExportSnapshot writes a consistent SQLite snapshot of dbPath to finalPath.
//
// The snapshot is written to a temp file in finalPath's directory and then
// renamed into place, so readers of finalPath see either the previous snapshot
// or the complete new snapshot. The source memory DB lock is held while the
// snapshot is created to serialize it with memory writes.
func ExportSnapshot(ctx context.Context, dbPath, finalPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	lock := NewFileLock(LockPathForDatabase(dbPath))
	if err := lock.Lock(ctx); err != nil {
		return err
	}
	defer func() { _ = lock.Unlock() }()

	db, err := sqliteutil.OpenDB(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating memory snapshot directory %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".mem_backup_*.db.tmp")
	if err != nil {
		return fmt.Errorf("creating temp memory snapshot: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp memory snapshot: %w", err)
	}
	if err := os.Remove(tmpName); err != nil {
		return fmt.Errorf("removing empty temp memory snapshot: %w", err)
	}
	defer os.Remove(tmpName)

	if err := vacuumInto(ctx, db, tmpName); err != nil {
		return err
	}
	if err := syncFile(tmpName); err != nil {
		return err
	}

	if err := atomic.ReplaceFile(tmpName, finalPath); err != nil {
		return fmt.Errorf("publishing memory snapshot %q: %w", finalPath, err)
	}

	syncDir(dir)
	return nil
}

func vacuumInto(ctx context.Context, db *sql.DB, path string) error {
	stmt := "VACUUM INTO " + sqliteString(path) // #nosec G202 -- VACUUM INTO does not accept bound parameters; sqliteString quotes the file path.
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("exporting memory snapshot: %w", err)
	}
	return nil
}

func sqliteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening memory snapshot for sync: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("syncing memory snapshot: %w", err)
	}
	return nil
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
