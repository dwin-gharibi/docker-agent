//go:build !windows

package board

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwner verifies that the board's socket directory belongs to the
// current user, so the board never binds its sockets inside a directory
// another local user pre-created.
func checkOwner(dir string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("board socket dir %s is not owned by the current user", dir)
	}
	return nil
}
