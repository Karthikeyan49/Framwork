//go:build !unix

package tools

import (
	"io/fs"
	"os"
)

// Non-unix platforms have no portable O_NOFOLLOW or owner-only mode bits;
// Lstat plus SameFile still refuses symlinks and non-regular files.
func openSecretFile(path string) (*os.File, error) { return os.Open(path) }

func secretModeAllowed(fs.FileMode) bool { return true }
