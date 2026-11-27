//go:build !plan9

package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// sevenZipVolume is one volume of a multi-volume 7z archive. base is the
// cumulative offset of the volume within the concatenated stream.
type sevenZipVolume struct {
	obj  fs.Object
	size int64
	base int64
}

type archiveVolumeScheme struct {
	base string
	kind string
}

func detectArchiveVolumes(srcFile string) (archiveVolumeScheme, bool) {
	lower := strings.ToLower(srcFile)
	if strings.HasSuffix(lower, ".001") {
		return archiveVolumeScheme{base: srcFile[:len(srcFile)-len(".001")], kind: "7z"}, true
	}
	if !strings.HasSuffix(lower, ".rar") {
		return archiveVolumeScheme{}, false
	}
	base := srcFile[:len(srcFile)-len(".rar")]
	stem := path.Base(base)
	if i := strings.LastIndex(strings.ToLower(stem), ".part"); i >= 0 {
		if _, err := strconv.Atoi(stem[i+len(".part"):]); err == nil {
			return archiveVolumeScheme{base: path.Join(path.Dir(srcFile), stem[:i]), kind: "rar-part"}, true
		}
	}
	return archiveVolumeScheme{base: base, kind: "rar-old"}, true
}

func (v archiveVolumeScheme) name(i int) string {
	switch v.kind {
	case "7z":
		return fmt.Sprintf("%s.%03d", v.base, i)
	case "rar-part":
		return fmt.Sprintf("%s.part%d.rar", v.base, i)
	default:
		if i == 1 {
			return v.base + ".rar"
		}
		return fmt.Sprintf("%s.r%02d", v.base, i-2)
	}
}

// collectArchiveVolumes collects the volumes comprising the archive whose
// first volume is srcFile, in order. Supported names are <base>.NNN,
// <base>.partN.rar and <base>.rar with <base>.rNN companions. All volumes
// must report a known size. The list and total concatenated size are returned.
func collectArchiveVolumes(ctx context.Context, src fs.Fs, srcFile string) ([]sevenZipVolume, int64, error) {
	scheme, ok := detectArchiveVolumes(srcFile)
	if !ok {
		return nil, 0, fmt.Errorf("unrecognised multi-volume archive name %q", srcFile)
	}
	first, err := src.NewObject(ctx, scheme.name(1))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to access first volume %q: %w", srcFile, err)
	}
	firstSize := first.Size()
	if firstSize < 0 {
		return nil, 0, fmt.Errorf("volume %q has unknown size", srcFile)
	}
	volumes := []sevenZipVolume{{obj: first, size: firstSize, base: 0}}
	total := firstSize
	for i := 2; ; i++ {
		if i > 9999 {
			return nil, 0, fmt.Errorf("volume set for %q grows past 9999 volumes", srcFile)
		}
		remote := scheme.name(i)
		obj, err := src.NewObject(ctx, remote)
		if errors.Is(err, fs.ErrorObjectNotFound) {
			if gapErr := archiveVolumeGap(ctx, src, scheme, remote, i); gapErr != nil {
				return nil, 0, gapErr
			}
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to stat volume %q: %w", remote, err)
		}
		size := obj.Size()
		if size < 0 {
			return nil, 0, fmt.Errorf("volume %q has unknown size", remote)
		}
		volumes = append(volumes, sevenZipVolume{obj: obj, size: size, base: total})
		total += size
	}
	return volumes, total, nil
}

// archiveVolumeGap turns a missing volume in the middle of a set into a
// named error. A later volume must be found by listing the directory before
// the gap can be told apart from the end of the set.
func archiveVolumeGap(ctx context.Context, src fs.Fs, scheme archiveVolumeScheme, missing string, next int) error {
	dir := path.Dir(scheme.base)
	if dir == "." {
		dir = ""
	}
	entries, err := src.List(ctx, dir)
	if err != nil {
		return nil
	}
	wanted := make(map[string]bool)
	for i := next + 1; i <= 9999; i++ {
		wanted[path.Base(scheme.name(i))] = true
	}
	for _, entry := range entries {
		obj, ok := entry.(fs.Object)
		if !ok {
			continue
		}
		if wanted[path.Base(obj.Remote())] {
			return fmt.Errorf("volume set for %q is missing volume %q (found %q)", scheme.base, missing, obj.Remote())
		}
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
