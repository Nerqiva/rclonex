package extract

import (
	"context"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// entryHandler receives one entry from the engine. The open function must
// return a fresh reader for the entry's contents on each call.
type entryHandler func(nameInArchive string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error

// Lib7zipAvailable reports whether this build can extract archives with the
// bundled 7-Zip engine.
func Lib7zipAvailable() bool { return lib7zipAvailable() }

// lib7zipDecodeError marks a failure produced by the 7-Zip decoder rather
// than by remote or destination I/O.
type lib7zipDecodeError struct {
	err error
}

func (e *lib7zipDecodeError) Error() string { return e.err.Error() }

func (e *lib7zipDecodeError) Unwrap() error { return e.err }

// lib7zipArgs carries everything the 7-Zip engine needs to stream an archive
// from a remote object into the destination.
type lib7zipArgs struct {
	ctx        context.Context
	src        fs.Fs
	srcObj     fs.Object
	srcFile    string
	windowSize int64
	tr         *accounting.Transfer
	password   string
	options    []fs.OpenOption
	nested     bool
	handle     entryHandler
}

// lib7zipSupportsPassword reports whether password extraction is allowed for
// this archive name. Rclone only accepts passwords for RAR and 7z archives.
func lib7zipSupportsPassword(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".7z") || strings.HasSuffix(lower, ".7z.001") {
		return true
	}
	if !strings.HasSuffix(lower, ".rar") {
		return false
	}
	base := strings.TrimSuffix(path.Base(lower), ".rar")
	for _, part := range strings.Split(base, ".") {
		if strings.HasPrefix(part, "part") && len(part) > 4 {
			if _, err := strconv.Atoi(part[4:]); err == nil {
				return true
			}
		}
		if len(part) == 2 && part[0] == 'r' && part[1] >= '0' && part[1] <= '9' {
			return true
		}
	}
	return true
}
