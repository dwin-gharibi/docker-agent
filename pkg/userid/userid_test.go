package userid

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet_GeneratesAndPersistsUUID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := New(dir)

	id := r.Get()

	require.NotEmpty(t, id)
	_, err := uuid.Parse(id)
	require.NoError(t, err, "Get must return a valid UUID")

	data, err := os.ReadFile(filepath.Join(dir, fileName))
	require.NoError(t, err)
	assert.Equal(t, id, string(data), "Get must persist the UUID to disk")
}

func TestGet_ReturnsExistingUUID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const stored = "11111111-2222-3333-4444-555555555555"
	require.NoError(t, os.WriteFile(filepath.Join(dir, fileName), []byte(stored+"\n"), 0o600))

	assert.Equal(t, stored, New(dir).Get(), "Get must return the persisted UUID, trimmed")
}

func TestGet_RegeneratesOnEmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, fileName), []byte("   \n"), 0o600))

	id := New(dir).Get()
	require.NotEmpty(t, id)
	_, err := uuid.Parse(id)
	require.NoError(t, err, "Get must regenerate when the existing file is blank")
}

func TestGet_CachesAcrossCalls(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	r := New(dir)

	first := r.Get()

	// Mutating the file on disk after the first call must not change
	// the value returned by subsequent calls (it is served from the
	// in-memory cache).
	require.NoError(t, os.WriteFile(filepath.Join(dir, fileName), []byte("changed-on-disk"), 0o600))

	assert.Equal(t, first, r.Get(), "Get must return the cached value on subsequent calls")
}

func TestGet_RegeneratesOnInvalidUUID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Write an invalid UUID to the file (e.g., manually corrupted)
	require.NoError(t, os.WriteFile(filepath.Join(dir, fileName), []byte("not-a-valid-uuid"), 0o600))

	r := New(dir)
	id := r.Get()
	require.NotEmpty(t, id)
	_, err := uuid.Parse(id)
	require.NoError(t, err, "Get must regenerate when the existing file contains an invalid UUID")

	// Verify the new valid UUID was persisted
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	require.NoError(t, err)
	assert.Equal(t, id, string(data), "Get must persist the regenerated UUID")
}
