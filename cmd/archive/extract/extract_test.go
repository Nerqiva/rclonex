//go:build !plan9

package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mholt/archives"
	_ "github.com/rclone/rclone/backend/memory"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		dstDir   string
		expected string
		wantErr  bool
	}{
		{
			name:     "sanitized name returned",
			input:    "./subdir/file.txt",
			expected: "subdir/file.txt",
		},
		{
			name:     "joined onto destination directory",
			input:    "file.txt",
			dstDir:   "safe/prefix",
			expected: "safe/prefix/file.txt",
		},
		{
			name:     "archive root entry skipped",
			input:    "./",
			expected: "",
		},
		{
			name:    "leading dot-dot rejected",
			input:   "../etc/passwd",
			wantErr: true,
		},
		{
			name:    "interior dot-dot rejected with destination",
			input:   "dir/../../escaped.txt",
			dstDir:  "safe/prefix",
			wantErr: true,
		},
		{
			name:    "backslash dot-dot rejected",
			input:   `..\escaped.txt`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := destPath(tc.input, tc.dstDir)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, got)
		})
	}
}

type testArchiveEntry struct {
	name    string
	data    []byte
	dir     bool
	modTime time.Time
}

func testArchiveEntries() []testArchiveEntry {
	mt := time.Unix(1735689600, 0).UTC() // 2025-01-01T00:00:00Z
	mt2 := time.Unix(1735776000, 0).UTC()
	mt3 := time.Unix(1735862400, 0).UTC()
	payload := make([]byte, 0, 256*64)
	for i := 0; i < 256*64; i++ {
		payload = append(payload, byte(i%256))
	}
	return []testArchiveEntry{
		{name: "hello.txt", data: []byte("hello rclonex\n"), modTime: mt},
		{name: "payload.bin", data: payload, modTime: mt2},
		{name: "sub/", dir: true, modTime: mt3},
		{name: "sub/dir/", dir: true, modTime: mt3},
		{name: "sub/dir/nested.txt", data: []byte("nested rclonex\n"), modTime: mt3},
	}
}

func makeZip(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, e := range entries {
		if e.dir {
			_, err := zw.Create(e.name)
			require.NoError(t, err)
			continue
		}
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.Modified = e.modTime
		w, err := zw.CreateHeader(hdr)
		require.NoError(t, err)
		_, err = w.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func makeTarGz(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		if e.dir {
			require.NoError(t, tw.WriteHeader(&tar.Header{
				Name:     e.name,
				Typeflag: tar.TypeDir,
				Mode:     0o755,
				ModTime:  e.modTime,
			}))
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     e.name,
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(e.data)),
			ModTime:  e.modTime,
		}))
		_, err := tw.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// checkExtracted compares the extracted files in dst against the expected
// entries and returns the number of regular files found.
func checkExtracted(t *testing.T, ctx context.Context, dst fs.Fs, entries []testArchiveEntry) {
	t.Helper()
	found := 0
	for _, e := range entries {
		if e.dir {
			continue
		}
		obj, err := dst.NewObject(ctx, e.name)
		require.NoError(t, err, "entry %q", e.name)
		in, err := obj.Open(ctx)
		require.NoError(t, err)
		got, err := io.ReadAll(in)
		require.NoError(t, err)
		require.NoError(t, in.Close())
		assert.Equal(t, e.data, got, "entry %q", e.name)
		mt := obj.ModTime(ctx)
		assert.Equal(t, e.modTime.Unix(), mt.Unix(), "entry %q modtime", e.name)
		found++
	}
	assert.Greater(t, found, 0)
}

func TestArchiveExtractZip(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "archive.zip", io.NopCloser(bytes.NewReader(makeZip(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "archive.zip"))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractZipWindowSizeZero(t *testing.T) {
	ctx := withExtractOptions(context.Background(), extractOptions{windowSize: 0})
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "archive.zip", io.NopCloser(bytes.NewReader(makeZip(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "archive.zip"))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractTarGz(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "archive.tar.gz", io.NopCloser(bytes.NewReader(makeTarGz(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "archive.tar.gz"))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractCorruptZip(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()
	data := makeZip(t, entries)

	// Truncate the archive - the central directory is missing
	_, err := operations.Rcat(ctx, src, "corrupt.zip", io.NopCloser(bytes.NewReader(data[:len(data)/2])), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "corrupt.zip")
	assert.Error(t, err)
}

// rangeIgnoringFs is an fs.Fs whose objects ignore RangeOption and return
// the whole file, like a remote which doesn't support range reads.
type rangeIgnoringFs struct {
	data []byte
}

func (f *rangeIgnoringFs) Name() string             { return "rangeignore" }
func (f *rangeIgnoringFs) Root() string             { return "" }
func (f *rangeIgnoringFs) String() string           { return "rangeIgnoring fs" }
func (f *rangeIgnoringFs) Precision() time.Duration { return time.Nanosecond }
func (f *rangeIgnoringFs) Hashes() hash.Set         { return hash.NewHashSet() }
func (f *rangeIgnoringFs) Features() *fs.Features   { return &fs.Features{} }
func (f *rangeIgnoringFs) List(ctx context.Context, dir string) (fs.DirEntries, error) {
	return nil, fs.ErrorDirNotFound
}
func (f *rangeIgnoringFs) Put(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) (fs.Object, error) {
	return nil, errors.New("not implemented")
}
func (f *rangeIgnoringFs) Mkdir(ctx context.Context, dir string) error { return nil }
func (f *rangeIgnoringFs) Rmdir(ctx context.Context, dir string) error {
	return errors.New("not implemented")
}

func (f *rangeIgnoringFs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	if remote != "archive.zip" {
		return nil, fs.ErrorObjectNotFound
	}
	return &rangeIgnoringObject{data: f.data}, nil
}

func TestArchiveExtractRangeNotSupported(t *testing.T) {
	ctx := context.Background()
	src := &rangeIgnoringFs{data: makeZip(t, testArchiveEntries())}
	dst := memoryFs(t, "dst")

	err := ArchiveExtract(ctx, dst, "", src, "archive.zip")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "range")
}

func TestArchiveExtractPassword(t *testing.T) {
	for _, name := range []string{"encrypted.7z", "encrypted-files.7z", "encrypted.rar"} {
		for _, tc := range []struct {
			name     string
			password string
			wantErr  bool
		}{
			{"no password", "", true},
			{"wrong password", "wrong", true},
			{"correct password", "rclonex", false},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				ctx := withExtractOptions(context.Background(), extractOptions{password: tc.password})
				src := memoryFs(t, "src")
				dst := memoryFs(t, "dst")
				archiveData, err := os.ReadFile("testdata/" + name)
				require.NoError(t, err)
				_, err = operations.Rcat(ctx, src, name, io.NopCloser(bytes.NewReader(archiveData)), time.Unix(1735689600, 0).UTC(), nil)
				require.NoError(t, err)

				err = ArchiveExtract(ctx, dst, "", src, name)
				if tc.wantErr {
					assert.Error(t, err)
					if strings.HasSuffix(name, ".7z") {
						// the extraction library reports a missing or wrong
						// password as a low-level error - check the hint is there
						assert.Contains(t, err.Error(), "--archive-password")
					}
					return
				}
				require.NoError(t, err)
				for _, e := range testArchiveEntries() {
					if e.dir {
						continue
					}
					obj, err := dst.NewObject(ctx, e.name)
					require.NoError(t, err)
					in, err := obj.Open(ctx)
					require.NoError(t, err)
					got, err := io.ReadAll(in)
					require.NoError(t, err)
					require.NoError(t, in.Close())
					assert.Equal(t, e.data, got, "entry %q", e.name)
				}
			})
		}
	}
}

func TestApplyOptions(t *testing.T) {
	opts := func(password string) extractOptions {
		return extractOptions{password: password}
	}

	t.Run("rar with password", func(t *testing.T) {
		got, err := applyOptions(archives.Rar{}, opts("secret"))
		require.NoError(t, err)
		assert.Equal(t, archives.Rar{Password: "secret"}, got)
	})
	t.Run("7z with password", func(t *testing.T) {
		got, err := applyOptions(archives.SevenZip{}, opts("secret"))
		require.NoError(t, err)
		assert.Equal(t, archives.SevenZip{Password: "secret"}, got)
	})
	t.Run("zip without password", func(t *testing.T) {
		got, err := applyOptions(archives.Zip{}, opts(""))
		require.NoError(t, err)
		assert.Equal(t, archives.Zip{}, got)
	})
	t.Run("tar without password", func(t *testing.T) {
		got, err := applyOptions(archives.Tar{}, opts(""))
		require.NoError(t, err)
		assert.Equal(t, archives.Tar{}, got)
	})
	t.Run("zip with password rejected", func(t *testing.T) {
		_, err := applyOptions(archives.Zip{}, opts("secret"))
		require.Error(t, err)
	})
	t.Run("tar with password rejected", func(t *testing.T) {
		_, err := applyOptions(archives.Tar{}, opts("secret"))
		require.Error(t, err)
	})
	t.Run("compressed archive with rar inside gets password", func(t *testing.T) {
		got, err := applyOptions(archives.CompressedArchive{Extraction: archives.Rar{}, Compression: archives.Gz{}}, opts("secret"))
		require.NoError(t, err)
		ca, ok := got.(archives.CompressedArchive)
		require.True(t, ok)
		assert.Equal(t, archives.Rar{Password: "secret"}, ca.Extraction)
	})
	t.Run("compressed archive with zip inside rejects password", func(t *testing.T) {
		_, err := applyOptions(archives.CompressedArchive{Extraction: archives.Zip{}, Compression: archives.Gz{}}, opts("secret"))
		require.Error(t, err)
	})
}
