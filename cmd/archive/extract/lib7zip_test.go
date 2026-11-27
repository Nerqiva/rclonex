//go:build cgo && !plan9

package extract

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
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
	_, err := operations.Rcat(ctx, src, "misnamed.7z", io.NopCloser(bytes.NewReader(makeTarGz(t, testArchiveEntries()))), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)

	err = ArchiveExtract(ctx, dst, "", src, "misnamed.7z", DefaultExtractOptions())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--archive-password")
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
			extractOne := func(legacy bool) []extractedFile {
				src := memoryFs(t, "src")
				dst := memoryFs(t, "dst")
				uploadFixtureVolumes(t, ctx, src, "", tc.volumes...)
				forceLegacyExtractor = legacy
				defer func() { forceLegacyExtractor = false }()
				require.NoError(t, ArchiveExtract(ctx, dst, "", src, tc.volumes[0], ExtractOptions{Password: tc.password}))
				return snapshotExtracted(t, ctx, dst)
			}

			byRemote := func(files []extractedFile) map[string]extractedFile {
				out := make(map[string]extractedFile, len(files))
				for _, f := range files {
					out[f.remote] = f
				}
				return out
			}
			got := byRemote(extractOne(false))
			want := byRemote(extractOne(true))
			require.Equal(t, len(want), len(got))
			for remote, expected := range want {
				actual, ok := got[remote]
				require.True(t, ok, "lib7zip did not extract %q", remote)
				require.Equal(t, expected.data, actual.data, "contents of %q", remote)
				require.True(t, expected.modTime.Equal(actual.modTime), "modTime of %q: got %v, want %v", remote, actual.modTime, expected.modTime)
			}
		})
	}
}
