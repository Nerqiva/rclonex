//go:build !plan9

package extract

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/md5"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/rclone/rclone/backend/local"
	"github.com/rclone/rclone/fs"

	"github.com/stretchr/testify/require"
)

// patternReader yields an infinite stream of byte(b) bytes.
type patternReader struct{ b byte }

func (p patternReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = p.b
	}
	return len(b), nil
}

// cyclingPatternReader yields an infinite stream of pattern repeated.
type cyclingPatternReader struct {
	pattern []byte
	pos     int
}

func (p *cyclingPatternReader) Read(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		n := copy(b[written:], p.pattern[p.pos:])
		written += n
		p.pos = (p.pos + n) % len(p.pattern)
	}
	return written, nil
}

// peakRSSBytes returns the process' peak resident set size, or 0 if it is
// not measurable on this platform.
func peakRSSBytes(t *testing.T) int64 {
	t.Helper()
	return maxRSSBytes()
}

// extractTarGzAndMeasure extracts srcName from src to dst, checks the
// payload size and hash against a constant pattern, and returns peak RSS.
func checkLargeExtraction(t *testing.T, dstDir, payloadName string, size int64, expectedMD5 [16]byte) {
	t.Helper()
	verify := func() {
		f, err := os.Open(filepath.Join(dstDir, payloadName))
		require.NoError(t, err)
		defer func() { require.NoError(t, f.Close()) }()
		sum := md5.New()
		n, err := io.Copy(sum, f)
		require.NoError(t, err)
		require.EqualValues(t, size, n)
		require.Equal(t, expectedMD5, [16]byte(sum.Sum(nil)))
	}
	verify()
}

func TestArchiveExtractLargeTarGz(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in -short mode")
	}
	const payloadSize = 256 << 20 // 256 MiB of 0x61
	ctx := context.Background()

	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "src")
	dstDir := filepath.Join(tmp, "dst")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	archivePath := filepath.Join(srcDir, "big.tar.gz")
	func() {
		out, err := os.Create(archivePath)
		require.NoError(t, err)
		gw := gzip.NewWriter(out)
		tw := tar.NewWriter(gw)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     "payload.bin",
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     payloadSize,
			ModTime:  time.Unix(1735689600, 0).UTC(),
		}))
		_, err = io.CopyN(tw, patternReader{b: 0x61}, payloadSize)
		require.NoError(t, err)
		require.NoError(t, tw.Close())
		require.NoError(t, gw.Close())
		require.NoError(t, out.Close())
	}()

	m := md5.New()
	_, _ = io.CopyN(m, patternReader{b: 0x61}, payloadSize)
	expect := [16]byte(m.Sum(nil))

	src, err := fs.NewFs(ctx, srcDir)
	require.NoError(t, err)
	dst, err := fs.NewFs(ctx, dstDir)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "big.tar.gz"))
	checkLargeExtraction(t, dstDir, "payload.bin", payloadSize, expect)

	if rss := peakRSSBytes(t); rss > 0 {
		const maxRSS = 512 << 20 // 512 MiB
		if rss > maxRSS {
			t.Fatalf("extraction peak RSS %d bytes exceeds budget %d", rss, maxRSS)
		}
		t.Logf("peak RSS: %d MiB (budget 512 MiB)", rss>>20)
	}
}

// TestArchiveExtractLargeSevenZipMultiVolume extracts a multi-volume 7z
// archive of a 48 MiB constant payload. The committed testdata/bigmv.7z.001..004
// fixture is tiny (the payload is highly compressible) yet still exercises
// the full extraction path.
//
// Note peak RSS is a process-wide high-water mark, so - as with the other
// large file tests - the budget must be generous; the focused regression
// check for the window reader's memory retention is in reader_test.go.
func TestArchiveExtractLargeSevenZipMultiVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in -short mode")
	}
	const payloadSize = 48 << 20 // 48 MiB of 'a' bytes

	ctx := context.Background()

	dstDir := t.TempDir()
	dst, err := fs.NewFs(ctx, dstDir)
	require.NoError(t, err)
	src := memoryFs(t, "src")
	uploadFixtureVolumes(t, ctx, src, "", "bigmv.7z.001", "bigmv.7z.002", "bigmv.7z.003", "bigmv.7z.004")

	m := md5.New()
	_, err = io.CopyN(m, patternReader{b: 0x61}, payloadSize)
	require.NoError(t, err)
	expect := [16]byte(m.Sum(nil))

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "bigmv.7z.001"))
	checkLargeExtraction(t, dstDir, "payload.bin", payloadSize, expect)

	if rss := peakRSSBytes(t); rss > 0 {
		const maxRSS = 512 << 20 // 512 MiB
		if rss > maxRSS {
			t.Fatalf("extraction peak RSS %d bytes exceeds budget %d", rss, maxRSS)
		}
		t.Logf("peak RSS: %d MiB (budget 512 MiB)", rss>>20)
	}
}

func TestArchiveExtractLargeZipRandomAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in -short mode")
	}
	const payloadSize = 64 << 20 // 64 MiB of repeating 0x00..0xff pattern
	ctx := context.Background()

	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "src")
	dstDir := filepath.Join(tmp, "dst")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	pattern := make([]byte, 1<<20)
	for i := range pattern {
		pattern[i] = byte(i)
	}
	archivePath := filepath.Join(srcDir, "big.zip")
	func() {
		out, err := os.Create(archivePath)
		require.NoError(t, err)
		zw := zip.NewWriter(out)
		w, err := zw.Create("payload.bin")
		require.NoError(t, err)
		_, err = io.CopyN(w, &cyclingPatternReader{pattern: pattern}, payloadSize)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		require.NoError(t, out.Close())
	}()

	m := md5.New()
	for i := int64(0); i < payloadSize/int64(len(pattern)); i++ {
		_, _ = m.Write(pattern)
	}
	expect := [16]byte(m.Sum(nil))

	src, err := fs.NewFs(ctx, srcDir)
	require.NoError(t, err)
	dst, err := fs.NewFs(ctx, dstDir)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "big.zip"))
	checkLargeExtraction(t, dstDir, "payload.bin", payloadSize, expect)

	if rss := peakRSSBytes(t); rss > 0 {
		const maxRSS = 512 << 20
		if rss > maxRSS {
			t.Fatalf("extraction peak RSS %d bytes exceeds budget %d", rss, maxRSS)
		}
		t.Logf("peak RSS: %d MiB (budget 512 MiB)", rss>>20)
	}
}
