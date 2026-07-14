package sandbox

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeExecutor struct {
	spec RunSpec
	res  Result
	err  error
}

func (f *fakeExecutor) Run(_ context.Context, spec RunSpec) (Result, error) {
	f.spec = spec
	return f.res, f.err
}

func newTestToolSet(exec Executor) *ToolSet {
	ts := New()
	ts.exec = exec
	return ts
}

func TestBuildRunArgs(t *testing.T) {
	t.Parallel()

	args, err := buildRunArgs(RunSpec{Language: "python", Code: "print(1)"})
	require.NoError(t, err)
	require.Equal(t, "run", args[0])
	require.Contains(t, args, "--rm")
	require.Contains(t, args, "-i")
	require.Contains(t, args, "--network")
	require.Contains(t, args, "none")
	require.Contains(t, args, "--memory")
	require.Contains(t, args, "--pids-limit")
	require.Contains(t, args, "python:3.12-slim")
}

func TestBuildRunArgsNetworkOptIn(t *testing.T) {
	t.Parallel()

	args, err := buildRunArgs(RunSpec{Language: "node", Code: "x", Network: true})
	require.NoError(t, err)
	require.False(t, slices.Contains(args, "none"), args)
	require.Contains(t, args, "node:22-alpine")
}

func TestBuildRunArgsUnknownLanguage(t *testing.T) {
	t.Parallel()

	_, err := buildRunArgs(RunSpec{Language: "cobol", Code: "x"})
	require.Error(t, err)
}

func TestResolveLangAliases(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"py", "python"}, {"python3", "python"}, {"Python", "python"},
		{"js", "node"}, {"javascript", "node"},
		{"sh", "bash"}, {"shell", "bash"},
	} {
		_, got, ok := resolveLang(tc.in)
		require.True(t, ok, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
}

func TestRunCodeSuccess(t *testing.T) {
	t.Parallel()

	fe := &fakeExecutor{res: Result{Stdout: "hello\n", ExitCode: 0}}
	ts := newTestToolSet(fe)

	res, err := ts.runCode(context.Background(), RunCodeArgs{Language: "python", Code: "print('hello')"})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)
	require.Contains(t, res.Output, "hello")
	require.Equal(t, "python", fe.spec.Language)
	require.Equal(t, "print('hello')", fe.spec.Code)
	require.False(t, fe.spec.Network)
}

func TestRunCodeNonZeroExit(t *testing.T) {
	t.Parallel()

	fe := &fakeExecutor{res: Result{Stderr: "boom\n", ExitCode: 1}}
	ts := newTestToolSet(fe)

	res, err := ts.runCode(context.Background(), RunCodeArgs{Language: "bash", Code: "exit 1"})
	require.NoError(t, err)
	require.Contains(t, res.Output, "1")
	require.Contains(t, res.Output, "boom")
}

func TestRunCodeRequiresCode(t *testing.T) {
	t.Parallel()

	ts := newTestToolSet(&fakeExecutor{})
	res, err := ts.runCode(context.Background(), RunCodeArgs{Language: "python", Code: "   "})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestRunCodeUnknownLanguage(t *testing.T) {
	t.Parallel()

	ts := newTestToolSet(&fakeExecutor{})
	res, err := ts.runCode(context.Background(), RunCodeArgs{Language: "rust", Code: "fn main(){}"})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestRunCodeTimeoutCapped(t *testing.T) {
	t.Parallel()

	fe := &fakeExecutor{res: Result{Stdout: "ok"}}
	ts := newTestToolSet(fe)

	_, err := ts.runCode(context.Background(), RunCodeArgs{Language: "python", Code: "x", TimeoutSeconds: 99999})
	require.NoError(t, err)
	require.Equal(t, ts.maxTimeout, fe.spec.Timeout)
}

func TestRunCodeExecutorError(t *testing.T) {
	t.Parallel()

	fe := &fakeExecutor{err: context.DeadlineExceeded}
	ts := newTestToolSet(fe)

	res, err := ts.runCode(context.Background(), RunCodeArgs{Language: "python", Code: "x"})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestToolSetInterfaces(t *testing.T) {
	t.Parallel()

	ts := New()
	require.NotEmpty(t, ts.Instructions())
	toolz, err := ts.Tools(context.Background())
	require.NoError(t, err)
	require.Len(t, toolz, 1)
	require.Equal(t, ToolNameRunCode, toolz[0].Name)
	require.Positive(t, ts.defaultTimeout)
	_ = time.Second
}
