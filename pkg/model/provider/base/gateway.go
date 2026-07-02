package base

import (
	"cmp"
	"context"
	"errors"
	"net/url"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/httpclient"
)

// VerifyDockerGatewayAuth fails fast when gateway targets a trusted Docker
// domain but Docker Desktop's auth token is unavailable. Provider clients
// call it at construction time so a missing sign-in surfaces before the
// first request. Non-Docker gateways need no Desktop token and always pass.
func VerifyDockerGatewayAuth(ctx context.Context, env environment.Provider, gateway string) error {
	if !environment.IsTrustedDockerURL(gateway) {
		return nil
	}
	if token, _ := env.Get(ctx, environment.DockerDesktopTokenEnv); token == "" {
		return errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
	}
	return nil
}

// GatewayAuthToken returns a fresh Docker Desktop auth token when gateway
// targets a trusted Docker domain, or "" for other gateways. Gateway clients
// call it on every request because Desktop tokens are short-lived.
func GatewayAuthToken(ctx context.Context, env environment.Provider, gateway string) (string, error) {
	if !environment.IsTrustedDockerURL(gateway) {
		return "", nil
	}
	token, _ := env.Get(ctx, environment.DockerDesktopTokenEnv)
	if token == "" {
		return "", errors.New(NoDesktopTokenErrorMessage)
	}
	return token, nil
}

// GatewayHTTPOptions builds the httpclient options shared by all
// gateway-mode provider clients: the proxied base URL (the provider's public
// endpoint unless the model overrides base_url), provider/model identity,
// the gateway's query parameters, and the title-generation marker.
func GatewayHTTPOptions(gatewayURL *url.URL, defaultBaseURL string, cfg *latest.ModelConfig, generatingTitle bool) []httpclient.Opt {
	opts := []httpclient.Opt{
		httpclient.WithProxiedBaseURL(cmp.Or(cfg.BaseURL, defaultBaseURL)),
		httpclient.WithProvider(cfg.Provider),
		httpclient.WithModel(cfg.Model),
		httpclient.WithModelName(cfg.Name),
		httpclient.WithQuery(gatewayURL.Query()),
	}
	if generatingTitle {
		opts = append(opts, httpclient.WithHeader("X-Cagent-GeneratingTitle", "1"))
	}
	return opts
}
