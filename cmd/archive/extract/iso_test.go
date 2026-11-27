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
	requireLib7zip(t)
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "test.iso", io.NopCloser(bytes.NewReader(makeISO(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "test.iso", DefaultExtractOptions()))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractISOWindowSizeZero(t *testing.T) {
	requireLib7zip(t)
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "test.iso", io.NopCloser(bytes.NewReader(makeISO(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	require.NoError(t, ArchiveExtract(ctx, dst, "", src, "test.iso", ExtractOptions{WindowSize: 0}))
	checkExtracted(t, ctx, dst, entries)
}

func TestArchiveExtractISOPasswordRejected(t *testing.T) {
	requireLib7zip(t)
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")
	entries := testArchiveEntries()

	_, err := operations.Rcat(ctx, src, "test.iso", io.NopCloser(bytes.NewReader(makeISO(t, entries))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "test.iso", ExtractOptions{Password: "secret"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--archive-password")
}

func TestArchiveExtractISONotISO(t *testing.T) {
	requireLib7zip(t)
	ctx := context.Background()
	src := memoryFs(t, "src")
	dst := memoryFs(t, "dst")

	// Zeroed beyond the system area and missing the CD001 identifier
	data := make([]byte, 64*1024)
	_, err := operations.Rcat(ctx, src, "not.iso", io.NopCloser(bytes.NewReader(data)), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "not.iso", DefaultExtractOptions())
	assert.Error(t, err)
}
