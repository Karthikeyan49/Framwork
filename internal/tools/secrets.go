package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxSecretBytes bounds one operator-provisioned credential file.
const MaxSecretBytes = 4096

// ErrSecretUnavailable is the only error class FileSecrets returns. Errors
// carry a fixed reason, never a path, value or underlying OS error text.
var ErrSecretUnavailable = errors.New("tool secret unavailable")

type secretError struct{ reason string }

func (e secretError) Error() string        { return ErrSecretUnavailable.Error() + ": " + e.reason }
func (e secretError) Is(target error) bool { return target == ErrSecretUnavailable }

// FileSecrets resolves a secret_ref to the contents of Dir/<ref>. The file
// must be a regular file (never a symlink), readable only by its owner
// (0600 or stricter on unix) and at most MaxSecretBytes. A single trailing
// newline is trimmed. Values are read at send time and never cached.
type FileSecrets struct {
	Dir string
}

func (f FileSecrets) Resolve(ctx context.Context, ref string) (string, error) {
	if ctx.Err() != nil {
		return "", secretError{"canceled"}
	}
	if !secretPattern.MatchString(ref) || f.Dir == "" {
		return "", secretError{"invalid reference"}
	}
	path := filepath.Join(f.Dir, ref)
	info, err := os.Lstat(path)
	if err != nil {
		return "", secretError{"not found"}
	}
	if err := checkSecretInfo(info); err != nil {
		return "", err
	}
	file, err := openSecretFile(path)
	if err != nil {
		return "", secretError{"not readable"}
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", secretError{"changed while opening"}
	}
	if err := checkSecretInfo(opened); err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxSecretBytes+1))
	if err != nil {
		return "", secretError{"not readable"}
	}
	if len(raw) > MaxSecretBytes {
		return "", secretError{"too large"}
	}
	value := strings.TrimSuffix(string(raw), "\n")
	if !validSecretValue(value) {
		return "", secretError{"invalid value"}
	}
	return value, nil
}

func checkSecretInfo(info os.FileInfo) error {
	switch {
	case !info.Mode().IsRegular():
		return secretError{"not a regular file"}
	case !secretModeAllowed(info.Mode()):
		return secretError{"permissions allow group or other access"}
	case info.Size() > MaxSecretBytes:
		return secretError{"too large"}
	}
	return nil
}

// validSecretValue admits visible ASCII only, so a credential can never
// inject header lines or whitespace into the bearer header.
func validSecretValue(v string) bool {
	if len(v) < 1 || len(v) > MaxSecretBytes {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x21 || v[i] > 0x7e {
			return false
		}
	}
	return true
}
