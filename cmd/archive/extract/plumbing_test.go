//go:build !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveExtractPrefixedZip(t *testing.T) {
	requireLib7zip(t)
	ctx := context.Background()
	src := memoryFs(t, "prefixed-src")
	dst := memoryFs(t, "prefixed-dst")
	entries := testArchiveEntries()
	prefixed := append([]byte("#!/bin/sh\nprefix bytes\n"), makeZip(t, entries)...)

	_, err := operations.Rcat(ctx, src, "sfx.zip", io.NopCloser(bytes.NewReader(prefixed)), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "sfx.zip", DefaultExtractOptions())
	require.NoError(t, err)
	checkExtracted(t, ctx, dst, entries)
}

type failAfterReader struct {
	remaining int
	sent      bool
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		if r.sent {
			return 0, io.EOF
		}
		r.sent = true
		return 0, errors.New("decoder failed halfway through")
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.remaining -= n
	return n, nil
}

// partialUploadFs stores a truncated object and then reports an upload error,
// modelling a remote that has already accepted bytes when a decoder fails.
type partialUploadFs struct {
	fs.Fs
}

func (f *partialUploadFs) Features() *fs.Features {
	features := *f.Fs.Features()
	features.PutStream = f.PutStream
	return &features
}

func (f *partialUploadFs) PutStream(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	buf := make([]byte, 64<<10)
	n, _ := io.ReadFull(in, buf)
	obj, err := operations.Rcat(ctx, f.Fs, src.Remote(), io.NopCloser(bytes.NewReader(buf[:n])), src.ModTime(ctx), nil)
	if err != nil {
		return obj, err
	}
	return obj, errors.New("decoder failed halfway through")
}

func TestUploadEntryRemovesPartialOnDecoderError(t *testing.T) {
	ctx := context.Background()
	dst := &partialUploadFs{Fs: memoryFs(t, "partial-dst")}

	err := uploadEntry(ctx, dst, "file.bin", "file.bin", -1, time.Time{}, func() (io.ReadCloser, error) {
		return io.NopCloser(&failAfterReader{remaining: 120 << 10}), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoder failed halfway through")

	_, err = dst.NewObject(ctx, "file.bin")
	assert.ErrorIs(t, err, fs.ErrorObjectNotFound, "partial upload must be removed")
}

func TestUploadEntryRemovesPartialOnByteCountMismatch(t *testing.T) {
	ctx := context.Background()
	dst := memoryFs(t, "short-dst")

	err := uploadEntry(ctx, dst, "file.bin", "file.bin", 10, time.Time{}, func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("short")), nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 10")
	_, err = dst.NewObject(ctx, "file.bin")
	assert.ErrorIs(t, err, fs.ErrorObjectNotFound, "partial upload must be removed")
}

func TestUploadEntryReportsOpenError(t *testing.T) {
	ctx := context.Background()
	dst := memoryFs(t, "open-error-dst")

	err := uploadEntry(ctx, dst, "file.bin", "file.bin", -1, time.Time{}, func() (io.ReadCloser, error) {
		return nil, errors.New("open failed: " + strings.Repeat("x", 1))
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open failed")
}

type failedPutFs struct {
	fs.Fs
}

func (f *failedPutFs) Features() *fs.Features {
	features := *f.Fs.Features()
	features.PutStream = func(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
		_, _ = io.Copy(io.Discard, in)
		return nil, errors.New("decoder failed before an object was created")
	}
	return &features
}

func (f *failedPutFs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	_, _ = io.Copy(io.Discard, in)
	return nil, errors.New("decoder failed before an object was created")
}

func TestUploadEntryKeepsExistingObjectOnDecoderError(t *testing.T) {
	ctx := context.Background()
	base := memoryFs(t, "keep-existing-dst")
	_, err := operations.Rcat(ctx, base, "file.bin", io.NopCloser(strings.NewReader("old")), time.Time{}, nil)
	require.NoError(t, err)

	dst := &failedPutFs{Fs: base}
	err = uploadEntry(ctx, dst, "file.bin", "file.bin", -1, time.Time{}, func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("new")), nil
	})
	require.Error(t, err)

	obj, err := base.NewObject(ctx, "file.bin")
	require.NoError(t, err, "a failed upload must not delete a pre-existing object")
	in, err := obj.Open(ctx)
	require.NoError(t, err)
	data, err := io.ReadAll(in)
	require.NoError(t, err)
	require.NoError(t, in.Close())
	require.Equal(t, "old", string(data))
}

func TestArchiveExtractIdentifiesByContentBeforeName(t *testing.T) {
	requireLib7zip(t)
	ctx := context.Background()
	src := memoryFs(t, "misnamed-src")
	dst := memoryFs(t, "misnamed-dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "misnamed.zip", io.NopCloser(bytes.NewReader(makeTarGz(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "misnamed.zip", DefaultExtractOptions()))
	checkExtracted(t, ctx, dst, entries)
}
