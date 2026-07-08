package root

import (
	"context"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/recording"
)

// setupFakeProxy starts a fake proxy if fakeResponses is non-empty.
// It configures the runtime config's ModelsGateway to point to the proxy.
func setupFakeProxy(ctx context.Context, fakeResponses string, streamDelayMs int, runConfig *config.RuntimeConfig) (cleanup func() error, err error) {
	proxyURL, cleanupFn, err := recording.SetupFakeProxy(ctx, fakeResponses, streamDelayMs)
	if err != nil {
		return nil, err
	}

	if proxyURL != "" {
		runConfig.ModelsGateway = proxyURL
	}

	return cleanupFn, nil
}

// setupRecordingProxy starts a recording proxy if recordPath is non-empty.
// It configures the runtime config's ModelsGateway to point to the proxy.
// Any models gateway already configured becomes the proxy's upstream, so
// recording keeps routing (and auth) through the user's gateway.
func setupRecordingProxy(ctx context.Context, recordPath string, runConfig *config.RuntimeConfig) (cassettePath string, cleanup func() error, err error) {
	cassettePath, proxyURL, cleanupFn, err := recording.SetupRecordingProxy(ctx, recordPath, runConfig.ModelsGateway)
	if err != nil {
		return "", nil, err
	}

	if proxyURL != "" {
		runConfig.ModelsGateway = proxyURL
	}

	return cassettePath, cleanupFn, nil
}
