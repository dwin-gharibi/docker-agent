//go:build unix

package database

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// lockFileExclusive attempts to acquire an exclusive advisory lock without
// blocking. The retry loop in FileLock.Lock handles waiting and cancellation.
func lockFileExclusive(f *os.File) error {
	lock := unix.Flock_t{
		Type:   unix.F_WRLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	return unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lock)
}

func unlockFile(f *os.File) error {
	lock := unix.Flock_t{
		Type:   unix.F_UNLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	return unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lock)
}

func isLockUnavailable(err error) bool {
	return errors.Is(err, unix.EACCES) || errors.Is(err, unix.EAGAIN)
}
