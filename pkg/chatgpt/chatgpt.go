// Package chatgpt implements "Sign in with ChatGPT" authentication for the
// chatgpt model provider.
//
// It performs the same OAuth 2.0 authorization-code + PKCE flow as OpenAI's
// Codex CLI (issuer https://auth.openai.com, public Codex client ID, fixed
// localhost callback on port 1455) so a ChatGPT Plus/Pro/Business
// subscription can be used instead of an OPENAI_API_KEY. The resulting
// access/refresh tokens authenticate requests against the ChatGPT Codex
// backend (https://chatgpt.com/backend-api/codex), which serves the gpt-5
// model family over the Responses API.
//
// Credentials are stored in <config-dir>/chatgpt-auth.json (owner-only), the
// same layout Codex CLI uses for ~/.codex/auth.json, and the access token is
// refreshed transparently when it approaches expiry.
package chatgpt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/atomicfile"
	"github.com/docker/docker-agent/pkg/paths"
)

const (
	// ProviderName is the built-in model provider name backed by this login.
	ProviderName = "chatgpt"

	// TokenEnvVar is the virtual environment variable through which the
	// stored login is exposed to the rest of docker-agent (credential
	// detection, doctor, first_available, ...). It resolves to a fresh
	// access token via the "chatgpt-login" environment source, and can be
	// set explicitly to inject a pre-minted access token (e.g. in CI).
	TokenEnvVar = "CHATGPT_OAUTH_TOKEN"

	// BaseURL is the ChatGPT Codex backend served to signed-in accounts.
	// It only exposes the Responses API (/responses).
	BaseURL = "https://chatgpt.com/backend-api/codex"

	// Originator identifies the client integration to the Codex backend.
	// The backend rejects requests from unknown originators, so we reuse
	// the Codex CLI value that is valid for every ChatGPT plan.
	Originator = "codex_cli_rs"

	// authFileName is the credentials file under the config directory.
	authFileName = "chatgpt-auth.json"

	// clientID is OpenAI's public OAuth client for Codex CLI sign-in. It is
	// a public (non-secret) identifier; PKCE protects the exchange.
	clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// ErrNotLoggedIn is returned when no ChatGPT account login is stored.
var ErrNotLoggedIn = errors.New("not signed in with a ChatGPT account")

// Credentials is the persisted result of a ChatGPT account login.
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	Plan         string    `json:"plan,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	LastRefresh  time.Time `json:"last_refresh,omitzero"`
}

// Expired reports whether the access token is expired or about to expire.
// A zero ExpiresAt means the expiry could not be determined; the token is
// then used as-is and a 401 from the backend surfaces naturally.
func (c *Credentials) Expired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	// Refresh slightly early so an in-flight request never carries a token
	// that expires mid-stream.
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

// storeMu serializes load/refresh/save so concurrent model clients in one
// process trigger at most one token refresh.
var storeMu sync.Mutex

// pathOverride redirects credential storage in tests. Production always
// derives the path from the config directory.
var pathOverride atomic.Pointer[string]

// SetCredentialsPathForTests points credential storage at path and returns a
// restore function. Without an override, every credential operation under
// `go test` behaves as "not signed in" so the developer's real login is never
// read, refreshed over the network, or overwritten by tests.
func SetCredentialsPathForTests(path string) (restore func()) {
	if !testing.Testing() {
		panic("SetCredentialsPathForTests called outside of tests")
	}
	pathOverride.Store(&path)
	return func() { pathOverride.Store(nil) }
}

// credentialsPath returns the credentials file location. ok is false when
// running under `go test` without an explicit override.
func credentialsPath() (path string, ok bool) {
	if p := pathOverride.Load(); p != nil {
		return *p, true
	}
	if testing.Testing() {
		return "", false
	}
	return filepath.Join(paths.GetConfigDir(), authFileName), true
}

// LoggedIn reports whether a ChatGPT account login is stored.
func LoggedIn() bool {
	_, err := Load()
	return err == nil
}

// Load returns the stored credentials without refreshing them. Callers that
// need a usable token should use AccessToken instead.
func Load() (*Credentials, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return load()
}

func load() (*Credentials, error) {
	path, ok := credentialsPath()
	if !ok {
		return nil, ErrNotLoggedIn
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read ChatGPT credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("ChatGPT credentials file %s is corrupt: %w", path, err)
	}
	if creds.AccessToken == "" {
		return nil, ErrNotLoggedIn
	}
	return &creds, nil
}

func save(creds *Credentials) error {
	path, ok := credentialsPath()
	if !ok {
		return errors.New("ChatGPT credential storage is unavailable under tests")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ") //nolint:gosec // credentials are intentionally serialized for storage
	if err != nil {
		return fmt.Errorf("failed to marshal ChatGPT credentials: %w", err)
	}
	if err := atomicfile.Write(path, bytes.NewReader(data), 0o600); err != nil {
		return fmt.Errorf("failed to write ChatGPT credentials: %w", err)
	}
	return nil
}

// Logout removes the stored login. It reports whether a login existed.
func Logout() (bool, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	path, ok := credentialsPath()
	if !ok {
		return false, nil
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to remove ChatGPT credentials: %w", err)
	}
	return true, nil
}
