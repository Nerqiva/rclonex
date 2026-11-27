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
	"strconv"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// eofNormalizeReader adapts a stream so io.EOF never accompanies data: a
// remote whose reader returns (n > 0, io.EOF) - legal, but rardecode's
// volume buffer keeps the stale io.EOF across volumes when this happens,
// failing the next volume with "RAR signature not found". EOF is reported
// only on the following zero-length read.
type eofNormalizeReader struct {
	rc  io.ReadCloser
	eof bool
}

func (r *eofNormalizeReader) Read(p []byte) (int, error) {
	if r.eof {
		return 0, io.EOF
	}
	n, err := r.rc.Read(p)
	if n > 0 && err == io.EOF {
		r.eof = true
		return n, nil
	}
	if err == io.EOF {
		r.eof = true
	}
	return n, err
}

func (r *eofNormalizeReader) Close() error { return r.rc.Close() }

// volumeReadVerifier reports a volume whose remote response ends before the
// size the object declared. rardecode treats a clean EOF as the end of the
// volume set, so a truncated response otherwise surfaces as an unrelated
// decoder error on whichever file happened to span the missing bytes.
type volumeReadVerifier struct {
	rc   io.ReadCloser
	name string
	size int64
	read int64
}

func (v *volumeReadVerifier) Read(p []byte) (int, error) {
	n, err := v.rc.Read(p)
	v.read += int64(n)
	if err == io.EOF && v.read < v.size {
		return n, fmt.Errorf("volume %q: remote returned %d bytes, expected %d", v.name, v.read, v.size)
	}
	return n, err
}

func (v *volumeReadVerifier) Close() error { return v.rc.Close() }

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
	rc = &eofNormalizeReader{rc: rc}
	size := obj.Size()
	if size >= 0 {
		rc = &volumeReadVerifier{rc: rc, name: clean, size: size}
	}
	if r.acc != nil {
		rc = r.acc.Account(r.ctx, rc)
	}
	return &remoteFile{ctx: r.ctx, name: path.Base(clean), obj: obj, rc: rc, size: size}, nil
}

type remoteFile struct {
	ctx  context.Context
	name string
	obj  fs.Object
	rc   io.ReadCloser
	size int64
}

func (f *remoteFile) Read(p []byte) (int, error) {
	return f.rc.Read(p)
}

func (f *remoteFile) Close() error {
	return f.rc.Close()
}

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

type remoteFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (i remoteFileInfo) Name() string { return i.name }

func (i remoteFileInfo) Size() int64 { return i.size }

func (i remoteFileInfo) Mode() iofs.FileMode { return 0o444 }

func (i remoteFileInfo) ModTime() time.Time { return i.modTime }

func (i remoteFileInfo) IsDir() bool { return false }

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
			if gapErr := sevenZipVolumeGap(ctx, src, baseName, remote, i); gapErr != nil {
				return nil, 0, gapErr
			}
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

// sevenZipVolumeGap turns a missing volume in the middle of a set into a
// named error. Sequential probing stops at the first absent name, so a later
// volume must be found by listing the directory before the gap can be told
// apart from the end of the set. A failed or unsupported list leaves the
// original end-of-set behaviour intact.
func sevenZipVolumeGap(ctx context.Context, src fs.Fs, baseName, missing string, next int) error {
	dir := path.Dir(baseName)
	if dir == "." {
		dir = ""
	}
	entries, err := src.List(ctx, dir)
	if err != nil {
		return nil
	}
	prefix := path.Base(baseName) + "."
	for _, entry := range entries {
		obj, ok := entry.(fs.Object)
		if !ok {
			continue
		}
		name := path.Base(obj.Remote())
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if err != nil || n <= next {
			continue
		}
		return fmt.Errorf("7z volume set for %q is missing volume %q (found %q)", baseName, missing, obj.Remote())
	}
	return nil
}

func concatFetch(ctx context.Context, volumes []sevenZipVolume, acc *accounting.Transfer, options ...fs.OpenOption) rangeFetch {
	state := &concatState{
		ctx:     ctx,
		volumes: volumes,
		acc:     acc,
		options: options,
		readers: make([]*windowedReader, len(volumes)),
	}
	return func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
		if off < 0 || want < 0 {
			return nil, fmt.Errorf("invalid fetch offset %d, want %d", off, want)
		}
		return &concatVolumesReader{state: state, pos: off, end: off + want}, nil
	}
}

type concatState struct {
	ctx     context.Context
	volumes []sevenZipVolume
	acc     *accounting.Transfer
	options []fs.OpenOption
	readers []*windowedReader
}

func (s *concatState) readerFor(i int) (*windowedReader, error) {
	if s.readers[i] == nil {
		r, err := newWindowedReader(s.ctx, s.volumes[i].obj, 0, s.acc, s.options...)
		if err != nil {
			return nil, err
		}
		r.name = s.volumes[i].obj.Remote()
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
	if err == nil && n > 0 {
		return n, nil
	}
	if err == nil {
		err = io.EOF
	}
	return n, err
}

// Close closes the reader; the per-volume readers hold no open handles.
func (r *concatVolumesReader) Close() error {
	return nil
}
