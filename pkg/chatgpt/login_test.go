package chatgpt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startFakeIssuer serves the /oauth/token authorization-code exchange and
// records the form it received.
func startFakeIssuer(t *testing.T, accessToken, idToken string) (gotForm *url.Values) {
	t.Helper()
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/token", r.URL.Path)
		assert.NoError(t, r.ParseForm())
		form = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh-1",
			"id_token":      idToken,
			"expires_in":    3600,
		})
	}))
	t.Cleanup(server.Close)
	restore := setAuthBaseURLForTests(server.URL)
	t.Cleanup(restore)
	return &form
}

// fakeBrowser simulates the user completing (or failing) the IdP flow: it
// parses the authorization URL docker-agent would open and immediately calls
// the localhost callback back, like the real redirect would.
func fakeBrowser(t *testing.T, mutate func(callback url.Values)) func(context.Context, string) error {
	t.Helper()
	return func(_ context.Context, authURL string) error {
		parsed, err := url.Parse(authURL)
		require.NoError(t, err)
		query := parsed.Query()

		assert.Equal(t, "code", query.Get("response_type"))
		assert.Equal(t, clientID, query.Get("client_id"))
		assert.Equal(t, "S256", query.Get("code_challenge_method"))
		assert.NotEmpty(t, query.Get("code_challenge"))
		assert.Equal(t, "openid profile email offline_access", query.Get("scope"))
		assert.Equal(t, "true", query.Get("codex_cli_simplified_flow"))
		assert.Equal(t, Originator, query.Get("originator"))

		callback := url.Values{
			"code":  {"auth-code-1"},
			"state": {query.Get("state")},
		}
		if mutate != nil {
			mutate(callback)
		}

		// The redirect happens on a separate "browser" flow, not on the
		// goroutine that opened the URL.
		go func() {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
				query.Get("redirect_uri")+"?"+callback.Encode(), http.NoBody)
			if err != nil {
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin_EndToEnd(t *testing.T) {
	useTempCredentials(t)

	idClaims := chatgptClaims("acc_7", "plus")
	idClaims["email"] = "user@example.com"
	accessToken := makeJWT(t, chatgptClaims("acc_7", "plus"))
	idToken := makeJWT(t, idClaims)

	gotForm := startFakeIssuer(t, accessToken, idToken)
	restoreAddr := setCallbackAddrForTests("127.0.0.1:0")
	defer restoreAddr()
	restoreBrowser := setBrowserOpenerForTests(fakeBrowser(t, nil))
	defer restoreBrowser()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var out strings.Builder
	result, err := Login(ctx, &out)
	require.NoError(t, err)

	assert.Equal(t, "user@example.com", result.Email)
	assert.Equal(t, "plus", result.Plan)
	assert.Equal(t, "acc_7", result.AccountID)
	assert.Contains(t, out.String(), "/oauth/authorize", "the authorization URL is printed for manual use")

	assert.Equal(t, "authorization_code", gotForm.Get("grant_type"))
	assert.Equal(t, "auth-code-1", gotForm.Get("code"))
	assert.NotEmpty(t, gotForm.Get("code_verifier"))

	creds, err := Load()
	require.NoError(t, err)
	assert.Equal(t, accessToken, creds.AccessToken)
	assert.Equal(t, "refresh-1", creds.RefreshToken)
	assert.Equal(t, "acc_7", creds.AccountID)
	assert.Equal(t, "user@example.com", creds.Email)
	assert.WithinDuration(t, time.Now().Add(time.Hour), creds.ExpiresAt, 5*time.Second)
}

func TestLogin_StateMismatchIsRejected(t *testing.T) {
	useTempCredentials(t)

	startFakeIssuer(t, makeJWT(t, chatgptClaims("acc", "plus")), "")
	restoreAddr := setCallbackAddrForTests("127.0.0.1:0")
	defer restoreAddr()
	restoreBrowser := setBrowserOpenerForTests(fakeBrowser(t, func(callback url.Values) {
		callback.Set("state", "forged")
	}))
	defer restoreBrowser()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err := Login(ctx, io.Discard)
	require.ErrorContains(t, err, "state mismatch")
	assert.False(t, LoggedIn())
}

func TestLogin_AuthorizationErrorIsSurfaced(t *testing.T) {
	useTempCredentials(t)

	startFakeIssuer(t, "unused", "")
	restoreAddr := setCallbackAddrForTests("127.0.0.1:0")
	defer restoreAddr()
	restoreBrowser := setBrowserOpenerForTests(fakeBrowser(t, func(callback url.Values) {
		callback.Del("code")
		callback.Set("error", "access_denied")
		callback.Set("error_description", "the user cancelled")
	}))
	defer restoreBrowser()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err := Login(ctx, io.Discard)
	require.ErrorContains(t, err, "access_denied")
	assert.False(t, LoggedIn())
}
