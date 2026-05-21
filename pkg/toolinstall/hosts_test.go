package toolinstall

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPackageInstallHosts_GoPackage(t *testing.T) {
	t.Parallel()

	// Go packages go through `go install`, which pulls from the Go
	// module proxy + checksum DB and may bootstrap a newer toolchain
	// from go.dev / dl.google.com / storage.googleapis.com. The
	// registry lookup itself still hits raw.githubusercontent.com
	// and api.github.com, so those must be present too.
	pkg := &Package{Type: "go_install", RepoOwner: "golang", RepoName: "tools"}

	got := pkg.InstallHosts()

	assert.ElementsMatch(t, []string{
		"raw.githubusercontent.com",
		"api.github.com",
		"proxy.golang.org",
		"sum.golang.org",
		"go.dev",
		"dl.google.com",
		"storage.googleapis.com",
	}, got, "go_install package must allow Go module proxy + toolchain bootstrap, not GitHub releases")

	// Crucially, GitHub release hosts must not be opened for a Go
	// package — `go install` never reaches them.
	assert.NotContains(t, got, "github.com")
	assert.NotContains(t, got, "objects.githubusercontent.com")
	assert.NotContains(t, got, "codeload.github.com")
}

func TestPackageInstallHosts_GitHubRelease(t *testing.T) {
	t.Parallel()

	pkg := &Package{Type: "github_release", RepoOwner: "junegunn", RepoName: "fzf"}

	got := pkg.InstallHosts()

	assert.ElementsMatch(t, []string{
		"raw.githubusercontent.com",
		"api.github.com",
		"github.com",
		"objects.githubusercontent.com",
		"codeload.github.com",
	}, got, "github_release package must allow GitHub release hosts, not the Go module proxy")

	// And a github_release tool must NOT punch holes for the Go
	// toolchain bootstrap — those hosts have nothing to do with it.
	assert.NotContains(t, got, "proxy.golang.org")
	assert.NotContains(t, got, "go.dev")
	assert.NotContains(t, got, "dl.google.com")
}

func TestPackageInstallHosts_SortedAndDeduped(t *testing.T) {
	t.Parallel()

	pkg := &Package{Type: "go_install"}
	got := pkg.InstallHosts()

	for i := 1; i < len(got); i++ {
		assert.Less(t, got[i-1], got[i],
			"InstallHosts must return sorted, dedup'd entries; got %v", got)
	}
}

func TestFallbackHosts_CoversBothPaths(t *testing.T) {
	t.Parallel()

	// FallbackHosts is what we open when we *cannot* narrow per
	// package (registry unreachable, no cache). It must therefore
	// cover every host any install path might reach — both Go
	// module proxy + toolchain AND GitHub releases — otherwise the
	// fallback is silently incomplete.
	got := FallbackHosts()

	for _, want := range []string{
		"raw.githubusercontent.com", "api.github.com",
		"proxy.golang.org", "sum.golang.org",
		"go.dev", "dl.google.com", "storage.googleapis.com",
		"github.com", "objects.githubusercontent.com", "codeload.github.com",
	} {
		assert.Contains(t, got, want, "fallback must cover %q", want)
	}
}

func TestMergeHosts_DedupAndSort(t *testing.T) {
	t.Parallel()

	got := mergeHosts(
		[]string{"b", "a"},
		[]string{"a", "c"},
	)
	assert.Equal(t, []string{"a", "b", "c"}, got)
}
