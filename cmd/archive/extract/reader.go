//go:build !plan9

package extract

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// errRangeNotSupported is returned when a remote returns the wrong number of
// bytes for a ranged open, e.g. when it ignores the Range header and returns
// the whole file.
var errRangeNotSupported = errors.New("remote does not appear to support range reads")

// rangeFetch opens a ranged ReadCloser yielding exactly want bytes starting
// at absolute offset off.
type rangeFetch func(ctx context.Context, off, want int64) (io.ReadCloser, error)

// window is the currently cached span of a windowedReader.
type window struct {
	off int64
	buf []byte
}

// windowedReader is a sliding-window io.ReaderAt + io.Seeker + io.Reader over
// a byte stream fetched in ranged reads. Formats that need random access
// (ZIP, 7z) fetch their header and footer with a small number of ranged
// opens instead of one open per ReadAt.
type windowedReader struct {
	ctx        context.Context
	fetch      rangeFetch
	windowSize int64
	size       int64
	offset     int64
	win        *window
	fetches    int
	name       string
}

var (
	_ io.Reader   = (*windowedReader)(nil)
	_ io.ReaderAt = (*windowedReader)(nil)
	_ io.Seeker   = (*windowedReader)(nil)
)

// objectFetch returns a rangeFetch for obj that accounts fetched bytes into
// acc, or leaves them unaccounted when acc is nil.
func objectFetch(obj fs.Object, acc *accounting.Transfer, options ...fs.OpenOption) rangeFetch {
	return func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
		ranged := make([]fs.OpenOption, 0, len(options)+1)
		ranged = append(ranged, options...)
		ranged = append(ranged, &fs.RangeOption{Start: off, End: off + want - 1})
		rc, err := obj.Open(ctx, ranged...)
		if err != nil {
			return nil, err
		}
		if acc != nil {
			rc = acc.Account(ctx, rc)
		}
		return rc, nil
	}
}

func newWindowedReader(ctx context.Context, obj fs.Object, windowSize int64, acc *accounting.Transfer, options ...fs.OpenOption) (*windowedReader, error) {
	size := obj.Size()
	if size < 0 {
		return nil, fmt.Errorf("windows can't be used on object %q with unknown size", obj)
	}
	r := newWindowedReaderFromFetch(ctx, size, windowSize, objectFetch(obj, acc, options...))
	r.name = obj.Remote()
	return r, nil
}

func newWindowedReaderFromFetch(ctx context.Context, size, windowSize int64, fetch rangeFetch) *windowedReader {
	if windowSize < 0 {
		windowSize = 0
	}
	return &windowedReader{
		ctx:        ctx,
		fetch:      fetch,
		windowSize: windowSize,
		size:       size,
	}
}

func (w *windowedReader) numFetches() int {
	return w.fetches
}

func (w *windowedReader) remoteError(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if w.name == "" {
		return err
	}
	return fmt.Errorf("remote %q: %w", w.name, err)
}

func (w *windowedReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative offset %d", off)
	}
	total := 0
	for total < len(p) {
		if off >= w.size {
			return total, io.EOF
		}
		if w.windowSize == 0 {
			n, err := w.fetchSpan(p[total:], off)
			total += n
			off += int64(n)
			if err != nil {
				return total, err
			}
			continue
		}
		if !w.inWindow(off) {
			want := w.windowSize
			if want > w.size-off {
				want = w.size - off
			}
			if err := w.refill(off, want); err != nil {
				return total, err
			}
		}
		n := copy(p[total:], w.win.buf[off-w.win.off:])
		total += n
		off += int64(n)
	}
	return total, nil
}

func (w *windowedReader) Read(p []byte) (int, error) {
	n, err := w.ReadAt(p, w.offset)
	w.offset += int64(n)
	if n > 0 && errors.Is(err, io.EOF) {
		return n, nil
	}
	return n, err
}

func (w *windowedReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = w.offset + offset
	case io.SeekEnd:
		abs = w.size + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative position %d", abs)
	}
	w.offset = abs
	return abs, nil
}

func (w *windowedReader) fetchSpan(p []byte, off int64) (int, error) {
	want := int64(len(p))
	if remaining := w.size - off; remaining < want {
		want = remaining
	}
	rangeEnd := off + want - 1
	rc, err := w.fetch(w.ctx, off, want)
	if err != nil {
		return 0, w.remoteError("failed to open range %d-%d: %w", off, rangeEnd, err)
	}
	n, readErr := io.ReadFull(rc, p[:want])
	var extra [1]byte
	extraN, _ := rc.Read(extra[:])
	closeErr := rc.Close()
	w.fetches++
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return n, w.remoteError("failed to read range %d-%d: %w", off, rangeEnd, readErr)
	}
	if int64(n) != want || extraN > 0 {
		return n, w.remoteError("remote does not appear to support range reads (requested %d bytes at offset %d): %w", want, off, errRangeNotSupported)
	}
	if closeErr != nil {
		return n, w.remoteError("failed to close range %d-%d: %w", off, rangeEnd, closeErr)
	}
	return int(want), nil
}

func (w *windowedReader) inWindow(off int64) bool {
	return w.win != nil && off >= w.win.off && off < w.win.off+int64(len(w.win.buf))
}

func (w *windowedReader) refill(off, want int64) error {
	end := off + want - 1
	rc, err := w.fetch(w.ctx, off, want)
	if err != nil {
		return w.remoteError("failed to open range %d-%d: %w", off, end, err)
	}
	got := make([]byte, want+1)
	n, readErr := io.ReadFull(rc, got)
	closeErr := rc.Close()
	w.fetches++
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return w.remoteError("failed to read range %d-%d: %w", off, end, readErr)
	}
	if int64(n) != want {
		return w.remoteError("remote does not appear to support range reads (requested %d bytes at offset %d): %w", want, off, errRangeNotSupported)
	}
	if closeErr != nil {
		return w.remoteError("failed to close range %d-%d: %w", off, end, closeErr)
	}
	w.win = &window{off: off, buf: got[:want]}
	return nil
}

type countingReadCloser struct {
	io.ReadCloser
	n int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n += int64(n)
	return n, err
}
