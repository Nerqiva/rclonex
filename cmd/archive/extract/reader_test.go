//go:build !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	_ "github.com/rclone/rclone/backend/memory"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testMemoryFsCounter int64

func testData(n int) []byte {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, byte(i%256))
	}
	return out
}

func memoryFs(t *testing.T, name string) fs.Fs {
	t.Helper()
	testMemoryFsCounter++
	f, err := fs.NewFs(context.Background(), fmt.Sprintf(":memory:%s-%d", name, testMemoryFsCounter))
	require.NoError(t, err)
	return f
}

func writeMemObject(t *testing.T, ctx context.Context, f fs.Fs, remote string, data []byte) fs.Object {
	t.Helper()
	obj, err := operations.Rcat(ctx, f, remote, io.NopCloser(bytes.NewReader(data)), time.Unix(1735689600, 0).UTC(), nil)
	require.NoError(t, err)
	return obj
}

func testWindowedReader(t *testing.T, ctx context.Context, obj fs.Object, windowSize int64) *windowedReader {
	t.Helper()
	r, err := newWindowedReader(ctx, obj, windowSize, nil)
	require.NoError(t, err)
	return r
}

func TestWindowedReaderReadAt(t *testing.T) {
	ctx := context.Background()
	data := testData(10000)
	f := memoryFs(t, "windowed")
	obj := writeMemObject(t, ctx, f, "file.bin", data)

	for _, tc := range []struct {
		name string
		off  int64
		len  int
	}{
		{"from start", 0, 100},
		{"unaligned", 333, 1000},
		{"straddling window boundary", 99, 202},
		{"last byte", int64(len(data) - 1), 1},
		{"whole file", 0, len(data)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := testWindowedReader(t, ctx, obj, 128)
			p := make([]byte, tc.len)
			n, err := r.ReadAt(p, tc.off)
			require.NoError(t, err)
			assert.Equal(t, tc.len, n)
			assert.Equal(t, data[tc.off:tc.off+int64(tc.len)], p)
		})
	}
}

func TestWindowedReaderReadAtEOF(t *testing.T) {
	ctx := context.Background()
	data := testData(100)
	f := memoryFs(t, "windowed")
	obj := writeMemObject(t, ctx, f, "file.bin", data)
	r := testWindowedReader(t, ctx, obj, 16)

	p := make([]byte, 10)
	n, err := r.ReadAt(p, 95)
	assert.Equal(t, 5, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, data[95:], p[:n])

	n, err = r.ReadAt(p, 100)
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, io.EOF)

	_, err = r.ReadAt(p, -1)
	assert.Error(t, err)
}

func TestWindowedReaderWindowReuse(t *testing.T) {
	ctx := context.Background()
	data := testData(10000)
	f := memoryFs(t, "windowed")
	obj := writeMemObject(t, ctx, f, "file.bin", data)
	r := testWindowedReader(t, ctx, obj, 128)

	p := make([]byte, 64)
	_, err := r.ReadAt(p, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, r.numFetches())

	_, err = r.ReadAt(make([]byte, 32), 32)
	require.NoError(t, err)
	_, err = r.ReadAt(make([]byte, 1), 127)
	require.NoError(t, err)
	assert.Equal(t, 1, r.numFetches())

	_, err = r.ReadAt(make([]byte, 1), 128)
	require.NoError(t, err)
	assert.Equal(t, 2, r.numFetches())

	_, err = r.ReadAt(make([]byte, 1000), 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, r.numFetches(), 3)
}

func TestWindowedReaderWindowDisabled(t *testing.T) {
	ctx := context.Background()
	data := testData(1000)
	f := memoryFs(t, "windowed")
	obj := writeMemObject(t, ctx, f, "file.bin", data)
	r := testWindowedReader(t, ctx, obj, 0)

	p := make([]byte, 100)
	n, err := r.ReadAt(p, 500)
	require.NoError(t, err)
	assert.Equal(t, 100, n)
	assert.Equal(t, data[500:600], p)
	_, err = r.ReadAt(make([]byte, 100), 500)
	require.NoError(t, err)
	assert.Equal(t, 2, r.numFetches())
	assert.Nil(t, r.win)
}

func TestWindowedReaderWindowDisabledRangeNotSupported(t *testing.T) {
	ctx := context.Background()
	obj := &rangeIgnoringObject{data: testData(10000)}
	r := testWindowedReader(t, ctx, obj, 0)

	_, err := r.ReadAt(make([]byte, 100), 500)
	assert.ErrorIs(t, err, errRangeNotSupported)
	_, err = r.ReadAt(make([]byte, 100), 0)
	assert.ErrorIs(t, err, errRangeNotSupported)
}

func TestWindowedReaderSeek(t *testing.T) {
	ctx := context.Background()
	data := testData(1000)
	f := memoryFs(t, "windowed")
	obj := writeMemObject(t, ctx, f, "file.bin", data)
	r := testWindowedReader(t, ctx, obj, 64)

	n, err := r.Seek(0, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), n)

	n, err = r.Seek(-10, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)-10), n)

	n, err = r.Seek(5, io.SeekCurrent)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)-5), n)

	n, err = r.Seek(100, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(100), n)

	n, err = r.Seek(-50, io.SeekCurrent)
	require.NoError(t, err)
	assert.Equal(t, int64(50), n)

	_, err = r.Seek(-200, io.SeekCurrent)
	assert.Error(t, err)

	_, err = r.Seek(0, 99)
	assert.Error(t, err)

	n, err = r.Seek(2000, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(2000), n)
	_, err = r.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF)

	_, err = r.Seek(0, io.SeekStart)
	require.NoError(t, err)
	p := make([]byte, 100)
	rn, err := r.Read(p)
	assert.NoError(t, err)
	assert.Equal(t, 100, rn)
	assert.Equal(t, data[:100], p)
	rn, err = r.Read(p)
	assert.NoError(t, err)
	assert.Equal(t, 100, rn)
	assert.Equal(t, data[100:200], p)
}

type rangeIgnoringObject struct {
	data []byte
}

func (o *rangeIgnoringObject) Fs() fs.Info { return nil }
func (o *rangeIgnoringObject) String() string {
	return "rangeIgnoringObject"
}
func (o *rangeIgnoringObject) Remote() string { return "file.bin" }
func (o *rangeIgnoringObject) ModTime(ctx context.Context) time.Time {
	return time.Unix(1735689600, 0).UTC()
}
func (o *rangeIgnoringObject) Size() int64 { return int64(len(o.data)) }
func (o *rangeIgnoringObject) Hash(ctx context.Context, ty hash.Type) (string, error) {
	return "", nil
}
func (o *rangeIgnoringObject) Storable() bool { return true }
func (o *rangeIgnoringObject) SetModTime(ctx context.Context, t time.Time) error {
	return nil
}
func (o *rangeIgnoringObject) Open(ctx context.Context, options ...fs.OpenOption) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.data)), nil
}
func (o *rangeIgnoringObject) Update(ctx context.Context, in io.Reader, src fs.ObjectInfo, options ...fs.OpenOption) error {
	return errors.New("not implemented")
}
func (o *rangeIgnoringObject) Remove(ctx context.Context) error {
	return errors.New("not implemented")
}

func TestWindowedReaderRangeNotSupported(t *testing.T) {
	ctx := context.Background()
	obj := &rangeIgnoringObject{data: testData(10000)}
	r := testWindowedReader(t, ctx, obj, 1024)

	_, err := r.ReadAt(make([]byte, 100), 0)
	assert.ErrorIs(t, err, errRangeNotSupported)
	assert.Contains(t, err.Error(), "file.bin")
}

func TestWindowedReaderRangeNotSupportedUnaligned(t *testing.T) {
	ctx := context.Background()
	obj := &rangeIgnoringObject{data: testData(10000)}
	r := testWindowedReader(t, ctx, obj, 1024)

	_, err := r.ReadAt(make([]byte, 10), 5000)
	assert.ErrorIs(t, err, errRangeNotSupported)
}
