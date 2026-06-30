package main

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestParallelFlagsMissingParallel(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(t *testing.T) {
	_ = 1
}
`
	offenses := coptest.RunNamed(t, TestParallel, "foo_test.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/TestParallel", offenses[0].CopName)
}

func TestTestParallelAllowsParallel(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(t *testing.T) {
	t.Parallel()
	_ = 1
}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo_test.go", src))
}

// t.Setenv anywhere in the body (here at top level) makes the test
// incompatible with parallelism, so it must not be flagged.
func TestTestParallelAllowsSetenv(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(t *testing.T) {
	t.Setenv("K", "V")
}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo_test.go", src))
}

// t.Chdir inside a subtest closure also rules the whole test out of parallelism.
func TestTestParallelAllowsChdirInSubtest(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		t.Chdir("/tmp")
	})
}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo_test.go", src))
}

// A parent that only parallelises its subtests still needs its own call.
func TestTestParallelFlagsWhenOnlySubtestParallel(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		t.Parallel()
	})
}
`
	offenses := coptest.RunNamed(t, TestParallel, "foo_test.go", src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/TestParallel", offenses[0].CopName)
}

func TestTestParallelIgnoresTestMain(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestMain(m *testing.M) {
	m.Run()
}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo_test.go", src))
}

// An unnamed *testing.T parameter offers no handle to call Parallel() on.
func TestTestParallelIgnoresUnnamedParam(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(_ *testing.T) {}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo_test.go", src))
}

// Helpers and non-test functions are not in scope.
func TestTestParallelIgnoresNonTestFuncs(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func helper(t *testing.T) {}
func TestableThing() {}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo_test.go", src))
}

// The cop only runs on *_test.go files.
func TestTestParallelIgnoresNonTestFile(t *testing.T) {
	t.Parallel()
	src := `package p
import "testing"
func TestFoo(t *testing.T) {}
`
	assert.Empty(t, coptest.RunNamed(t, TestParallel, "foo.go", src))
}
