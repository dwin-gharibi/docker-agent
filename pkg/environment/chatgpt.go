package environment

import (
	"context"
	"errors"
	"log/slog"

	"github.com/docker/docker-agent/pkg/chatgpt"
)

// chatGPTLoginProvider exposes the stored ChatGPT account login as the
// virtual CHATGPT_OAUTH_TOKEN variable. Serving the credential through the
// standard source chain lets every env-var-driven consumer (credential
// detection, doctor, first_available, model preflight, the chatgpt model
// provider itself) treat the browser login exactly like an API key, while
// still allowing an explicit CHATGPT_OAUTH_TOKEN from an earlier source
// (e.g. the OS environment) to win.
type chatGPTLoginProvider struct{}

// NewChatGPTLoginProvider returns the provider serving CHATGPT_OAUTH_TOKEN
// from the stored ChatGPT account login. The access token is refreshed
// transparently when it is close to expiry.
func NewChatGPTLoginProvider() Provider {
	return chatGPTLoginProvider{}
}

func (chatGPTLoginProvider) Get(ctx context.Context, name string) (string, bool) {
	if name != chatgpt.TokenEnvVar {
		return "", false
	}
	token, err := chatgpt.AccessToken(ctx)
	if err != nil {
		if !errors.Is(err, chatgpt.ErrNotLoggedIn) {
			slog.DebugContext(ctx, "ChatGPT login could not supply an access token", "error", err)
		}
		return "", false
	}
	return token, true
}
