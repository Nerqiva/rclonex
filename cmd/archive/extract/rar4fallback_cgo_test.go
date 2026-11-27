//go:build cgo && unarr && !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rclone/rclone/fs/accounting"
)

func memoryFetch(data []byte) func(context.Context, int64, int64) (io.ReadCloser, error) {
	return func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if off < 0 || want < 0 || off+want > int64(len(data)) {
			return nil, io.ErrUnexpectedEOF
		}
		return io.NopCloser(bytes.NewReader(data[off : off+want])), nil
	}
}

func TestRAR4StreamAdapter(t *testing.T) {
	data := []byte("0123456789abcdef")
	w := newWindowedReaderFromFetch(context.Background(), int64(len(data)), 8, memoryFetch(data))
	s := &rar4Stream{r: w}
	p := make([]byte, 5)
	if n, err := s.read(p); err != nil || n != 5 || string(p) != "01234" {
		t.Fatalf("read: n=%d p=%q err=%v", n, p, err)
	}
	if pos, err := s.seek(2, io.SeekStart); err != nil || pos != 2 {
		t.Fatalf("seek: pos=%d err=%v", pos, err)
	}
	if pos := s.tell(); pos != 2 {
		t.Fatalf("tell: %d", pos)
	}
	if n, err := s.read(p); err != nil || n != 5 || string(p[:n]) != "23456" {
		t.Fatalf("ranged read: n=%d p=%q err=%v", n, p, err)
	}

	wantErr := errors.New("remote unavailable")
	w = newWindowedReaderFromFetch(context.Background(), int64(len(data)), 8,
		func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
			return nil, wantErr
		})
	s = &rar4Stream{r: w}
	if _, err := s.read(make([]byte, 4)); err == nil {
		t.Fatal("expected remote error from read")
	}
	if !errors.Is(s.check(), wantErr) {
		t.Fatalf("stream error lost: %v", s.check())
	}
}

func TestRAR4FallbackFromFetch(t *testing.T) {
	data, err := os.ReadFile("testdata/rar4.rar")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	handler := entryHandler(func(name string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error {
		rc, err := open()
		if err != nil {
			return err
		}
		defer rc.Close()
		got, err := io.ReadAll(rc)
		if err != nil {
			return err
		}
		if int64(len(got)) != size {
			return fmt.Errorf("%s: read %d bytes, expected %d", name, len(got), size)
		}
		names = append(names, name)
		return nil
	})
	if err := (rar4Fetch{size: int64(len(data)), windowSize: 16, fetch: memoryFetch(data)}).extract(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no entries extracted")
	}
}

func TestRAR4FallbackDiscardsPrimaryStatsError(t *testing.T) {
	data, err := os.ReadFile("testdata/rar4.rar")
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt one byte of the file header. The built-in decoder rejects the
	// archive while unarr still extracts the entry.
	data[0x2e] ^= 0x80

	ctx := context.Background()
	stats := accounting.Stats(ctx)
	stats.ResetErrors()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	writeMemObject(t, ctx, src, "bad.rar", data)
	stats.Error(errors.New("primary decode failure"))

	if err := ArchiveExtract(ctx, dst, "", src, "bad.rar", DefaultExtractOptions()); err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if stats.Errored() || stats.GetLastError() != nil {
		t.Fatalf("discarded primary error survived: %v", stats.GetLastError())
	}
}

func TestRAR4FallbackCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data, err := os.ReadFile("testdata/rar4.rar")
	if err != nil {
		t.Fatal(err)
	}
	handler := entryHandler(func(name string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error {
		return nil
	})
	err = rar4Fetch{size: int64(len(data)), windowSize: 16, fetch: memoryFetch(data)}.extract(ctx, handler)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
