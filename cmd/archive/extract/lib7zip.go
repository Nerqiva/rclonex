//go:build cgo && !plan9

package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/itchio/sevenzip-go/sz"
	"github.com/rclone/rclone/fs"
)

func init() {
	if path := os.Getenv("RCLONE_7ZIP_LIB"); path != "" {
		sz.LibraryPath = path
	}
}

// lib7zipReader adapts the ranged remote reader to the stream interface the
// 7-Zip binding requires. The remote reader owns its own resources.
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
	ext := strings.TrimPrefix(path.Ext(obj.Remote()), ".")
	in, err := sz.NewInStream(lr, ext, obj.Size())
	if err != nil {
		return nil, lr, fmt.Errorf("failed to create 7z input stream for %q: %w", obj.Remote(), err)
	}
	return in, lr, nil
}

// lib7zipVolumes exposes a remote volume set through the callback interface
// used by lib7zip. No volume is written to local storage.
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

func extractWithLib7zip(args lib7zipArgs) error {
	lib, err := sz.NewLib()
	if err != nil {
		return fmt.Errorf("%w: failed to initialize 7-Zip: %w", errLib7zipUnavailable, err)
	}
	defer lib.Free()
	args.tr.Reset(args.ctx)

	var streams []*sz.InStream
	defer func() {
		for _, stream := range streams {
			stream.Free()
		}
	}()

	var archive *sz.Archive
	if strings.HasSuffix(strings.ToLower(args.srcFile), ".001") {
		volumes, _, err := collectSevenZipVolumes(args.ctx, args.src, args.srcFile)
		if err != nil {
			return fmt.Errorf("failed to collect the 7z volume set for %q: %w", args.srcFile, err)
		}
		volumesCB := &lib7zipVolumes{args: args, volumes: volumes}
		callback, err := sz.NewMultiVolumeCallback(volumesCB)
		if err != nil {
			return fmt.Errorf("failed to create the 7z volume callback: %w", err)
		}
		defer callback.Free()
		archive, err = lib.OpenMultiVolumeArchive(callback, args.password, false)
		streams = append(streams, volumesCB.streams...)
		if err != nil {
			return fmt.Errorf("failed to open 7z volume set %q: %w", args.srcFile, err)
		}
	} else {
		in, _, err := newLib7zipInStream(args.ctx, args.srcObj, args)
		if err != nil {
			return err
		}
		streams = append(streams, in)
		archive, err = lib.OpenArchiveEx(in, args.password, false)
		if err != nil {
			return fmt.Errorf("failed to open 7z archive %q: %w", args.srcFile, err)
		}
	}
	defer archive.Free()
	defer archive.Close()

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
		name, ok := item.GetStringProperty(sz.PidPath)
		if !ok {
			item.Free()
			continue
		}
		name = strings.ReplaceAll(name, "\\", "/")
		name = strings.TrimPrefix(name, "./")
		if name == "" || name == "." {
			item.Free()
			continue
		}
		isDir, _ := item.GetBoolProperty(sz.PidIsDir)
		size, _ := item.GetUInt64Property(sz.PidSize)
		mtime, ok := item.GetFileTimeProperty(sz.PidMTime)
		if !ok {
			if ticks, found := item.GetUInt64Property(sz.PidMTime); found && ticks > 0 {
				mtime = time.Unix(int64(ticks)/10000000-11644473600, int64(ticks)%10000000)
			} else {
				mtime = time.Time{}
			}
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
		out, err := sz.NewOutStream(pw)
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

		extractErr := archive.Extract(item, out)
		closeErr := out.Close()
		item.Free()
		if extractErr == nil {
			extractErr = closeErr
		}
		uploadResult := <-uploadErr
		if extractErr != nil {
			errs = append(errs, fmt.Errorf("failed to extract %q: %w", name, extractErr))
		}
		if uploadResult != nil {
			errs = append(errs, uploadResult)
		}
	}
	return errors.Join(errs...)
}
