//go:build !plan9

package extract

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	diskfsfile "github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeISO builds a real ISO9660 image holding the entries, with Rock
// Ridge enabled so names keep their case and the modtime is preserved.
// The image is produced by go-diskfs itself and returned as bytes.
func makeISO(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	workspace := t.TempDir()
	for _, e := range entries {
		full := filepath.Join(workspace, filepath.FromSlash(e.name))
		if e.dir {
			require.NoError(t, os.MkdirAll(full, 0o755))
			require.NoError(t, os.Chtimes(full, e.modTime, e.modTime))
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, e.data, 0o644))
		require.NoError(t, os.Chtimes(full, e.modTime, e.modTime))
	}
	img := filepath.Join(t.TempDir(), "test.iso")
	const size = int64(32 * 1024 * 1024)
	out, err := diskfsfile.CreateFromPath(img, size)
	require.NoError(t, err)
	fsys, err := iso9660.Create(out, size, 0, 2048, workspace)
	require.NoError(t, err)
	require.NoError(t, fsys.Finalize(iso9660.FinalizeOptions{RockRidge: true}))
	data, err := os.ReadFile(img)
	require.NoError(t, err)
	return data
}

func TestArchiveExtractISO(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "test.iso", io.NopCloser(bytes.NewReader(makeISO(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "test.iso"))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractISOWindowSizeZero(t *testing.T) {
	ctx := withExtractOptions(context.Background(), extractOptions{windowSize: 0})
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "test.iso", io.NopCloser(bytes.NewReader(makeISO(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "test.iso"))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractISOPasswordRejected(t *testing.T) {
	ctx := withExtractOptions(context.Background(), extractOptions{password: "secret"})
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "test.iso", io.NopCloser(bytes.NewReader(makeISO(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "test.iso")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--archive-password")
}

func TestArchiveExtractISONotISO(t *testing.T) {
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")

	// Zeroed beyond the system area and missing the CD001 identifier
	data := make([]byte, 64*1024)
	_, err := operations.Rcat(ctx, src, "not.iso", io.NopCloser(bytes.NewReader(data)), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "not.iso")
	assert.Error(t, err)
}

func TestProbeISO(t *testing.T) {
	iso := makeISO(t, testArchiveEntries())
	assert.True(t, probeISO(bytes.NewReader(iso), int64(len(iso))))
	assert.False(t, probeISO(bytes.NewReader(make([]byte, 1024)), 1024))
}
