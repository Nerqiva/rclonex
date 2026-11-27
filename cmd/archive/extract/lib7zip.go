//go:build cgo && !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/itchio/sevenzip-go/sz"
	"github.com/rclone/rclone/fs"
)

func init() {
	if path := os.Getenv("RCLONE_7ZIP_LIB"); path != "" {
		sz.LibraryPath = path
	}
}

func lib7zipAvailable() bool {
	_, err := lib7zipLibrary()
	return err == nil
}

type lib7zipReader struct {
	*windowedReader
}

// Close implements io.Closer for lib7zipReader.
func (r *lib7zipReader) Close() error { return nil }

func newLib7zipInStream(ctx context.Context, obj fs.Object, args lib7zipArgs) (*sz.InStream, *lib7zipReader, error) {
	r, err := newWindowedReader(ctx, obj, args.windowSize, args.tr, args.options...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open 7z stream for %q: %w", obj.Remote(), err)
	}
	lr := &lib7zipReader{windowedReader: r}
	if obj.Size() > 1 {
		var last [1]byte
		if _, err := lr.ReadAt(last[:], obj.Size()-1); err != nil {
			return nil, lr, err
		}
	}
	ext := strings.TrimPrefix(path.Ext(obj.Remote()), ".")
	in, err := sz.NewInStream(lr, ext, obj.Size())
	if err != nil {
		return nil, lr, fmt.Errorf("failed to create 7z input stream for %q: %w", obj.Remote(), err)
	}
	return in, lr, nil
}

type lib7zipVolumes struct {
	args    lib7zipArgs
	volumes []sevenZipVolume
	current int
	streams []*sz.InStream
}

// GetFirstVolumeName returns the first volume's base name.
func (v *lib7zipVolumes) GetFirstVolumeName() string {
	return path.Base(v.volumes[0].obj.Remote())
}

// MoveToVolume selects a volume by its base name.
func (v *lib7zipVolumes) MoveToVolume(name string) error {
	for i, volume := range v.volumes {
		if path.Base(volume.obj.Remote()) == name {
			v.current = i
			return nil
		}
	}
	return fmt.Errorf("7z volume %q not found in the remote set", name)
}

// GetCurrentVolumeSize returns the selected volume's size.
func (v *lib7zipVolumes) GetCurrentVolumeSize() uint64 {
	return uint64(v.volumes[v.current].obj.Size())
}

// OpenCurrentVolumeStream opens the selected volume as a ranged remote stream.
func (v *lib7zipVolumes) OpenCurrentVolumeStream() (*sz.InStream, error) {
	in, _, err := newLib7zipInStream(v.args.ctx, v.volumes[v.current].obj, v.args)
	if err != nil {
		return nil, err
	}
	v.streams = append(v.streams, in)
	return in, nil
}

var (
	lib7zipOnce sync.Once
	lib7zipLib  *sz.Lib
	lib7zipErr  error
	lib7zipMu   sync.Mutex
)

// lib7zipLibrary owns one Lib for the process. Freeing a Lib deinitialises the
// shared 7z engine, which cannot be reinitialised safely in the same process.
func lib7zipLibrary() (*sz.Lib, error) {
	lib7zipOnce.Do(func() {
		lib7zipLib, lib7zipErr = sz.NewLib()
	})
	return lib7zipLib, lib7zipErr
}

// lib7zip reports encrypted headers as ErrNeedPassword and encrypted contents
// as a failed extraction of entries marked encrypted.
func lib7zipEncryptionHint(err error, password string, encrypted bool) error {
	if err == nil {
		return nil
	}
	needPassword := encrypted || errors.Is(err, sz.ErrNeedPassword)
	unsupportedWithPassword := password != "" && errors.Is(err, sz.ErrNotSupportedArchive)
	if !needPassword && !unsupportedWithPassword {
		return err
	}
	if password == "" {
		return fmt.Errorf("archive is encrypted - use --archive-password to supply the password: %w", err)
	}
	return fmt.Errorf("failed to decrypt archive - the password supplied with --archive-password is incorrect (or the archive is corrupt): %w", err)
}

func lib7zipOpenError(args lib7zipArgs, format string, a ...any) error {
	err := lib7zipEncryptionHint(fmt.Errorf(format, a...), args.password, false)
	return &lib7zipDecodeError{err: err}
}

type ctxWriteCloser struct {
	ctx context.Context
	w   io.WriteCloser
}

func (w ctxWriteCloser) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func (w ctxWriteCloser) Close() error { return w.w.Close() }

type symlinkTargetWriter struct {
	buf bytes.Buffer
}

func (w *symlinkTargetWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > 64<<10 {
		return 0, errors.New("symlink target too long")
	}
	return w.buf.Write(p)
}

func (w *symlinkTargetWriter) Close() error { return nil }

func extractWithLib7zip(args lib7zipArgs) error {
	// The shared Lib and the 7z engine behind it are not reentrant, so only
	// one extraction may use them at a time.
	lib7zipMu.Lock()
	defer lib7zipMu.Unlock()

	lib, err := lib7zipLibrary()
	if err != nil {
		return fmt.Errorf("failed to initialize 7-Zip: %w", err)
	}
	args.tr.Reset(args.ctx)

	var streams []*sz.InStream
	defer func() {
		for _, stream := range streams {
			stream.Free()
		}
	}()

	var archive *sz.Archive
	scheme, multi := detectArchiveVolumes(args.srcFile)
	if multi {
		volumes, _, err := collectArchiveVolumes(args.ctx, args.src, args.srcFile)
		if err != nil {
			return fmt.Errorf("failed to collect the 7z volume set for %q: %w", args.srcFile, err)
		}
		volumesCB := &lib7zipVolumes{args: args, volumes: volumes}
		callback, err := sz.NewMultiVolumeCallback(volumesCB)
		if err != nil {
			return fmt.Errorf("failed to create the 7z volume callback: %w", err)
		}
		defer callback.Free()
		archive, err = lib.OpenMultiVolumeArchive(callback, args.password, scheme.kind != "7z")
		streams = append(streams, volumesCB.streams...)
		if err != nil {
			return lib7zipOpenError(args, "failed to open volume set %q: %w", args.srcFile, err)
		}
	} else {
		in, lr, err := newLib7zipInStream(args.ctx, args.srcObj, args)
		if err != nil {
			return err
		}
		streams = append(streams, in)
		if err := checkZipEndRecord(lr.windowedReader, args.srcObj.Size()); err != nil {
			return err
		}
		if kind := detectGoCompression(lr.windowedReader, args.srcObj.Size(), args.srcFile); kind != goCompressionNone {
			return extractGoCompressedTar(args, lib, kind, 0)
		}
		args.nested = hasNestedCompression(lr.windowedReader, args.srcObj.Size())
		archive, err = lib.OpenArchiveEx(in, args.password, true)
		if errors.Is(err, sz.ErrNotSupportedArchive) {
			// SFX archives carry their signature after a prefix. 7-Zip's
			// signature check only looks at registered offsets, so let it
			// select the handler from the file name and locate the format
			// from the archive tail instead.
			archive, err = lib.OpenArchiveEx(in, args.password, false)
		}
		if err != nil {
			return lib7zipOpenError(args, "failed to open %q: %w", args.srcFile, err)
		}
	}
	defer archive.Free()
	defer archive.Close()

	return extractItems(args, lib, archive, 0)
}

func checkZipEndRecord(r *windowedReader, size int64) error {
	if size < 4 {
		return nil
	}
	var magic [4]byte
	if _, err := r.ReadAt(magic[:], 0); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if !bytes.Equal(magic[:], []byte("PK\x03\x04")) && !bytes.Equal(magic[:], []byte("PK\x05\x06")) {
		return nil
	}
	const maxTail = 64<<10 + 22
	n := size
	if n > maxTail {
		n = maxTail
	}
	tail := make([]byte, n)
	if _, err := r.ReadAt(tail, size-n); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	for i := len(tail) - 22; i >= 0; i-- {
		if bytes.Equal(tail[i:i+4], []byte("PK\x05\x06")) {
			return nil
		}
	}
	return fmt.Errorf("%q is not a complete ZIP archive: central directory end record not found", r.name)
}

type streamReaderAt struct {
	r        io.ReadCloser
	regen    func() (io.ReadCloser, error)
	buf      []byte
	base     int64
	max      int64
	eof      bool
	restarts int
}

const maxNestedRestarts = 1000

func (s *streamReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < s.base {
		if s.regen == nil || s.restarts >= maxNestedRestarts {
			return 0, fmt.Errorf("nested archive seeks to offset %d behind the stream buffer at %d", off, s.base)
		}
		_ = s.r.Close()
		r, err := s.regen()
		if err != nil {
			return 0, err
		}
		s.r = r
		s.buf = nil
		s.base = 0
		s.eof = false
		s.restarts++
	}
	if off > s.base {
		drop := off - s.base
		if drop > int64(len(s.buf)) {
			skip := drop - int64(len(s.buf))
			discard := make([]byte, 64<<10)
			for skip > 0 {
				want := int64(len(discard))
				if skip < want {
					want = skip
				}
				n, err := s.r.Read(discard[:want])
				skip -= int64(n)
				if err != nil {
					if err == io.EOF {
						s.eof = true
					}
					return 0, err
				}
			}
			s.buf = s.buf[:0]
			s.base = off
		} else {
			s.buf = s.buf[drop:]
			s.base = off
		}
	}
	end := off + int64(len(p))
	for s.base+int64(len(s.buf)) < end && !s.eof {
		room := s.max - int64(len(s.buf))
		if room <= 0 {
			return 0, fmt.Errorf("nested archive buffer exceeded RCLONEX_ARCHIVE_BUFFER_SIZE of %d bytes", s.max)
		}
		want := end - (s.base + int64(len(s.buf)))
		if want > room {
			want = room
		}
		if want > 64<<10 {
			want = 64 << 10
		}
		chunk := make([]byte, int(want))
		n, err := s.r.Read(chunk)
		s.buf = append(s.buf, chunk[:n]...)
		if err == io.EOF {
			s.eof = true
			break
		}
		if err != nil {
			return 0, err
		}
	}
	available := s.base + int64(len(s.buf))
	if end > available {
		end = available
	}
	return copy(p, s.buf[:end-s.base]), nil
}

func (s *streamReaderAt) Close() error { return nil }

func archiveBufferSize() int64 {
	const defaultSize = 64 << 20
	value := os.Getenv("RCLONEX_ARCHIVE_BUFFER_SIZE")
	if value == "" {
		return defaultSize
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1<<20 {
		return defaultSize
	}
	return n
}

func hasNestedCompression(r *windowedReader, size int64) bool {
	if size < 3 {
		return false
	}
	var magic [6]byte
	n := len(magic)
	if size < int64(n) {
		n = int(size)
	}
	if _, err := r.ReadAt(magic[:n], 0); err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	switch {
	case magic[0] == 0x1f && magic[1] == 0x8b:
		return true
	case string(magic[:3]) == "BZh":
		return true
	case n == 6 && bytes.Equal(magic[:], []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}):
		return true
	}
	return false
}

func extractNestedArchive(args lib7zipArgs, lib *sz.Lib, archive *sz.Archive, item *sz.Item, depth int) error {
	start := func(ch chan error) *io.PipeReader {
		pr, pw := io.Pipe()
		go func() {
			out, err := sz.NewOutStream(pw)
			if err == nil {
				if args.password != "" {
					err = archive.ExtractWithPassword(item, out, args.password)
				} else {
					err = archive.Extract(item, out)
				}
				if closeErr := out.Close(); err == nil {
					err = closeErr
				}
			}
			if err != nil {
				_ = pw.CloseWithError(err)
			} else {
				_ = pw.Close()
			}
			ch <- err
		}()
		return pr
	}

	extractErrCh := make(chan error, 1)
	pr := start(extractErrCh)
	var sra *streamReaderAt
	sra = &streamReaderAt{
		r:   pr,
		max: archiveBufferSize(),
		regen: func() (io.ReadCloser, error) {
			_ = sra.r.Close()
			<-extractErrCh
			nextCh := make(chan error, 1)
			next := start(nextCh)
			extractErrCh = nextCh
			return next, nil
		},
	}
	defer func() {
		if closer, ok := sra.r.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	in, err := sz.NewInStream(sra, "tar", math.MaxInt64)
	if err != nil {
		item.Free()
		return err
	}
	defer in.Free()
	nested, err := lib.OpenArchiveEx(in, args.password, true)
	if err != nil {
		if pipe, ok := sra.r.(*io.PipeReader); ok {
			_ = pipe.CloseWithError(err)
		}
		extractErr := <-extractErrCh
		item.Free()
		if extractErr != nil {
			return &lib7zipDecodeError{err: extractErr}
		}
		return &lib7zipDecodeError{err: err}
	}
	err = extractItems(args, lib, nested, depth+1)
	nested.Close()
	nested.Free()
	if closer, ok := sra.r.(io.Closer); ok {
		_ = closer.Close()
	}
	<-extractErrCh
	item.Free()
	return err
}

func extractSymlinkTarget(args lib7zipArgs, archive *sz.Archive, item *sz.Item) (string, error) {
	target := &symlinkTargetWriter{}
	out, err := sz.NewOutStream(target)
	if err != nil {
		return "", err
	}
	if args.password != "" {
		err = archive.ExtractWithPassword(item, out, args.password)
	} else {
		err = archive.Extract(item, out)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	return target.buf.String(), nil
}

func extractItems(args lib7zipArgs, lib *sz.Lib, archive *sz.Archive, depth int) error {
	count, err := archive.GetItemCount()
	if err != nil {
		return fmt.Errorf("failed to read 7z item count: %w", err)
	}

	var errs []error
	for i := int64(0); i < count; i++ {
		item := archive.GetItem(i)
		if item == nil {
			continue
		}
		if err := args.ctx.Err(); err != nil {
			item.Free()
			return errors.Join(append(errs, err)...)
		}
		name, ok := item.GetStringProperty(sz.PidPath)
		if !ok || name == "" || name == "." {
			if depth < 2 && args.nested {
				errs = append(errs, extractNestedArchive(args, lib, archive, item, depth))
			} else {
				item.Free()
			}
			continue
		}
		name = strings.ReplaceAll(name, "\\", "/")
		name = strings.TrimPrefix(name, "./")
		isDir, _ := item.GetBoolProperty(sz.PidIsDir)
		size, _ := item.GetUInt64Property(sz.PidSize)
		encrypted, _ := item.GetBoolProperty(sz.PidEncrypted)
		mtime, ok := item.GetFileTimeProperty(sz.PidMTime)
		if !ok {
			if ticks, found := item.GetUInt64Property(sz.PidMTime); found && ticks > 0 {
				mtime = time.Unix(int64(ticks)/10000000-11644473600, int64(ticks)%10000000)
			} else {
				mtime = time.Time{}
			}
		}
		link, linked := item.GetStringProperty(sz.PidSymLink)
		if !linked || link == "" {
			if attr, found := item.GetUInt64Property(sz.PidPosixAttrib); found && attr&0xf000 == 0xa000 {
				target, targetErr := extractSymlinkTarget(args, archive, item)
				if targetErr != nil {
					errs = append(errs, fmt.Errorf("failed to read symlink %q: %w", name, targetErr))
					item.Free()
					continue
				}
				linked = true
				link = target
			}
		}
		if linked {
			item.Free()
			if !fs.GetConfig(args.ctx).Links {
				fs.Debugf(nil, "Skipping symlink %q in %s - use -l/--links to read it", name, args.srcFile)
				continue
			}
			err := args.handle(name+fs.LinkSuffix, int64(len(link)), mtime, false, func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(link)), nil
			})
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if encrypted && args.password == "" {
			item.Free()
			errs = append(errs, lib7zipEncryptionHint(fmt.Errorf("archive entry %q is encrypted", name), "", true))
			continue
		}
		if isDir {
			err := args.handle(name, 0, mtime, true, func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("")), nil
			})
			item.Free()
			if err != nil {
				errs = append(errs, err)
			}
			continue
		}

		pr, pw := io.Pipe()
		out, err := sz.NewOutStream(ctxWriteCloser{ctx: args.ctx, w: pw})
		if err != nil {
			_ = pr.Close()
			_ = pw.Close()
			item.Free()
			errs = append(errs, fmt.Errorf("failed to create 7z output stream for %q: %w", name, err))
			continue
		}
		uploadErr := make(chan error, 1)
		go func(entry string, declared int64, modTime time.Time) {
			err := args.handle(entry, declared, modTime, false, func() (io.ReadCloser, error) {
				return pr, nil
			})
			_ = pr.Close()
			uploadErr <- err
		}(name, int64(size), mtime)

		var extractErr error
		if args.password != "" {
			extractErr = archive.ExtractWithPassword(item, out, args.password)
		} else {
			extractErr = archive.Extract(item, out)
		}
		closeErr := out.Close()
		item.Free()
		if extractErr == nil {
			extractErr = closeErr
		}
		uploadResult := <-uploadErr
		var short *shortEntryError
		decryptFailed := encrypted && (extractErr != nil || errors.As(uploadResult, &short))
		if decryptFailed {
			errs = append(errs, lib7zipEncryptionHint(errors.Join(extractErr, uploadResult), args.password, true))
			continue
		}
		if extractErr != nil {
			errs = append(errs, &lib7zipDecodeError{err: fmt.Errorf("failed to extract %q: %w", name, extractErr)})
		}
		if uploadResult != nil {
			errs = append(errs, uploadResult)
		}
	}
	return errors.Join(errs...)
}
