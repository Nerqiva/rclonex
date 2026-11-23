//go:build !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	_ "github.com/rclone/rclone/backend/memory"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uploadFixtureVolumes uploads the fixture volumes to src, optionally under
// prefix, e.g. "sub/archive".
func uploadFixtureVolumes(t *testing.T, ctx context.Context, src fs.Fs, prefix string, names ...string) {
	t.Helper()
	for _, name := range names {
		data, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		remote := name
		if prefix != "" {
			remote = prefix + "/" + name
		}
		_, err = operations.Rcat(ctx, src, remote, io.NopCloser(bytes.NewReader(data)), time.Unix(1735689600, 0).UTC(), nil)
		require.NoError(t, err)
	}
}

// assertFixtureContents checks the files extracted from the multi-volume
// fixtures - hello.txt, payload.bin and sub/dir/nested.txt.
func assertFixtureContents(t *testing.T, ctx context.Context, dst fs.Fs) {
	t.Helper()
	for name, data := range map[string][]byte{
		"hello.txt":          []byte("hello rclonex\n"),
		"payload.bin":        testData(16384),
		"sub/dir/nested.txt": []byte("nested rclonex\n"),
	} {
		obj, err := dst.NewObject(ctx, name)
		require.NoError(t, err, "entry %q", name)
		in, err := obj.Open(ctx)
		require.NoError(t, err)
		got, err := io.ReadAll(in)
		require.NoError(t, err)
		require.NoError(t, in.Close())
		assert.Equal(t, data, got, "entry %q", name)
	}
}

// eofChunkFs wraps a memory fs so every Open reports the final bytes with
// io.EOF, like an HTTP response body
type eofChunkFs struct{ fs.Fs }

func (f *eofChunkFs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	obj, err := f.Fs.NewObject(ctx, remote)
	if err != nil {
		return nil, err
	}
	return &eofChunkObject{obj}, nil
}

type eofChunkObject struct{ fs.Object }

func (o *eofChunkObject) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	rc, err := o.Object.Open(ctx, options...)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(rc)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}
	_ = rc.Close()
	return &eofChunkReader{data: data}, nil
}

// eofChunkReader returns io.EOF alongside the final data chunk, not on the
// following zero-length read
type eofChunkReader struct {
	data []byte
	done bool
}

func (r *eofChunkReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		r.done = true
		return n, io.EOF
	}
	return n, nil
}

func (r *eofChunkReader) Close() error { return nil }

func TestExtractRarMultiVolumeChunkedEOF(t *testing.T) {
	ctx := context.Background()
	base := memoryFs(t, "src")
	src := &eofChunkFs{base}
	dst := memoryFs(t, "dst")
	uploadFixtureVolumes(t, ctx, base, "", "mv-multivolume.part1.rar", "mv-multivolume.part2.rar", "mv-multivolume.part3.rar")

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "mv-multivolume.part1.rar"))
	assertFixtureContents(t, ctx, dst)
}

func TestExtractRarMultiVolume(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	uploadFixtureVolumes(t, ctx, src, "", "mv-multivolume.part1.rar", "mv-multivolume.part2.rar", "mv-multivolume.part3.rar")

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "mv-multivolume.part1.rar"))
	assertFixtureContents(t, ctx, dst)
}

func TestExtractRarMultiVolumePathPrefix(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	uploadFixtureVolumes(t, ctx, src, "sub/archive", "mv-multivolume.part1.rar", "mv-multivolume.part2.rar", "mv-multivolume.part3.rar")

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "sub/archive/mv-multivolume.part1.rar"))
	assertFixtureContents(t, ctx, dst)
}

func TestExtractRarMultiVolumeOldNaming(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	uploadFixtureVolumes(t, ctx, src, "", "mv-multivolume-old.rar", "mv-multivolume-old.r00", "mv-multivolume-old.r01")

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "mv-multivolume-old.rar"))
	assertFixtureContents(t, ctx, dst)
}

func TestExtractRarMultiVolumeMissingVolume(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	// part2 is missing - payload.bin spans it
	uploadFixtureVolumes(t, ctx, src, "", "mv-multivolume.part1.rar", "mv-multivolume.part3.rar")

	err := ArchiveExtract(ctx, dst, "", src, "mv-multivolume.part1.rar")
	assert.Error(t, err)
}

func TestExtractRarMultiVolumePassword(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"no password", "", true},
		{"wrong password", "wrong", true},
		{"correct password", "rclonex", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := withExtractOptions(context.Background(), extractOptions{password: tc.password})
			src := memoryFs(t, "src")
			dst := memoryFs(t, "dst")
			uploadFixtureVolumes(t, ctx, src, "", "mv-encrypted.part1.rar", "mv-encrypted.part2.rar", "mv-encrypted.part3.rar")

			err := ArchiveExtract(ctx, dst, "", src, "mv-encrypted.part1.rar")
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assertFixtureContents(t, ctx, dst)
		})
	}
}

func TestExtractRarSingleVolumeViaRemoteFS(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	uploadFixtureVolumes(t, ctx, src, "", "mv-single.rar")

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "mv-single.rar"))
	assertFixtureContents(t, ctx, dst)
}

func TestExtractSevenZipMultiVolume(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	uploadFixtureVolumes(t, ctx, src, "", "mv-multivolume.7z.001", "mv-multivolume.7z.002", "mv-multivolume.7z.003")

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "mv-multivolume.7z.001"))
	assertFixtureContents(t, ctx, dst)
}

func TestExtractSevenZipMultiVolumePassword(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"no password", "", true},
		{"wrong password", "wrong", true},
		{"correct password", "rclonex", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := withExtractOptions(context.Background(), extractOptions{password: tc.password})
			src := memoryFs(t, "src")
			dst := memoryFs(t, "dst")
			uploadFixtureVolumes(t, ctx, src, "", "mv-encrypted.7z.001", "mv-encrypted.7z.002", "mv-encrypted.7z.003")

			err := ArchiveExtract(ctx, dst, "", src, "mv-encrypted.7z.001")
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assertFixtureContents(t, ctx, dst)
		})
	}
}

func TestExtractSevenZipMultiVolumeMissingVolume(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	// .002 is missing - the volume collection stops at .001 and the archive
	// header at the true end is unreadable
	uploadFixtureVolumes(t, ctx, src, "", "mv-multivolume.7z.001", "mv-multivolume.7z.003")

	err := ArchiveExtract(ctx, dst, "", src, "mv-multivolume.7z.001")
	assert.Error(t, err)
}

// shortErrorReader yields its data then fails with err on the next read.
type shortErrorReader struct {
	data []byte
	err  error
	sent bool
}

func (r *shortErrorReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	return 0, r.err
}

func (r *shortErrorReader) Close() error { return nil }

func TestConcatVolumesReaderPropagatesReadError(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("volume read failed")
	const size = 8
	fetch := func(ctx context.Context, off, want int64) (io.ReadCloser, error) {
		return &shortErrorReader{data: []byte("ab"), err: sentinel}, nil
	}
	state := &concatState{
		volumes: []sevenZipVolume{{size: size, base: 0}},
		readers: []*windowedReader{newWindowedReaderFromFetch(ctx, size, 0, nil, fetch)},
	}
	r := &concatVolumesReader{state: state, pos: 0, end: size}

	n, err := r.Read(make([]byte, size))

	assert.Equal(t, 2, n)
	assert.ErrorIs(t, err, sentinel)
}
