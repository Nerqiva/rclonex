//go:build cgo && !plan9

package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type extractedFile struct {
	remote  string
	data    []byte
	modTime time.Time
}

func snapshotExtracted(t *testing.T, ctx context.Context, dst fs.Fs) []extractedFile {
	t.Helper()
	entries, err := dst.List(ctx, "")
	require.NoError(t, err)
	out := make([]extractedFile, 0, len(entries))
	for _, entry := range entries {
		obj, ok := entry.(fs.Object)
		if !ok {
			continue
		}
		in, err := obj.Open(ctx)
		require.NoError(t, err, "open %q", obj.Remote())
		data, err := io.ReadAll(in)
		require.NoError(t, err)
		require.NoError(t, in.Close())
		out = append(out, extractedFile{remote: obj.Remote(), data: data, modTime: obj.ModTime(ctx)})
	}
	return out
}

func TestLib7zipUnsupportedArchiveWithoutPassword(t *testing.T) {
	if _, err := lib7zipLibrary(); err != nil {
		t.Skipf("lib7zip engine unavailable: %v", err)
	}

	ctx := context.Background()
	src := memoryFs(t, "unsupported-src")
	dst := memoryFs(t, "unsupported-dst")
	_, err := operations.Rcat(ctx, src, "not-an-archive.bin", io.NopCloser(bytes.NewReader([]byte("this is not an archive"))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "not-an-archive.bin", DefaultExtractOptions())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--archive-password")
}

func TestLib7zipRar5Method(t *testing.T) {
	if _, err := lib7zipLibrary(); err != nil {
		t.Skipf("lib7zip engine unavailable: %v", err)
	}

	ctx := context.Background()
	src := memoryFs(t, "rar5-src")
	dst := memoryFs(t, "rar5-dst")
	data, err := os.ReadFile("testdata/encrypted.rar")
	require.NoError(t, err)
	_, err = operations.Rcat(ctx, src, "encrypted.rar", io.NopCloser(bytes.NewReader(data)), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "encrypted.rar", ExtractOptions{Password: "rclonex"})
	require.NoError(t, err)
	obj, err := dst.NewObject(ctx, "hello.txt")
	require.NoError(t, err)
	in, err := obj.Open(ctx)
	require.NoError(t, err)
	got, err := io.ReadAll(in)
	require.NoError(t, err)
	require.NoError(t, in.Close())
	assert.Equal(t, "hello rclonex\n", string(got))
}

func TestLib7zipParity(t *testing.T) {
	if _, err := lib7zipLibrary(); err != nil {
		t.Skipf("lib7zip engine unavailable: %v", err)
	}

	cases := []struct {
		name     string
		password string
		volumes  []string
	}{
		{name: "encrypted.7z", password: "rclonex", volumes: []string{"encrypted.7z"}},
		{name: "encrypted-files.7z", password: "rclonex", volumes: []string{"encrypted-files.7z"}},
		{name: "mv-multivolume.7z.001", volumes: []string{"mv-multivolume.7z.001", "mv-multivolume.7z.002", "mv-multivolume.7z.003"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			src := memoryFs(t, "src")
			dst := memoryFs(t, "dst")
			uploadFixtureVolumes(t, ctx, src, "", tc.volumes...)
			require.NoError(t, ArchiveExtract(ctx, dst, "", src, tc.volumes[0], ExtractOptions{Password: tc.password}))
			assertFixtureContents(t, ctx, dst)
			for _, name := range []string{"hello.txt", "payload.bin", "sub/dir/nested.txt"} {
				obj, err := dst.NewObject(ctx, name)
				require.NoError(t, err, "entry %q", name)
				assert.False(t, obj.ModTime(ctx).IsZero(), "modTime of %q", name)
			}
		})
	}
}

func TestLib7zipRar4(t *testing.T) {
	if _, err := lib7zipLibrary(); err != nil {
		t.Skipf("lib7zip engine unavailable: %v", err)
	}

	ctx := context.Background()
	src := memoryFs(t, "rar4-src")
	dst := memoryFs(t, "rar4-dst")
	uploadFixtureVolumes(t, ctx, src, "", "rar4.rar")
	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "rar4.rar", DefaultExtractOptions()))
	obj, err := dst.NewObject(ctx, "rar")
	require.NoError(t, err)
	assert.False(t, obj.ModTime(ctx).IsZero(), "rar modtime")
	in, err := obj.Open(ctx)
	require.NoError(t, err)
	data, err := io.ReadAll(in)
	require.NoError(t, err)
	require.NoError(t, in.Close())
	require.Equal(t, []byte("unarr\n"), data)
}

func makeSymlinkTarGz(t *testing.T) []byte {
	t.Helper()
	mt := time.Unix(1735689600, 0).UTC()
	buf := &bytes.Buffer{}
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "hello.txt", Typeflag: tar.TypeReg, Mode: 0o644,
		Size: int64(len("hello rclonex\n")), ModTime: mt,
	}))
	_, err := tw.Write([]byte("hello rclonex\n"))
	require.NoError(t, err)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "link", Linkname: "hello.txt", Typeflag: tar.TypeSymlink,
		Mode: 0o777, ModTime: mt,
	}))
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func makeSymlinkZip(t *testing.T) []byte {
	t.Helper()
	mt := time.Unix(1735689600, 0).UTC()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	hdr := &zip.FileHeader{Name: "hello.txt", Method: zip.Deflate}
	hdr.Modified = mt
	w, err := zw.CreateHeader(hdr)
	require.NoError(t, err)
	_, err = w.Write([]byte("hello rclonex\n"))
	require.NoError(t, err)
	hdr = &zip.FileHeader{Name: "link"}
	hdr.Modified = mt
	hdr.SetMode(os.ModeSymlink | 0o777)
	w, err = zw.CreateHeader(hdr)
	require.NoError(t, err)
	_, err = w.Write([]byte("hello.txt"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestLib7zipSymlinks(t *testing.T) {
	if _, err := lib7zipLibrary(); err != nil {
		t.Skipf("lib7zip engine unavailable: %v", err)
	}

	cases := []struct {
		name string
		make func(*testing.T) []byte
	}{
		{name: "archive.tar.gz", make: makeSymlinkTarGz},
		{name: "archive.zip", make: makeSymlinkZip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, links := range []bool{false, true} {
				t.Run(fmt.Sprintf("links=%v", links), func(t *testing.T) {
					ctx := context.Background()
					if links {
						var cfg *fs.ConfigInfo
						ctx, cfg = fs.AddConfig(ctx)
						cfg.Links = true
					}
					src := memoryFs(t, "src")
					dst := memoryFs(t, "dst")
					_, err := operations.Rcat(ctx, src, tc.name, io.NopCloser(bytes.NewReader(tc.make(t))), time.Unix(1735689600, 0).UTC(), nil)
					require.NoError(t, err)
					require.NoError(t, ArchiveExtract(ctx, dst, "", src, tc.name, DefaultExtractOptions()))
					obj, err := dst.NewObject(ctx, "hello.txt")
					require.NoError(t, err)
					require.Equal(t, int64(1735689600), obj.ModTime(ctx).Unix())
					linkObj, err := dst.NewObject(ctx, "link"+fs.LinkSuffix)
					if !links {
						require.Error(t, err)
						return
					}
					require.NoError(t, err)
					in, err := linkObj.Open(ctx)
					require.NoError(t, err)
					link, err := io.ReadAll(in)
					require.NoError(t, err)
					require.NoError(t, in.Close())
					require.Equal(t, "hello.txt", string(link))
				})
			}
		})
	}
}
