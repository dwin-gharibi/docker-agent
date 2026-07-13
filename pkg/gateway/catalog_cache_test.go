package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
)

const (
	etagV1 = `"v1"`
	etagV2 = `"v2"`

	catalogBodyV1 = `{"registry":{"fetch":{"type":"server"}}}`
	catalogBodyV2 = `{"registry":{"fetch":{"type":"server"},"github-official":{"type":"server"}}}`
)

var (
	catalogV1 = Catalog{"fetch": {Type: "server"}}
	catalogV2 = Catalog{
		"fetch":           {Type: "server"},
		"github-official": {Type: "server"},
	}
)

// fakeCatalogTransport serves canned responses for the fixed catalog URL and
// records the If-None-Match header of every request. It is deliberately not
// an *http.Transport: remote.NewTransport returns any other RoundTripper
// unchanged, so these tests never probe Docker Desktop (whose availability is
// memoized process-wide) and never touch the network.
type fakeCatalogTransport struct {
	respond func(req *http.Request) (*http.Response, error)

	mu          sync.Mutex
	ifNoneMatch []string
}

func (f *fakeCatalogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != catalogJSON {
		return nil, errors.New("unexpected request URL: " + req.URL.String())
	}
	f.mu.Lock()
	f.ifNoneMatch = append(f.ifNoneMatch, req.Header.Get("If-None-Match"))
	f.mu.Unlock()
	return f.respond(req)
}

func (f *fakeCatalogTransport) sentIfNoneMatch() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ifNoneMatch)
}

func catalogResponse(status int, etag, body string) *http.Response {
	header := http.Header{}
	if etag != "" {
		header.Set("ETag", etag)
	}
	return &http.Response{
		StatusCode: status,
		Status:     strconv.Itoa(status) + " " + http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func respondWith(status int, etag, body string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		return catalogResponse(status, etag, body), nil
	}
}

// catalogFetchScenarioEnv names the scenario TestCatalogFetchScenarioHelper
// must run; runCatalogFetchScenario sets it in the child's environment.
const catalogFetchScenarioEnv = "GATEWAY_CATALOG_FETCH_SCENARIO"

type catalogFetchScenario struct {
	// respond serves the canned response for the catalog request.
	respond func(*http.Request) (*http.Response, error)
	// seedCache pre-populates the on-disk cache with catalogV1/etagV1.
	seedCache bool
	// run is the scenario body, executed inside the child process.
	run func(t *testing.T, cacheFile string, rt *fakeCatalogTransport)
}

// catalogFetchScenarios are the fetchAndCache/fetchFromNetwork cases that
// depend on process globals (http.DefaultTransport and the paths cache-dir
// override). Each runs in its own child process via runCatalogFetchScenario,
// so the global swaps can never leak into other tests in this binary.
var catalogFetchScenarios = map[string]catalogFetchScenario{
	"initial fetch persists catalog and etag": {
		respond: respondWith(http.StatusOK, etagV1, catalogBodyV1),
		run: func(t *testing.T, cacheFile string, rt *fakeCatalogTransport) {
			t.Helper()
			catalog, err := fetchAndCache(t.Context())
			require.NoError(t, err)
			assert.Equal(t, catalogV1, catalog)

			// First fetch has no cached ETag, so the request must be unconditional.
			assert.Equal(t, []string{""}, rt.sentIfNoneMatch())

			cached := loadFromDisk(cacheFile)
			assert.Equal(t, catalogV1, cached.Catalog)
			assert.Equal(t, etagV1, cached.ETag)
		},
	},
	"not modified reuses cache": {
		respond:   respondWith(http.StatusNotModified, "", ""),
		seedCache: true,
		run: func(t *testing.T, cacheFile string, rt *fakeCatalogTransport) {
			t.Helper()
			catalog, err := fetchAndCache(t.Context())
			require.NoError(t, err)
			assert.Equal(t, catalogV1, catalog)
			assert.Equal(t, []string{etagV1}, rt.sentIfNoneMatch())

			cached := loadFromDisk(cacheFile)
			assert.Equal(t, catalogV1, cached.Catalog)
			assert.Equal(t, etagV1, cached.ETag)
		},
	},
	"refreshed fetch updates cache and etag": {
		respond:   respondWith(http.StatusOK, etagV2, catalogBodyV2),
		seedCache: true,
		run: func(t *testing.T, cacheFile string, rt *fakeCatalogTransport) {
			t.Helper()
			catalog, err := fetchAndCache(t.Context())
			require.NoError(t, err)
			assert.Equal(t, catalogV2, catalog)
			assert.Equal(t, []string{etagV1}, rt.sentIfNoneMatch())

			cached := loadFromDisk(cacheFile)
			assert.Equal(t, catalogV2, cached.Catalog)
			assert.Equal(t, etagV2, cached.ETag)
		},
	},
	"network error falls back to stale cache": {
		respond: func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		},
		seedCache: true,
		run:       assertStaleCacheSurvivesFailedRefresh,
	},
	"unexpected status falls back to stale cache": {
		respond:   respondWith(http.StatusInternalServerError, "", ""),
		seedCache: true,
		run:       assertStaleCacheSurvivesFailedRefresh,
	},
	"malformed body falls back to stale cache": {
		respond:   respondWith(http.StatusOK, etagV2, "{not json"),
		seedCache: true,
		run:       assertStaleCacheSurvivesFailedRefresh,
	},
	"failure without cache returns error": {
		respond: func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		},
		run: func(t *testing.T, _ string, _ *fakeCatalogTransport) {
			t.Helper()
			catalog, err := fetchAndCache(t.Context())
			require.ErrorContains(t, err, "fetching MCP catalog")
			require.ErrorContains(t, err, "no cached copy available")
			require.ErrorContains(t, err, "connection reset")
			assert.Nil(t, catalog)
		},
	},
	// A 304 can only follow an If-None-Match header, which requires a cached
	// copy; if a server nevertheless sends one against an empty cache, the
	// current behaviour is a nil catalog with no error. This scenario
	// documents that behaviour without endorsing it.
	"not modified without cache returns nil catalog": {
		respond: respondWith(http.StatusNotModified, "", ""),
		run: func(t *testing.T, _ string, rt *fakeCatalogTransport) {
			t.Helper()
			catalog, err := fetchAndCache(t.Context())
			require.NoError(t, err)
			assert.Nil(t, catalog)
			assert.Equal(t, []string{""}, rt.sentIfNoneMatch())
		},
	},
	// A zero-value Loader must fall back to the production fetch path instead
	// of panicking inside once.Do.
	"zero-value loader uses production fetch": {
		respond: respondWith(http.StatusOK, etagV1, catalogBodyV1),
		run: func(t *testing.T, _ string, _ *fakeCatalogTransport) {
			t.Helper()
			var loader Loader
			catalog, err := loader.load(t.Context())
			require.NoError(t, err)
			assert.Equal(t, catalogV1, catalog)
		},
	},
}

func assertStaleCacheSurvivesFailedRefresh(t *testing.T, cacheFile string, _ *fakeCatalogTransport) {
	t.Helper()
	catalog, err := fetchAndCache(t.Context())
	require.NoError(t, err)
	assert.Equal(t, catalogV1, catalog)

	// The stale cache must survive the failed refresh.
	cached := loadFromDisk(cacheFile)
	assert.Equal(t, catalogV1, cached.Catalog)
	assert.Equal(t, etagV1, cached.ETag)
}

// TestCatalogFetchScenarioHelper is not a standalone test: it is the child
// process body spawned by runCatalogFetchScenario and skips during a normal
// test sweep. Because it is the only test running in its process, it may
// safely swap the process globals fetchAndCache depends on.
func TestCatalogFetchScenarioHelper(t *testing.T) {
	name := os.Getenv(catalogFetchScenarioEnv)
	if name == "" {
		t.Skipf("helper process for catalog fetch scenarios; spawned with %s set", catalogFetchScenarioEnv)
	}
	scenario, ok := catalogFetchScenarios[name]
	require.Truef(t, ok, "unknown catalog fetch scenario %q", name)

	cacheDir := t.TempDir()
	paths.SetCacheDir(cacheDir)
	t.Cleanup(func() { paths.SetCacheDir("") })

	rt := &fakeCatalogTransport{respond: scenario.respond}
	previous := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = previous })

	cacheFile := filepath.Join(cacheDir, catalogCacheFileName)
	if scenario.seedCache {
		saveToDisk(cacheFile, catalogV1, etagV1)
	}

	scenario.run(t, cacheFile, rt)
}

// runCatalogFetchScenario runs the named catalogFetchScenarios entry in a
// child process by re-executing this test binary filtered down to
// TestCatalogFetchScenarioHelper. Confining the global swaps to a throwaway
// child keeps this binary's own state pristine, so callers are free to use
// t.Parallel.
func runCatalogFetchScenario(t *testing.T, name string) {
	t.Helper()

	_, ok := catalogFetchScenarios[name]
	require.Truef(t, ok, "unknown catalog fetch scenario %q", name)

	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestCatalogFetchScenarioHelper$", "-test.v", "-test.timeout=1m")
	cmd.Env = append(os.Environ(), catalogFetchScenarioEnv+"="+name)

	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "catalog fetch scenario %q failed:\n%s", name, out)

	// Guard against a silent no-op: if the helper were renamed or skipped,
	// every scenario would otherwise "pass" without running anything.
	require.Containsf(t, string(out), "--- PASS: TestCatalogFetchScenarioHelper",
		"catalog fetch scenario %q did not run the helper:\n%s", name, out)
}

func TestFetchAndCache_initialFetchPersistsCatalogAndETag(t *testing.T) {
	t.Parallel()
	runCatalogFetchScenario(t, "initial fetch persists catalog and etag")
}

func TestFetchAndCache_notModifiedReusesCache(t *testing.T) {
	t.Parallel()
	runCatalogFetchScenario(t, "not modified reuses cache")
}

func TestFetchAndCache_refreshedFetchUpdatesCacheAndETag(t *testing.T) {
	t.Parallel()
	runCatalogFetchScenario(t, "refreshed fetch updates cache and etag")
}

func TestFetchAndCache_fallsBackToStaleCacheOnFailure(t *testing.T) {
	t.Parallel()

	scenarios := []string{
		"network error falls back to stale cache",
		"unexpected status falls back to stale cache",
		"malformed body falls back to stale cache",
	}
	for _, name := range scenarios {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runCatalogFetchScenario(t, name)
		})
	}
}

func TestFetchAndCache_failureWithoutCacheReturnsError(t *testing.T) {
	t.Parallel()
	runCatalogFetchScenario(t, "failure without cache returns error")
}

func TestFetchAndCache_notModifiedWithoutCacheReturnsNilCatalog(t *testing.T) {
	t.Parallel()
	runCatalogFetchScenario(t, "not modified without cache returns nil catalog")
}

func TestLoader_zeroValueUsesProductionFetch(t *testing.T) {
	t.Parallel()
	runCatalogFetchScenario(t, "zero-value loader uses production fetch")
}

func TestLoadFromDisk(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		cached := loadFromDisk(filepath.Join(t.TempDir(), catalogCacheFileName))
		assert.Equal(t, cachedCatalog{}, cached)
	})

	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), catalogCacheFileName)
		require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

		cached := loadFromDisk(path)
		assert.Equal(t, cachedCatalog{}, cached)
	})

	t.Run("valid cache", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), catalogCacheFileName)
		data := `{"catalog":{"fetch":{"type":"server"}},"etag":"\"v1\""}`
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

		cached := loadFromDisk(path)
		assert.Equal(t, catalogV1, cached.Catalog)
		assert.Equal(t, etagV1, cached.ETag)
	})
}

func TestSaveToDisk_createsDirectoryAndWritesValidCache(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "cache", catalogCacheFileName)
	saveToDisk(path, catalogV1, etagV1)

	cached := loadFromDisk(path)
	assert.Equal(t, catalogV1, cached.Catalog)
	assert.Equal(t, etagV1, cached.ETag)

	// The temp file used for the atomic rename must not linger.
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, catalogCacheFileName, entries[0].Name())
}

func TestLoader_memoizesFetch(t *testing.T) {
	t.Parallel()

	calls := 0
	loader := &Loader{fetch: func(context.Context) (Catalog, error) {
		calls++
		return catalogV1, nil
	}}

	for range 3 {
		catalog, err := loader.load(t.Context())
		require.NoError(t, err)
		assert.Equal(t, catalogV1, catalog)
	}
	assert.Equal(t, 1, calls)
}

func TestLoader_memoizesFetchError(t *testing.T) {
	t.Parallel()

	fetchErr := errors.New("catalog unavailable")
	calls := 0
	loader := &Loader{fetch: func(context.Context) (Catalog, error) {
		calls++
		return nil, fetchErr
	}}

	for range 2 {
		_, err := loader.load(t.Context())
		require.ErrorIs(t, err, fetchErr)
	}
	assert.Equal(t, 1, calls)
}

func TestWithLoader_nilLoaderFallsBackToDefault(t *testing.T) {
	t.Parallel()

	ctx := WithLoader(t.Context(), nil)
	assert.Same(t, defaultLoader, loaderFrom(ctx))
}

func TestLoader_loadDetachesCallerCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var fetchCtxErr error
	loader := &Loader{fetch: func(fetchCtx context.Context) (Catalog, error) {
		fetchCtxErr = fetchCtx.Err()
		return catalogV1, nil
	}}

	catalog, err := loader.load(ctx)
	require.NoError(t, err)
	assert.Equal(t, catalogV1, catalog)
	require.NoError(t, fetchCtxErr, "a cancelled first caller must not poison the memoized result")
}
