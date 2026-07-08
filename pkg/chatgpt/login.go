package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/docker/docker-agent/pkg/browser"
	"github.com/docker/docker-agent/pkg/httpclient"
)

// The redirect URI is registered with OpenAI's OAuth client, so both the
// port and the path are fixed: the flow cannot fall back to a random port
// the way dynamic-registration MCP OAuth does.
const (
	callbackPath = "/auth/callback"

	// loginTimeout bounds how long the login waits for the user to complete
	// the browser flow. Matches the MCP OAuth wait budget.
	loginTimeout = 10 * time.Minute
)

// callbackAddr is a variable so tests can bind an ephemeral port.
var callbackAddr = "127.0.0.1:1455"

// openBrowser is a seam for tests; production opens the system browser.
var openBrowser = browser.Open

// LoginResult summarizes a completed login for display.
type LoginResult struct {
	Email     string
	Plan      string
	AccountID string
}

// Login runs the interactive "Sign in with ChatGPT" flow: it starts the
// localhost callback server, opens the browser on the authorization URL
// (also printed to out for manual use), exchanges the authorization code,
// and persists the resulting credentials.
func Login(ctx context.Context, out io.Writer) (*LoginResult, error) {
	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", callbackAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s (is another sign-in already in progress?): %w", callbackAddr, err)
	}

	redirectURI := "http://localhost:1455" + callbackPath
	if callbackAddr != "127.0.0.1:1455" {
		// Tests bind an ephemeral port; keep the redirect consistent with it.
		redirectURI = "http://" + listener.Addr().String() + callbackPath
	}

	type outcome struct {
		creds *Credentials
		err   error
	}
	done := make(chan outcome, 1)
	report := func(creds *Credentials, err error) {
		select {
		case done <- outcome{creds: creds, err: err}:
		default:
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if errCode := query.Get("error"); errCode != "" {
			description := query.Get("error_description")
			writeCallbackPage(w, http.StatusBadRequest, "Sign-in failed", errCode+": "+description)
			report(nil, fmt.Errorf("authorization failed: %s: %s", errCode, description))
			return
		}
		if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 {
			writeCallbackPage(w, http.StatusBadRequest, "Sign-in failed", "state mismatch in authorization response")
			report(nil, errors.New("state mismatch in authorization response"))
			return
		}
		code := query.Get("code")
		if code == "" {
			writeCallbackPage(w, http.StatusBadRequest, "Sign-in failed", "no authorization code in callback")
			report(nil, errors.New("no authorization code in callback"))
			return
		}

		creds, err := exchangeAuthorizationCode(r.Context(), code, verifier, redirectURI)
		if err != nil {
			writeCallbackPage(w, http.StatusInternalServerError, "Sign-in failed", err.Error())
			report(nil, err)
			return
		}
		writeCallbackPage(w, http.StatusOK, "Signed in to ChatGPT", "You can close this tab and return to the terminal.")
		report(creds, nil)
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			report(nil, fmt.Errorf("callback server failed: %w", err))
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.WarnContext(ctx, "Failed to shut down ChatGPT login callback server", "error", err)
		}
	}()

	authURL := buildAuthorizationURL(redirectURI, state, oauth2.S256ChallengeFromVerifier(verifier))
	fmt.Fprintf(out, "Opening your browser to sign in with your ChatGPT account...\n\n")
	fmt.Fprintf(out, "If the browser does not open, visit:\n  %s\n\n", authURL)
	if err := openBrowser(ctx, authURL); err != nil {
		slog.WarnContext(ctx, "Failed to open the browser for ChatGPT sign-in", "error", err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			return nil, result.err
		}
		storeMu.Lock()
		err := save(result.creds)
		storeMu.Unlock()
		if err != nil {
			return nil, err
		}
		return &LoginResult{
			Email:     result.creds.Email,
			Plan:      result.creds.Plan,
			AccountID: result.creds.AccountID,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(loginTimeout):
		return nil, fmt.Errorf("timed out after %s waiting for the browser sign-in to complete", loginTimeout)
	}
}

// buildAuthorizationURL composes the auth.openai.com authorize URL with the
// Codex-specific parameters (simplified flow, originator, organizations in
// the id_token) that the ChatGPT sign-in expects.
func buildAuthorizationURL(redirectURI, state, challenge string) string {
	query := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {Originator},
	}
	return authBaseURL + "/oauth/authorize?" + query.Encode()
}

// exchangeAuthorizationCode trades the authorization code for tokens and
// derives the stored credentials from them.
func exchangeAuthorizationCode(ctx context.Context, code, verifier, redirectURI string) (*Credentials, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authBaseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpclient.NewHTTPClient(ctx).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange the authorization code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("failed to exchange the authorization code: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("token response contained no access token")
	}

	creds := &Credentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      token.IDToken,
		ExpiresAt:    expiryFromToken(token),
		LastRefresh:  time.Now(),
	}
	applyClaims(creds)
	return creds, nil
}

func randomState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func writeCallbackPage(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>%[1]s</title></head>
<body style="font-family: system-ui, sans-serif; text-align: center; padding-top: 4rem;">
<h1>%[1]s</h1>
<p>%[2]s</p>
</body>
</html>`, html.EscapeString(title), html.EscapeString(detail))
}
