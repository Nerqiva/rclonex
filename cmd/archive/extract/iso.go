//go:build !plan9

package extract

// ISO9660 images are extracted with go-diskfs over a windowed reader of
// ranged reads, the same random-access strategy the ZIP and 7z formats
// use. The archives library has no ISO support, so the image is detected
// by the "CD001" standard identifier every ISO9660 volume descriptor
// carries at sector 16 + 1 - Joliet and Rock Ridge images are recognised
// too.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"strings"
	"time"

	"github.com/diskfs/go-diskfs/backend"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/sanitize"
)

// isoMagicOffset is the offset of the standard identifier in the primary
// volume descriptor: 16 sectors of 2048 bytes for the system area, plus
// 1 byte into the descriptor.
const isoMagicOffset = int64(0x8001)

var isoMagic = []byte("CD001")

// probeISO reports whether the bytes at isoMagicOffset look like an
// ISO9660 standard identifier. An error reading the bytes is treated as
// "not an ISO" so the caller can fall back to its own detection.
func probeISO(rs io.ReaderAt, size int64) bool {
	if size >= 0 && size < isoMagicOffset+int64(len(isoMagic)) {
		return false
	}
	buf := make([]byte, len(isoMagic))
	_, err := rs.ReadAt(buf, isoMagicOffset)
	return err == nil && bytes.Equal(buf, isoMagic)
}

// recoverParsePanic converts a panic raised while parsing an ISO9660
// image into an error assigned through err. The go-diskfs parser reads
// lazily, so an image which opens cleanly can still panic on a later
// read.
func recoverParsePanic(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("invalid or corrupt ISO9660 image: %v", r)
	}
}

// recoveringReadCloser wraps a reader returned by the go-diskfs parser so
// panics raised while reading become errors.
type recoveringReadCloser struct {
	io.ReadCloser
}

func (r recoveringReadCloser) Read(p []byte) (n int, err error) {
	defer recoverParsePanic(&err)
	return r.ReadCloser.Read(p)
}

var errNotImplemented = errors.New("internal error: iso storage method not implemented")

// isoStorage exposes a windowedReader as a backend.Storage so go-diskfs
// can read the image. Read only - the write paths report errors.
type isoStorage struct {
	r    *windowedReader
	size int64
}

// Read reads bytes from the current position in the image.
func (s *isoStorage) Read(p []byte) (int, error) { return s.r.Read(p) }

// ReadAt reads bytes at an absolute offset in the image.
func (s *isoStorage) ReadAt(p []byte, off int64) (int, error) { return s.r.ReadAt(p, off) }

// Seek sets the offset for the next Read.
func (s *isoStorage) Seek(offset int64, whence int) (int64, error) {
	return s.r.Seek(offset, whence)
}

// Close does nothing - the windowed reader holds no open handle on the
// underlying object.
func (s *isoStorage) Close() error { return nil }

// Stat returns a read-only FileInfo for the image.
func (s *isoStorage) Stat() (iofs.FileInfo, error) { return isoStorageFileInfo{size: s.size}, nil }

// Sys returns an error as there is no underlying OS file for the image.
func (s *isoStorage) Sys() (*os.File, error) { return nil, errNotImplemented }

// Writable returns an error as the image is read only.
func (s *isoStorage) Writable() (backend.WritableFile, error) { return nil, errNotImplemented }

// Path returns an empty string as the image has no local path.
func (s *isoStorage) Path() string { return "" }

// isoStorageFileInfo is a read-only fs.FileInfo for the image.
type isoStorageFileInfo struct {
	size int64
}

var _ backend.Storage = (*isoStorage)(nil)

// Name returns "" as the image has no meaningful name.
func (i isoStorageFileInfo) Name() string { return "" }

// Size returns the size of the image in bytes.
func (i isoStorageFileInfo) Size() int64 { return i.size }

// Mode returns 0o444 - the image is read only.
func (i isoStorageFileInfo) Mode() os.FileMode { return 0o444 }

// ModTime returns the zero time.
func (i isoStorageFileInfo) ModTime() time.Time { return time.Time{} }

// IsDir returns false.
func (i isoStorageFileInfo) IsDir() bool { return false }

// Sys returns nil.
func (i isoStorageFileInfo) Sys() any { return nil }

// extractISO walks the ISO9660 image read from source (an image of size
// bytes) and extracts every file in it, calling handle for each entry.
// Directory entries are passed with isDir set and a name with a trailing
// "/"; file entries are passed an open function streaming the file's
// contents. Rock Ridge symlinks are skipped unless the global --links
// flag is set, in which case they are extracted as .rclonelink files
// holding the link target.
//
// If continueOnError is set then an entry which fails to extract is
// logged and the walk continues; the first error aborts the walk
// otherwise.
func extractISO(ctx context.Context, source *windowedReader, size int64, continueOnError bool, handle func(nameInArchive string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error) (err error) {
	defer recoverParsePanic(&err)
	links := fs.GetConfig(ctx).Links
	fsys, err := iso9660.Read(&isoStorage{r: source, size: size}, size, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to read ISO9660 image: %w", err)
	}
	skipped := 0
	skippedLinks := 0
	// fail stores the error if continueOnError is set, logging it and
	// returning nil so the walk continues; otherwise it returns the error
	fail := func(name string, err error) error {
		if !continueOnError {
			return err
		}
		fs.Errorf(nil, "Failed to extract %q: %v", name, err)
		return nil
	}
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := fsys.ReadDir(dir)
		if err != nil {
			if dir == "." {
				// Nothing can be extracted without the root directory
				return fmt.Errorf("failed to read the root directory of the ISO: %w", err)
			}
			return fail(dir, fmt.Errorf("failed to read directory %q in the ISO: %w", dir, err))
		}
		for _, entry := range entries {
			rel := entry.Name()
			if dir != "." {
				rel = dir + "/" + entry.Name()
			}
			// Skip entry names that aren't safe
			if err := sanitize.Leaf(entry.Name()); err != nil {
				fs.Debugf(nil, "Skipping ISO entry %q which escapes the archive", rel)
				skipped++
				continue
			}
			info, err := entry.Info()
			if err != nil {
				if err := fail(rel, fmt.Errorf("failed to stat ISO entry %q: %w", rel, err)); err != nil {
					return err
				}
				continue
			}
			isDir := entry.IsDir()
			isLink := info.Mode()&os.ModeSymlink != 0
			switch {
			case isDir:
				if err := fail(rel, handle(rel+"/", 0, info.ModTime(), true, nil)); err != nil {
					return err
				}
				if err := walk(rel); err != nil {
					return err
				}
			case isLink && links:
				target, ok := isoLinkTarget(info)
				if !ok {
					fs.Debugf(nil, "Skipping ISO symlink %q - no target recorded", rel)
					skippedLinks++
					continue
				}
				open := func() (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader(target)), nil
				}
				if err := fail(rel, handle(rel+fs.LinkSuffix, int64(len(target)), info.ModTime(), false, open)); err != nil {
					return err
				}
			case isLink:
				skippedLinks++
			case !info.Mode().IsRegular():
				fs.Debugf(nil, "Skipping ISO entry %q - not a regular file", rel)
				skipped++
			default:
				open := func() (io.ReadCloser, error) {
					node, err := fsys.OpenFile(toIOFS(rel), os.O_RDONLY)
					if err != nil {
						return nil, err
					}
					return recoveringReadCloser{node}, nil
				}
				if err := fail(rel, handle(rel, info.Size(), info.ModTime(), false, open)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk("."); err != nil {
		return err
	}
	if skipped > 0 {
		fs.Logf(nil, "Skipped %d entries in the ISO whose names escape the archive or are not regular files", skipped)
	}
	if skippedLinks > 0 {
		fs.Logf(nil, "Skipped %d symlinks in the ISO - use -l/--links to read them", skippedLinks)
	}
	return nil
}

// toIOFS converts an absolute-ish ISO path ("" or "a/b") into the io/fs
// path form go-diskfs expects. The root is ".".
func toIOFS(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

// isoLinkTarget returns the Rock Ridge target of a symlink entry, or ok
// is false if the entry isn't a symlink or has no target. The target is
// the content of the .rclonelink file and is returned verbatim.
func isoLinkTarget(info iofs.FileInfo) (target string, ok bool) {
	st, ok := info.Sys().(*iso9660.StatT)
	if !ok || st.LinkTarget == "" {
		return "", false
	}
	return st.LinkTarget, true
}
