package skills

import (
	"os"
	"testing"

	"github.com/docker/docker-agent/pkg/httpclient"
)

// TestMain swaps the SSRF-safe HTTP client for the loopback-allowing
// variant so tests can hit httptest.NewServer (which binds to 127.0.0.1).
// Production code keeps the safe client.
func TestMain(m *testing.M) {
	skillsHTTPClient = httpclient.NewSafeClient(remoteHTTPTimeout, true)
	os.Exit(m.Run())
}
