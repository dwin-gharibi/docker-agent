package mcp

import (
	"os"
	"testing"
)

// TestMain swaps the OAuth helpers' SSRF-safe HTTP client for the
// loopback-allowing variant so tests can hit httptest.NewServer (which
// binds to 127.0.0.1). Production code keeps the safe client. The variant
// carries its own connection pool, so parallel tests closing httptest
// servers (which prunes http.DefaultTransport's pool) can't break its
// in-flight requests.
func TestMain(m *testing.M) {
	oauthHTTPClient = oauthHTTPClientForAllowPrivateIPs(true)
	os.Exit(m.Run())
}
