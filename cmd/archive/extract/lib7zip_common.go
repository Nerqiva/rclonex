package extract

import (
	"context"
	"errors"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// forceLegacyExtractor lets parity tests run the previous extractor in a
// process where the lib7zip engine is available.
var forceLegacyExtractor bool

// errLib7zipUnavailable reports that the 7-Zip engine cannot run in this
// environment, so the previous extraction path must be used.
var errLib7zipUnavailable = errors.New("7-Zip engine unavailable")

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
	handle     entryHandler
}

// lib7zipHandles reports whether the 7-Zip engine owns this archive name.
func lib7zipHandles(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".7z") || strings.HasSuffix(lower, ".7z.001")
}
