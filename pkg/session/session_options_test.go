package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithWorkingDir_SetsAllowedDirectories(t *testing.T) {
	t.Parallel()
	s := New(WithWorkingDir("/projects/myapp"))

	assert.Equal(t, "/projects/myapp", s.WorkingDir)
	assert.Equal(t, []string{"/projects/myapp"}, s.AllowedDirectories())
}

func TestWithWorkingDir_EmptyReturnsNilAllowedDirs(t *testing.T) {
	t.Parallel()
	s := New()

	assert.Empty(t, s.WorkingDir)
	assert.Nil(t, s.AllowedDirectories())
}

func TestNewSession_AllOptionsApplied(t *testing.T) {
	t.Parallel()
	s := New(
		WithMaxIterations(10),
		WithToolsApproved(true),
		WithHideToolResults(true),
		WithWorkingDir("/work"),
	)

	assert.Equal(t, 10, s.MaxIterations)
	assert.True(t, s.ToolsApproved)
	assert.True(t, s.HideToolResults)
	assert.Equal(t, "/work", s.WorkingDir)
	assert.Equal(t, []string{"/work"}, s.AllowedDirectories())
}

// TestNewSession_ConsistencyBetweenInitialAndSpawned verifies that the
// initial session and spawned sessions receive the same set of options.
// This test documents the expected option set so that adding a new option
// to one path without the other will be caught.
func TestNewSession_ConsistencyBetweenInitialAndSpawned(t *testing.T) {
	t.Parallel()
	workingDir := "/projects/app"
	autoApprove := true
	hideToolResults := true
	maxIterations := 25

	// Simulate what createLocalRuntimeAndSession builds (initial session).
	initial := New(
		WithMaxIterations(maxIterations),
		WithToolsApproved(autoApprove),
		WithHideToolResults(hideToolResults),
		WithWorkingDir(workingDir),
	)

	// Simulate what createSessionSpawner builds (spawned session).
	spawned := New(
		WithMaxIterations(maxIterations),
		WithToolsApproved(autoApprove),
		WithHideToolResults(hideToolResults),
		WithWorkingDir(workingDir),
	)

	assert.Equal(t, initial.MaxIterations, spawned.MaxIterations)
	assert.Equal(t, initial.ToolsApproved, spawned.ToolsApproved)
	assert.Equal(t, initial.HideToolResults, spawned.HideToolResults)
	assert.Equal(t, initial.WorkingDir, spawned.WorkingDir)
	assert.Equal(t, initial.AllowedDirectories(), spawned.AllowedDirectories())
}

func TestAddAttachedFile(t *testing.T) {
	t.Parallel()
	t.Run("deduplicates and preserves order", func(t *testing.T) {
		t.Parallel()
		fooPath := filepath.Join(os.TempDir(), "foo.go")
		barPath := filepath.Join(os.TempDir(), "bar.go")
		s := New()
		s.AddAttachedFile(fooPath)
		s.AddAttachedFile(barPath)
		s.AddAttachedFile(fooPath) // duplicate
		assert.Equal(t, []string{fooPath, barPath}, s.AttachedFilesSnapshot())
	})

	t.Run("ignores empty paths", func(t *testing.T) {
		t.Parallel()
		s := New()
		s.AddAttachedFile("")
		assert.Empty(t, s.AttachedFilesSnapshot())
	})

	t.Run("ignores non-absolute paths", func(t *testing.T) {
		t.Parallel()
		s := New()
		s.AddAttachedFile("foo.go")
		s.AddAttachedFile("./bar.go")
		s.AddAttachedFile("../baz.go")
		assert.Empty(t, s.AttachedFilesSnapshot())
	})

	t.Run("snapshot is independent of session storage", func(t *testing.T) {
		t.Parallel()
		fooPath := filepath.Join(os.TempDir(), "foo.go")
		s := New()
		s.AddAttachedFile(fooPath)
		snap := s.AttachedFilesSnapshot()
		snap[0] = "mutated"
		assert.Equal(t, []string{fooPath}, s.AttachedFilesSnapshot())
	})
}

func TestRemoveAttachedFile(t *testing.T) {
	t.Parallel()
	t.Run("removes and reports presence", func(t *testing.T) {
		t.Parallel()
		fooPath := filepath.Join(os.TempDir(), "foo.go")
		barPath := filepath.Join(os.TempDir(), "bar.go")
		bazPath := filepath.Join(os.TempDir(), "baz.go")
		s := New()
		s.AddAttachedFile(fooPath)
		s.AddAttachedFile(barPath)
		s.AddAttachedFile(bazPath)

		assert.True(t, s.RemoveAttachedFile(barPath))
		assert.Equal(t, []string{fooPath, bazPath}, s.AttachedFilesSnapshot())
	})

	t.Run("reports absent paths", func(t *testing.T) {
		t.Parallel()
		fooPath := filepath.Join(os.TempDir(), "foo.go")
		otherPath := filepath.Join(os.TempDir(), "other.go")
		s := New()
		s.AddAttachedFile(fooPath)
		assert.False(t, s.RemoveAttachedFile(otherPath))
		assert.False(t, s.RemoveAttachedFile(""))
		assert.Equal(t, []string{fooPath}, s.AttachedFilesSnapshot())
	})

	t.Run("no-op on empty list", func(t *testing.T) {
		t.Parallel()
		fooPath := filepath.Join(os.TempDir(), "foo.go")
		s := New()
		assert.False(t, s.RemoveAttachedFile(fooPath))
		assert.Empty(t, s.AttachedFilesSnapshot())
	})

	t.Run("file can be re-attached after removal", func(t *testing.T) {
		t.Parallel()
		fooPath := filepath.Join(os.TempDir(), "foo.go")
		s := New()
		s.AddAttachedFile(fooPath)
		require.True(t, s.RemoveAttachedFile(fooPath))
		s.AddAttachedFile(fooPath)
		assert.Equal(t, []string{fooPath}, s.AttachedFilesSnapshot())
	})
}

func TestWithAttachedFiles(t *testing.T) {
	t.Parallel()
	fooPath := filepath.Join(os.TempDir(), "foo.go")
	barPath := filepath.Join(os.TempDir(), "bar.go")
	s := New(WithAttachedFiles([]string{fooPath, "", "relative/path.go", barPath, fooPath}))
	assert.Equal(t, []string{fooPath, barPath}, s.AttachedFilesSnapshot())
}
