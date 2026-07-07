package root

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chatgpt"
)

func executeAuth(t *testing.T, args []string, opts ...authCmdOption) (string, error) {
	t.Helper()

	var buf bytes.Buffer
	cmd := newAuthCmd(opts...)
	cmd.SilenceErrors = true
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}

// withAuthTestSeams stubs the terminal check and the credential store so
// auth commands never open a browser or touch the real login.
func withAuthTestSeams(creds *chatgpt.Credentials, loginResult *chatgpt.LoginResult, loginErr error) (authCmdOption, *bool) {
	loginCalled := new(bool)
	return func(seams *authSeams) {
		seams.isTTY = func() bool { return true }
		seams.login = func(_ context.Context, _ io.Writer) (*chatgpt.LoginResult, error) {
			*loginCalled = true
			return loginResult, loginErr
		}
		seams.logout = func() (bool, error) { return creds != nil, nil }
		seams.load = func() (*chatgpt.Credentials, error) {
			if creds == nil {
				return nil, chatgpt.ErrNotLoggedIn
			}
			return creds, nil
		}
	}, loginCalled
}

func TestAuthLogin(t *testing.T) {
	t.Parallel()

	seams, loginCalled := withAuthTestSeams(nil, &chatgpt.LoginResult{Email: "user@example.com", Plan: "plus"}, nil)

	output, err := executeAuth(t, []string{"login", "chatgpt"}, seams)
	require.NoError(t, err)

	assert.True(t, *loginCalled)
	assert.Contains(t, output, "Signed in with your ChatGPT account.")
	assert.Contains(t, output, "user@example.com")
	assert.Contains(t, output, "docker agent run --model chatgpt/")
}

func TestAuthLogin_DefaultsToChatGPT(t *testing.T) {
	t.Parallel()

	seams, loginCalled := withAuthTestSeams(nil, &chatgpt.LoginResult{}, nil)

	_, err := executeAuth(t, []string{"login"}, seams)
	require.NoError(t, err)
	assert.True(t, *loginCalled)
}

func TestAuthLogin_UnknownProvider(t *testing.T) {
	t.Parallel()

	seams, loginCalled := withAuthTestSeams(nil, nil, nil)

	_, err := executeAuth(t, []string{"login", "grok"}, seams)
	require.ErrorContains(t, err, `unknown provider "grok"`)
	require.ErrorContains(t, err, "chatgpt")
	assert.False(t, *loginCalled)
}

func TestAuthLogin_NeedsTerminal(t *testing.T) {
	t.Parallel()

	seams, _ := withAuthTestSeams(nil, nil, nil)

	_, err := executeAuth(t, []string{"login", "chatgpt"}, seams, func(s *authSeams) {
		s.isTTY = func() bool { return false }
	})
	require.ErrorContains(t, err, "needs a browser and a terminal")
	require.ErrorContains(t, err, chatgpt.TokenEnvVar)
}

func TestAuthLogin_PropagatesLoginError(t *testing.T) {
	t.Parallel()

	seams, _ := withAuthTestSeams(nil, nil, errors.New("authorization failed: access_denied"))

	_, err := executeAuth(t, []string{"login", "chatgpt"}, seams)
	require.ErrorContains(t, err, "access_denied")
}

func TestAuthLogout(t *testing.T) {
	t.Parallel()

	seams, _ := withAuthTestSeams(&chatgpt.Credentials{AccessToken: "x"}, nil, nil)
	output, err := executeAuth(t, []string{"logout"}, seams)
	require.NoError(t, err)
	assert.Contains(t, output, "Signed out of chatgpt.")

	seams, _ = withAuthTestSeams(nil, nil, nil)
	output, err = executeAuth(t, []string{"logout", "chatgpt"}, seams)
	require.NoError(t, err)
	assert.Contains(t, output, "No chatgpt sign-in was stored.")
}

func TestAuthStatus(t *testing.T) {
	t.Parallel()

	expiry := time.Now().Add(2 * time.Hour)
	seams, _ := withAuthTestSeams(&chatgpt.Credentials{
		AccessToken:  "secret-access-token",
		RefreshToken: "secret-refresh-token",
		Email:        "user@example.com",
		Plan:         "pro",
		ExpiresAt:    expiry,
	}, nil, nil)

	output, err := executeAuth(t, []string{"status"}, seams)
	require.NoError(t, err)

	assert.Contains(t, output, "chatgpt: signed in")
	assert.Contains(t, output, "user@example.com")
	assert.Contains(t, output, "pro")
	assert.NotContains(t, output, "secret-access-token", "token values must never be printed")
	assert.NotContains(t, output, "secret-refresh-token", "token values must never be printed")
}

func TestAuthStatus_NotSignedIn(t *testing.T) {
	t.Parallel()

	seams, _ := withAuthTestSeams(nil, nil, nil)

	output, err := executeAuth(t, []string{"status"}, seams)
	require.NoError(t, err)
	assert.Contains(t, output, "chatgpt: not signed in")
	assert.Contains(t, output, "docker agent auth login chatgpt")
}

func TestAuthStatus_JSON(t *testing.T) {
	t.Parallel()

	seams, _ := withAuthTestSeams(&chatgpt.Credentials{
		AccessToken:  "secret",
		RefreshToken: "refresh",
		Email:        "user@example.com",
		Plan:         "plus",
		AccountID:    "acc_1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}, nil, nil)

	output, err := executeAuth(t, []string{"status", "--json"}, seams)
	require.NoError(t, err)

	var entries []authStatusEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "chatgpt", entries[0].Provider)
	assert.True(t, entries[0].SignedIn)
	assert.Equal(t, "user@example.com", entries[0].Email)
	assert.Equal(t, "plus", entries[0].Plan)
	assert.Equal(t, "acc_1", entries[0].AccountID)
	assert.True(t, entries[0].Refresh)
	assert.False(t, entries[0].Expired)
	assert.NotContains(t, output, "secret", "token values must never be serialized")
}
