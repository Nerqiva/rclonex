//go:build cgo && unarr && !plan9

package extract

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gen2brain/go-unarr"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
)

// rar4Stream maps a windowedReader onto the callbacks used by unarr. A Go
// error can't cross the C callback boundary, so it is kept here and checked
// after every unarr call; remote I/O failures then stay infrastructure
// errors instead of looking like decoder failures.
type rar4Stream struct {
	r   *windowedReader
	err error
}

func (s *rar4Stream) read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && s.err == nil {
		s.err = err
	}
	return n, err
}

func (s *rar4Stream) seek(offset int64, whence int) (int64, error) {
	pos, err := s.r.Seek(offset, whence)
	if err != nil && s.err == nil {
		s.err = err
	}
	return pos, err
}

func (s *rar4Stream) tell() int64 {
	pos, err := s.r.Seek(0, io.SeekCurrent)
	if err != nil && s.err == nil {
		s.err = err
	}
	return pos
}

func (s *rar4Stream) check() error {
	if s.err != nil {
		return s.err
	}
	return s.r.ctx.Err()
}

type unarrEntryReader struct {
	archive   *unarr.Archive
	stream    *rar4Stream
	remaining int
}

func (e *unarrEntryReader) Read(p []byte) (int, error) {
	if err := e.stream.check(); err != nil {
		return 0, err
	}
	if e.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > int64(e.remaining) {
		p = p[:e.remaining]
	}
	n, err := e.archive.Read(p)
	e.remaining -= n
	if streamErr := e.stream.check(); streamErr != nil {
		return n, streamErr
	}
	return n, err
}

func (e *unarrEntryReader) Close() error {
	return nil
}

var _ io.ReadCloser = (*unarrEntryReader)(nil)

type rar4Fetch struct {
	size       int64
	windowSize int64
	fetch      rangeFetch
}

func (f rar4Fetch) extract(ctx context.Context, handleEntry entryHandler) error {
	w := newWindowedReaderFromFetch(ctx, f.size, f.windowSize, f.fetch)
	stream := &rar4Stream{r: w}
	archive, err := unarr.NewArchiveFromStream(&unarr.StreamOps{
		Read: stream.read,
		Seek: stream.seek,
		Tell: stream.tell,
	})
	if err != nil {
		if streamErr := stream.check(); streamErr != nil {
			return streamErr
		}
		return err
	}
	defer archive.Close()

	for {
		if err := stream.check(); err != nil {
			return err
		}
		err := archive.Entry()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if streamErr := stream.check(); streamErr != nil {
				return streamErr
			}
			return err
		}
		name := archive.Name()
		size := archive.Size()
		if size < 0 {
			return fmt.Errorf("RAR4 entry %q: negative size", name)
		}
		// unarr has no directory flag. Name() is path.Clean'ed, so use the
		// raw name to spot the trailing separator RAR uses for directories.
		rawName := archive.RawName()
		isDir := strings.HasSuffix(rawName, "/") || strings.HasSuffix(rawName, "\\")
		entry := &unarrEntryReader{archive: archive, stream: stream, remaining: size}
		if err := handleEntry(name, int64(size), archive.ModTime(), isDir, func() (io.ReadCloser, error) {
			return entry, nil
		}); err != nil {
			return err
		}
	}
}

func extractRAR4WithUnarr(ctx context.Context, srcObj fs.Object, windowSize int64, tr *accounting.Transfer, handleEntry entryHandler) error {
	var options []fs.OpenOption
	for _, option := range fs.GetConfig(ctx).DownloadHeaders {
		options = append(options, option)
	}
	return rar4Fetch{
		size:       srcObj.Size(),
		windowSize: windowSize,
		fetch:      objectFetch(srcObj, tr, options...),
	}.extract(ctx, handleEntry)
}
