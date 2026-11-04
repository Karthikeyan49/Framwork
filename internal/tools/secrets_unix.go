//go:build unix

package tools

import (
	"io/fs"
	"os"
	"syscall"
)

// openSecretFile refuses a final-component symlink (O_NOFOLLOW) and never
// blocks on a FIFO swapped in after Lstat (O_NONBLOCK); Stat then confirms it
// is the same regular file.
func openSecretFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

func secretModeAllowed(mode fs.FileMode) bool { return mode.Perm()&0o077 == 0 }
