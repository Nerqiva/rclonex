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

// windowedReader is a sliding-window io.ReaderAt + io.Seeker + io.Reader over
// a byte stream fetched in ranged reads. Formats that need random access
// (ZIP, 7z) fetch their header and footer with a small number of ranged
// opens instead of one open per ReadAt.
//
// A window of windowSize bytes starting at the offset of the first uncached
// read is kept in memory; reads falling inside the window are served from
// the buffer. A windowSize of 0 disables caching - every ReadAt performs a
// direct ranged fetch of exactly the requested span.
//
// windowedReader is used from a single goroutine; it is not safe for
// concurrent use.
type windowedReader struct {
	ctx        context.Context
	fetch      func(ctx context.Context, off, want int64) (io.ReadCloser, error)
	windowSize int64
	size       int64
	offset     int64  // current position for Read and Seek
	buf        []byte // contents of the current window
	bufOff     int64  // absolute offset of buf[0], -1 when buf is empty
	acc        *accounting.Transfer
	fetches    int
}

var (
	_ io.Reader   = (*windowedReader)(nil)
	_ io.ReaderAt = (*windowedReader)(nil)
	_ io.Seeker   = (*windowedReader)(nil)
)

// newWindowedReader makes a windowedReader for obj.
//
// The object must know its size - an error is returned if it doesn't as
// the 7z and ZIP formats determine the stream size by seeking to the end.
// acc is the archive transfer to count fetched bytes into, or nil for no
// accounting. No I/O is performed at construction time; the first Read or
// fetch happens on the first ReadAt or Read.
func newWindowedReader(ctx context.Context, obj fs.Object, windowSize int64, acc *accounting.Transfer, options ...fs.OpenOption) (*windowedReader, error) {
	size := obj.Size()
	if size < 0 {
		return nil, fmt.Errorf("windows can't be used on object %q with unknown size", obj)
	}
	fetch := func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
		end := off + want - 1
		ranged := make([]fs.OpenOption, 0, len(options)+1)
		ranged = append(ranged, options...)
		ranged = append(ranged, &fs.RangeOption{Start: off, End: end})
		return obj.Open(ctx, ranged...)
	}
	return newWindowedReaderFromFetch(ctx, size, windowSize, acc, fetch), nil
}

// newWindowedReaderFromFetch makes a windowedReader whose bytes come from
// fetch. fetch must return a ReadCloser yielding exactly want bytes starting
// at absolute offset off; refill verifies the byte count. size is the total
// size of the (possibly virtual) stream.
func newWindowedReaderFromFetch(ctx context.Context, size, windowSize int64, acc *accounting.Transfer, fetch func(ctx context.Context, off, want int64) (io.ReadCloser, error)) *windowedReader {
	if windowSize < 0 {
		windowSize = 0
	}
	return &windowedReader{
		ctx:        ctx,
		fetch:      fetch,
		windowSize: windowSize,
		size:       size,
		bufOff:     -1,
		acc:        acc,
	}
}

// numFetches returns how many ranged opens have been performed. Used in
// tests.
func (w *windowedReader) numFetches() int {
	return w.fetches
}

// ReadAt reads len(p) bytes from the stream at absolute offset off. It
// returns the number of bytes read and an error if the read was short; the
// error is io.EOF when reading past the end of the stream.
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
		n := copy(p[total:], w.buf[off-w.bufOff:])
		if n == 0 {
			// The window should always cover off after a refill
			return total, fmt.Errorf("windowed reader made no progress at offset %d", off)
		}
		total += n
		off += int64(n)
	}
	return total, nil
}

// Read reads len(p) bytes from the current position and advances it.
func (w *windowedReader) Read(p []byte) (int, error) {
	n, err := w.ReadAt(p, w.offset)
	w.offset += int64(n)
	if n > 0 && errors.Is(err, io.EOF) {
		return n, nil
	}
	return n, err
}

// Seek sets the offset for the next Read or Write to offset interpreted
// relative to whence: io.SeekStart, io.SeekCurrent or io.SeekEnd. Seeking
// past the end is allowed; reads at or past the end return io.EOF. Negative
// positions are an error.
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

// fetchSpan reads min(len(p), size-off) bytes starting at off directly
// into p, verifying the remote returns exactly that many bytes - a
// remote which ignores the Range header is reported as
// errRangeNotSupported rather than silently serving the wrong bytes.
// Nothing is retained in w.buf.
func (w *windowedReader) fetchSpan(p []byte, off int64) (int, error) {
	want := int64(len(p))
	if remaining := w.size - off; remaining < want {
		want = remaining
	}
	rangeEnd := off + want - 1
	rc, err := w.fetch(w.ctx, off, want)
	if err != nil {
		return 0, fmt.Errorf("failed to open range %d-%d: %w", off, rangeEnd, err)
	}
	if w.acc != nil {
		rc = w.acc.Account(w.ctx, rc)
	}
	n, readErr := io.ReadFull(rc, p[:want])
	var extra [1]byte
	extraN, _ := rc.Read(extra[:])
	closeErr := rc.Close()
	w.fetches++
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return n, fmt.Errorf("failed to read range %d-%d: %w", off, rangeEnd, readErr)
	}
	if int64(n) != want || extraN > 0 {
		return n, fmt.Errorf("remote does not appear to support range reads (requested %d bytes at offset %d): %w", want, off, errRangeNotSupported)
	}
	if closeErr != nil {
		return n, fmt.Errorf("failed to close range %d-%d: %w", off, rangeEnd, closeErr)
	}
	return int(want), nil
}

// inWindow returns true if off is inside the current window.
func (w *windowedReader) inWindow(off int64) bool {
	return off >= w.bufOff && off < w.bufOff+int64(len(w.buf))
}

// refill fetches a window of want bytes starting at off. The fetch is
// verified to return exactly the requested number of bytes - a remote which
// ignores the Range header returns an explicit error rather than silently
// serving the wrong data.
func (w *windowedReader) refill(off, want int64) error {
	end := off + want - 1
	rc, err := w.fetch(w.ctx, off, want)
	if err != nil {
		return fmt.Errorf("failed to open range %d-%d: %w", off, end, err)
	}
	if w.acc != nil {
		rc = w.acc.Account(w.ctx, rc)
	}
	// Preallocate the read buffer - ReadFull spares the growth
	// transient an io.ReadAll of the same span would incur
	got := make([]byte, want+1)
	n, readErr := io.ReadFull(rc, got)
	closeErr := rc.Close()
	w.fetches++
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return fmt.Errorf("failed to read range %d-%d: %w", off, end, readErr)
	}
	if int64(n) != want {
		return fmt.Errorf("remote does not appear to support range reads (requested %d bytes at offset %d): %w", want, off, errRangeNotSupported)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close range %d-%d: %w", off, end, closeErr)
	}
	w.buf = got[:want]
	w.bufOff = off
	return nil
}

// countingReadCloser counts the bytes read through it. It is used to check
// the number of bytes extracted against the size the archive claims for the
// entry.
type countingReadCloser struct {
	io.ReadCloser
	n int64
}

// Read implements io.Reader.
func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n += int64(n)
	return n, err
}
