//go:build js && wasm

package userconfig

import "os"

// flockExclusive is a no-op under js/wasm. The runtime is single-threaded and
// there is no second process to coordinate with.
func flockExclusive(_ *os.File) error {
	return nil
}

// flockRelease mirrors flockExclusive: there is nothing to release.
func flockRelease(_ *os.File) {
}
