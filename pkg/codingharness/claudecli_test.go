package codingharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClaudeCLI scripts the probe's exec seams and records every invocation,
// so the tests never touch the developer's real `claude` installation.
type fakeClaudeCLI struct {
	installed  bool
	version    string
	versionErr error
	authOut    string
	authErr    error

	commands [][]string
}

func (f *fakeClaudeCLI) probe() ClaudeCLIProbe {
	return ClaudeCLIProbe{
		LookPath: func(file string) (string, error) {
			if !f.installed {
				return "", errors.New("executable file not found in $PATH")
			}
			return "/usr/local/bin/" + file, nil
		},
		RunOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
			f.commands = append(f.commands, append([]string{name}, args...))
			switch {
			case len(args) == 1 && args[0] == "--version":
				return []byte(f.version), f.versionErr
			default:
				return []byte(f.authOut), f.authErr
			}
		},
	}
}

const fakeAuthStatusJSON = `{
  "loggedIn": true,
  "authMethod": "claude.ai",
  "apiProvider": "firstParty",
  "email": "dev@example.com",
  "orgId": "org-1234",
  "orgName": "Example Org",
  "subscriptionType": "pro"
}`

func TestClaudeCLIProbe_NotInstalled(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: false}
	status := fake.probe().Probe(t.Context())

	assert.Equal(t, ClaudeStateNotInstalled, status.State)
	assert.False(t, status.Installed())
	assert.False(t, status.Authenticated())
	assert.Contains(t, status.Detail, "not found in PATH")
	assert.Empty(t, fake.commands, "nothing must be executed when the binary is missing")
}

func TestClaudeCLIProbe_Authenticated(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: true, version: "2.1.210 (Claude Code)\n", authOut: fakeAuthStatusJSON}
	status := fake.probe().Probe(t.Context())

	assert.Equal(t, ClaudeStateAuthenticated, status.State)
	assert.True(t, status.Installed())
	assert.True(t, status.Authenticated())
	assert.Equal(t, "2.1.210 (Claude Code)", status.Version)
	assert.Equal(t, "claude.ai", status.AuthMethod)
	assert.Equal(t, "firstParty", status.APIProvider)
	assert.Equal(t, "pro", status.SubscriptionType)
	assert.Equal(t, "auth: claude.ai, api: firstParty, subscription: pro", status.AuthSummary())

	require.Equal(t, [][]string{
		{"claude", "--version"},
		{"claude", "auth", "status", "--json"},
	}, fake.commands, "the probe must call the CLI directly, without a shell")
}

// The status must never retain identity fields: marshalling it back must not
// leak anything the auth-status document carried beyond the safe fields.
func TestClaudeCLIProbe_DropsIdentityFields(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: true, authOut: fakeAuthStatusJSON}
	status := fake.probe().Probe(t.Context())

	marshalled, err := json.Marshal(status)
	require.NoError(t, err)
	for _, sensitive := range []string{"email", "dev@example.com", "org", "Example"} {
		assert.NotContains(t, string(marshalled), sensitive)
	}
}

func TestClaudeCLIProbe_Unauthenticated(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: true, version: "2.1.210 (Claude Code)", authOut: `{"loggedIn": false}`}
	status := fake.probe().Probe(t.Context())

	assert.Equal(t, ClaudeStateUnauthenticated, status.State)
	assert.True(t, status.Installed())
	assert.False(t, status.Authenticated())
	assert.Equal(t, "2.1.210 (Claude Code)", status.Version)
}

// A logged-out CLI may exit non-zero while still printing the status JSON:
// the parsed document wins over the exit code.
func TestClaudeCLIProbe_LoggedOutWithNonZeroExit(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: true, authOut: `{"loggedIn": false}`, authErr: errors.New("exit status 1")}
	status := fake.probe().Probe(t.Context())

	assert.Equal(t, ClaudeStateUnauthenticated, status.State)
}

func TestClaudeCLIProbe_AuthCommandFails(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: true, version: "2.1.210 (Claude Code)", authErr: errors.New("exit status 1")}
	status := fake.probe().Probe(t.Context())

	assert.Equal(t, ClaudeStateAuthCheckFailed, status.State)
	assert.True(t, status.Installed())
	assert.False(t, status.Authenticated())
	assert.Contains(t, status.Detail, "exit status 1")
	assert.Equal(t, "2.1.210 (Claude Code)", status.Version, "the version survives a failed auth check")
}

func TestClaudeCLIProbe_InvalidStatusJSON(t *testing.T) {
	t.Parallel()

	for name, out := range map[string]string{
		"not json":       "please run claude auth login",
		"missing key":    `{"status": "ok"}`,
		"null document":  "null",
		"empty output":   "",
		"wrong type":     `{"loggedIn": "yes"}`,
		"array document": `[true]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeClaudeCLI{installed: true, authOut: out}
			status := fake.probe().Probe(t.Context())

			assert.Equal(t, ClaudeStateAuthCheckFailed, status.State)
			assert.Contains(t, status.Detail, "unexpected output")
			if out != "" {
				assert.NotContains(t, status.Detail, out, "raw CLI output must never be surfaced")
			}
		})
	}
}

func TestClaudeCLIProbe_VersionFailureDoesNotDecideState(t *testing.T) {
	t.Parallel()

	fake := &fakeClaudeCLI{installed: true, versionErr: errors.New("boom"), authOut: fakeAuthStatusJSON}
	status := fake.probe().Probe(t.Context())

	assert.Equal(t, ClaudeStateAuthenticated, status.State)
	assert.Empty(t, status.Version)
}

// The default seams run a real executable found via PATH, directly and
// without a shell. A fake `claude` script stands in for the CLI.
func TestClaudeCLIProbe_DefaultSeamsUseRealExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude executable is a shell script")
	}

	binDir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then echo "9.9.9 (Claude Code)"; exit 0; fi
echo '%s'
`, `{"loggedIn": true, "authMethod": "claude.ai", "apiProvider": "firstParty", "subscriptionType": "max", "email": "dev@example.com"}`)
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o700))
	t.Setenv("PATH", binDir)

	status := ProbeClaudeCLI(t.Context())

	assert.Equal(t, ClaudeStateAuthenticated, status.State)
	assert.Equal(t, "9.9.9 (Claude Code)", status.Version)
	assert.Equal(t, "max", status.SubscriptionType)
}

func TestClaudeCLIProbe_MissingBinaryWithDefaultSeams(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	status := ProbeClaudeCLI(t.Context())

	assert.Equal(t, ClaudeStateNotInstalled, status.State)
}

func TestClaudeCLIStatus_AuthSummaryOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	assert.Empty(t, ClaudeCLIStatus{}.AuthSummary())
	assert.Equal(t, "subscription: pro", ClaudeCLIStatus{SubscriptionType: "pro"}.AuthSummary())
}
