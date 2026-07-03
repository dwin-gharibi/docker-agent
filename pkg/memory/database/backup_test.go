package database_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/memory/database"
	"github.com/docker/docker-agent/pkg/memory/database/sqlite"
)

func TestExportSnapshotCreatesReadableBackup(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sqlite.NewMemoryDatabase(sourcePath)
	require.NoError(t, err)
	source := db.(*sqlite.MemoryDatabase)
	defer source.Close()

	require.NoError(t, db.AddMemory(t.Context(), database.UserMemory{
		ID:        "one",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "remember this",
		Category:  "fact",
	}))

	backupPath := filepath.Join(t.TempDir(), "memory.bak")
	require.NoError(t, database.ExportSnapshot(t.Context(), sourcePath, backupPath))

	backupDB, err := sqlite.NewMemoryDatabase(backupPath)
	require.NoError(t, err)
	backup := backupDB.(*sqlite.MemoryDatabase)
	defer backup.Close()

	memories, err := backupDB.GetMemories(t.Context())
	require.NoError(t, err)
	require.Len(t, memories, 1)
	assert.Equal(t, "one", memories[0].ID)
	assert.Equal(t, "remember this", memories[0].Memory)
	assert.Equal(t, "fact", memories[0].Category)
}

func TestExportSnapshotIncludesOpenWALWrites(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sqlite.NewMemoryDatabase(sourcePath)
	require.NoError(t, err)
	source := db.(*sqlite.MemoryDatabase)
	defer source.Close()

	for i := range 10 {
		require.NoError(t, db.AddMemory(t.Context(), database.UserMemory{
			ID:        string(rune('a' + i)),
			CreatedAt: time.Now().Format(time.RFC3339),
			Memory:    "open db write",
		}))
	}

	backupPath := filepath.Join(t.TempDir(), "memory.bak")
	require.NoError(t, database.ExportSnapshot(t.Context(), sourcePath, backupPath))

	backupDB, err := sqlite.NewMemoryDatabase(backupPath)
	require.NoError(t, err)
	backup := backupDB.(*sqlite.MemoryDatabase)
	defer backup.Close()

	memories, err := backupDB.GetMemories(t.Context())
	require.NoError(t, err)
	assert.Len(t, memories, 10)
}

func TestExportSnapshotOverwritesAtomicallyAndCleansTemp(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sqlite.NewMemoryDatabase(sourcePath)
	require.NoError(t, err)
	source := db.(*sqlite.MemoryDatabase)
	defer source.Close()

	require.NoError(t, db.AddMemory(t.Context(), database.UserMemory{
		ID:        "first",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "first snapshot",
	}))

	backupDir := t.TempDir()
	backupPath := filepath.Join(backupDir, "memory.bak")
	require.NoError(t, database.ExportSnapshot(t.Context(), sourcePath, backupPath))

	require.NoError(t, db.AddMemory(t.Context(), database.UserMemory{
		ID:        "second",
		CreatedAt: time.Now().Format(time.RFC3339),
		Memory:    "second snapshot",
	}))
	require.NoError(t, database.ExportSnapshot(t.Context(), sourcePath, backupPath))

	matches, err := filepath.Glob(filepath.Join(backupDir, ".mem_backup_*.db.tmp"))
	require.NoError(t, err)
	assert.Empty(t, matches)

	backupDB, err := sqlite.NewMemoryDatabase(backupPath)
	require.NoError(t, err)
	backup := backupDB.(*sqlite.MemoryDatabase)
	defer backup.Close()

	memories, err := backupDB.GetMemories(t.Context())
	require.NoError(t, err)
	assert.Len(t, memories, 2)
}
