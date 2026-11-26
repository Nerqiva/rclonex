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
	w := newWindowedReaderFromFetch(context.Background(), int64(len(data)), 8, nil, memoryFetch(data))
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
	w = newWindowedReaderFromFetch(context.Background(), int64(len(data)), 8, nil,
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
	if err := extractRAR4FromFetch(context.Background(), int64(len(data)), 16, nil, memoryFetch(data), handler); err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no entries extracted")
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
	err = extractRAR4FromFetch(ctx, int64(len(data)), 16, nil, memoryFetch(data), handler)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
