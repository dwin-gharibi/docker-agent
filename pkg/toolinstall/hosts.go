package toolinstall

import (
	"context"
	"slices"
	"sort"
)

// Package-host sets used by the auto-installer at runtime.
//
// We split them by what the install code actually does so callers
// (notably the docker-agent sandbox) can open the smallest possible
// hole in a default-deny network policy:
//
//   - lookupHosts: the registry index + per-package YAML
//     (raw.githubusercontent.com), and the GitHub API used for
//     latest-version resolution and as auth boost for raw fetches.
//     Every install path consults at least these.
//
//   - goInstallHosts: what `go install` reaches for. proxy.golang.org
//     and sum.golang.org are the module proxy + checksum DB. The Go
//     toolchain bootstrap (GOTOOLCHAIN=auto) consults go.dev/dl,
//     downloads from dl.google.com, which redirects to
//     storage.googleapis.com — all three need to be reachable when a
//     module pins a newer Go than the sandbox image ships
//     (e.g. gopls@v0.21.0 needs Go 1.24 on a 1.23 image).
//
//   - githubReleaseHosts: github.com (release page), the asset host
//     that releases redirect to (objects.githubusercontent.com), and
//     codeload (used for source tarballs by some package types).
//
// Hosts must be bare (no scheme, no port, no path) so they pass
// through Backend.AllowHosts unchanged.
var (
	lookupHosts = []string{
		"raw.githubusercontent.com",
		"api.github.com",
	}

	goInstallHosts = []string{
		"proxy.golang.org",
		"sum.golang.org",
		"go.dev",
		"dl.google.com",
		"storage.googleapis.com",
	}

	githubReleaseHosts = []string{
		"github.com",
		"objects.githubusercontent.com",
		"codeload.github.com",
	}
)

// FallbackHosts returns the union of every host any auto-install path
// might reach. Callers use it when they decide to allow a tool to
// install but cannot resolve the specific package on the host (e.g.
// the registry is unreachable and there's no disk cache yet). It
// preserves install behaviour at the cost of giving up the per-package
// narrowing — security-conscious callers should prefer reporting the
// resolution error and refusing the run instead.
func FallbackHosts() []string {
	return mergeHosts(lookupHosts, goInstallHosts, githubReleaseHosts)
}

// InstallHosts returns the hostnames the auto-installer will reach
// for when installing this package. Always includes the registry-side
// lookup hosts because the in-sandbox installer re-runs the same
// LookupByName/LookupByCommand path the host uses here.
func (p *Package) InstallHosts() []string {
	if p.IsGoPackage() {
		return mergeHosts(lookupHosts, goInstallHosts)
	}
	return mergeHosts(lookupHosts, githubReleaseHosts)
}

// ResolveHosts returns the set of hostnames the auto-installer needs
// to reach inside a sandbox in order to install command at version.
//
// version follows the [EnsureCommand] convention:
//   - "" → resolve by command name; latest version.
//   - "owner/repo" → look up by aqua name; latest version.
//   - "owner/repo@version" → look up by aqua name; explicit version.
//
// The returned list is sorted and deduplicated. Errors propagate the
// underlying registry failure verbatim so the caller can log it and
// decide whether to fail the run or fall back to [FallbackHosts].
func ResolveHosts(ctx context.Context, command, version string) ([]string, error) {
	pkg, _, err := lookupPackage(ctx, SharedRegistry(), command, version)
	if err != nil {
		return nil, err
	}
	return pkg.InstallHosts(), nil
}

// mergeHosts returns the sorted, deduplicated union of its arguments.
func mergeHosts(sets ...[]string) []string {
	var out []string
	for _, s := range sets {
		out = append(out, s...)
	}
	sort.Strings(out)
	return slices.Compact(out)
}
