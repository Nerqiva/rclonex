//go:build !plan9

package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// remoteFS exposes an rclone fs.Fs as an io/fs.FS so rardecode can open
// multi-volume archive volumes by name. Names arrive as
// path.Dir(srcFile)+"/"+volume (e.g. "sub/dir/fixture.part2.rar" when the
// source is "sub/dir/fixture.part1.rar") and are opened on the remote -
// volumes are streamed, never staged locally. Only Open is implemented;
// nothing else of io/fs.FS is used by rardecode.
type remoteFS struct {
	ctx context.Context
	f   fs.Fs
	acc *accounting.Transfer
}

// Open opens the named volume on the remote. A missing volume is reported
// as io/fs.ErrNotExist so rardecode can detect the end of the volume set;
// any name escaping the remote root is rejected.
func (r remoteFS) Open(name string) (iofs.File, error) {
	clean := path.Clean(name)
	switch {
	case clean == ".", strings.HasPrefix(clean, "/"), clean == "..", strings.HasPrefix(clean, "../"):
		return nil, &iofs.PathError{Op: "open", Path: name, Err: iofs.ErrInvalid}
	}
	obj, err := r.f.NewObject(r.ctx, clean)
	if err != nil {
		if errors.Is(err, fs.ErrorObjectNotFound) {
			return nil, &iofs.PathError{Op: "open", Path: name, Err: iofs.ErrNotExist}
		}
		return nil, &iofs.PathError{Op: "open", Path: name, Err: err}
	}
	rc, err := obj.Open(r.ctx)
	if err != nil {
		return nil, &iofs.PathError{Op: "open", Path: name, Err: err}
	}
	if r.acc != nil {
		rc = r.acc.Account(r.ctx, rc)
	}
	size := obj.Size()
	return &remoteFile{ctx: r.ctx, name: path.Base(clean), obj: obj, rc: rc, size: size}, nil
}

// remoteFile is an io/fs.File backed by a remote rclone object. The bytes
// read through Read are counted into the archive transfer.
type remoteFile struct {
	ctx  context.Context
	name string
	obj  fs.Object
	rc   io.ReadCloser
	size int64
}

// Read reads bytes from the volume.
func (f *remoteFile) Read(p []byte) (int, error) {
	return f.rc.Read(p)
}

// Close closes the volume.
func (f *remoteFile) Close() error {
	return f.rc.Close()
}

// Stat returns the volume's file info.
func (f *remoteFile) Stat() (iofs.FileInfo, error) {
	if f.size < 0 {
		return nil, fmt.Errorf("volume %q has unknown size", f.name)
	}
	return remoteFileInfo{
		name:    f.name,
		size:    f.size,
		modTime: f.obj.ModTime(f.ctx),
	}, nil
}

// remoteFileInfo is a read-only io/fs.FileInfo for an archive volume.
type remoteFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

// Name returns the base name of the volume.
func (i remoteFileInfo) Name() string { return i.name }

// Size returns the size of the volume in bytes.
func (i remoteFileInfo) Size() int64 { return i.size }

// Mode returns the volume's mode (read-only).
func (i remoteFileInfo) Mode() iofs.FileMode { return 0o444 }

// ModTime returns the volume's modification time.
func (i remoteFileInfo) ModTime() time.Time { return i.modTime }

// IsDir returns false - a volume is not a directory.
func (i remoteFileInfo) IsDir() bool { return false }

// Sys returns nil.
func (i remoteFileInfo) Sys() any { return nil }

// sevenZipVolume is one volume of a multi-volume 7z archive. base is the
// cumulative offset of the volume within the concatenated stream.
type sevenZipVolume struct {
	obj  fs.Object
	size int64
	base int64
}

// collectSevenZipVolumes collects the volumes comprising the 7z archive
// whose first volume is srcFile (e.g. "sub/mv.7z.001"), in order. The base
// name is derived by trimming the trailing ".001"; subsequent volumes are
// looked up as "<base>.<nnn>" until a volume is missing. All volumes must
// report a known size. A .001 file with no companions yields a
// single-volume list. The list and the total size of the concatenated
// stream are returned.
func collectSevenZipVolumes(ctx context.Context, src fs.Fs, srcFile string) ([]sevenZipVolume, int64, error) {
	if !strings.HasSuffix(strings.ToLower(srcFile), ".001") {
		return nil, 0, fmt.Errorf("7z volume sets must start with a .001 file, not %q", srcFile)
	}
	baseName := srcFile[:len(srcFile)-len(".001")]
	first, err := src.NewObject(ctx, srcFile)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to access first 7z volume %q: %w", srcFile, err)
	}
	firstSize := first.Size()
	if firstSize < 0 {
		return nil, 0, fmt.Errorf("7z volume %q has unknown size", srcFile)
	}
	volumes := []sevenZipVolume{{obj: first, size: firstSize, base: 0}}
	total := firstSize
	for i := 2; ; i++ {
		if i > 9999 {
			return nil, 0, fmt.Errorf("7z volume set for %q grows past 9999 volumes", srcFile)
		}
		remote := fmt.Sprintf("%s.%03d", baseName, i)
		obj, err := src.NewObject(ctx, remote)
		if errors.Is(err, fs.ErrorObjectNotFound) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to stat 7z volume %q: %w", remote, err)
		}
		size := obj.Size()
		if size < 0 {
			return nil, 0, fmt.Errorf("7z volume %q has unknown size", remote)
		}
		volumes = append(volumes, sevenZipVolume{obj: obj, size: size, base: total})
		total += size
	}
	return volumes, total, nil
}

// concatFetch returns the fetch closure for newWindowedReaderFromFetch
// serving the concatenation of volumes as one stream. fetch(off, want)
// returns a ReadCloser yielding exactly want bytes; the reads map onto
// per-volume windowedReaders constructed lazily with the same acc and
// options as the outer reader.
//
// The volume readers account the fetched bytes into acc; the outer reader
// must be constructed without an accounting transfer or the bytes would be
// counted twice. The volume sizes are known from collectSevenZipVolumes, so
// a volume shrinking between Stat and Open surfaces as a short read and is
// reported by the outer refill's exact-byte-count verification.
func concatFetch(ctx context.Context, volumes []sevenZipVolume, windowSize int64, acc *accounting.Transfer, options ...fs.OpenOption) func(context.Context, int64, int64) (io.ReadCloser, error) {
	state := &concatState{
		ctx:        ctx,
		volumes:    volumes,
		windowSize: windowSize,
		acc:        acc,
		options:    options,
		readers:    make([]*windowedReader, len(volumes)),
	}
	return func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
		if off < 0 || want < 0 {
			return nil, fmt.Errorf("invalid fetch offset %d, want %d", off, want)
		}
		return &concatVolumesReader{state: state, pos: off, end: off + want}, nil
	}
}

// concatState holds the volumes and the lazily constructed per-volume
// windowed readers shared by all concatVolumesReaders.
type concatState struct {
	ctx        context.Context
	volumes    []sevenZipVolume
	windowSize int64
	acc        *accounting.Transfer
	options    []fs.OpenOption
	readers    []*windowedReader
}

// readerFor returns the windowed reader for volume i, constructing it on
// first use.
func (s *concatState) readerFor(i int) (*windowedReader, error) {
	if s.readers[i] == nil {
		r, err := newWindowedReader(s.ctx, s.volumes[i].obj, s.windowSize, s.acc, s.options...)
		if err != nil {
			return nil, err
		}
		s.readers[i] = r
	}
	return s.readers[i], nil
}

// concatVolumesReader yields exactly the bytes in [pos, end) of the
// concatenated volume stream.
type concatVolumesReader struct {
	state *concatState
	pos   int64
	end   int64
}

// Read reads bytes from the concatenated stream.
func (r *concatVolumesReader) Read(p []byte) (int, error) {
	if r.pos >= r.end {
		return 0, io.EOF
	}
	if remaining := r.end - r.pos; int64(len(p)) > remaining {
		p = p[:remaining]
	}
	i := sort.Search(len(r.state.volumes), func(i int) bool {
		return r.state.volumes[i].base+r.state.volumes[i].size > r.pos
	})
	if i >= len(r.state.volumes) {
		return 0, io.EOF
	}
	v := r.state.volumes[i]
	local := r.pos - v.base
	if local+int64(len(p)) > v.size {
		p = p[:v.size-local]
	}
	wr, err := r.state.readerFor(i)
	if err != nil {
		return 0, err
	}
	n, err := wr.ReadAt(p, local)
	r.pos += int64(n)
	if n > 0 {
		return n, nil
	}
	if err == nil {
		err = io.EOF
	}
	return 0, err
}

// Close closes the reader; the per-volume readers hold no open handles.
func (r *concatVolumesReader) Close() error {
	return nil
}
