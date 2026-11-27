//go:build cgo && !plan9

package extract

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/itchio/sevenzip-go/sz"
	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/minio/minlz"
	"github.com/pierrec/lz4/v4"
	lzip "github.com/sorairolake/lzip-go"
)

type goCompression int

const (
	goCompressionNone goCompression = iota
	goCompressionZstd
	goCompressionLZ4
	goCompressionBrotli
	goCompressionS2
	goCompressionLzip
	goCompressionMinLZ
)

func detectGoCompression(r *windowedReader, size int64, name string) goCompression {
	var magic [6]byte
	n := len(magic)
	if size < int64(n) {
		n = int(size)
	}
	if n > 0 {
		if _, err := r.ReadAt(magic[:n], 0); err != nil && err != io.EOF {
			return goCompressionNone
		}
	}
	if n >= 4 {
		switch {
		case bytes.Equal(magic[:4], []byte{0x28, 0xb5, 0x2f, 0xfd}):
			return goCompressionZstd
		case bytes.Equal(magic[:4], []byte{0x04, 0x22, 0x4d, 0x18}):
			return goCompressionLZ4
		case bytes.Equal(magic[:4], []byte("LZIP")):
			return goCompressionLzip
		}
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".br"):
		return goCompressionBrotli
	case strings.HasSuffix(lower, ".sz"):
		return goCompressionS2
	case strings.HasSuffix(lower, ".mz"):
		return goCompressionMinLZ
	}
	return goCompressionNone
}

type compressionReader struct {
	io.Reader
	closer io.Closer
}

func (r *compressionReader) Close() error { return r.closer.Close() }

type zstdCompressionReader struct {
	*zstd.Decoder
	closer io.Closer
}

func (r *zstdCompressionReader) Close() error {
	r.Decoder.Close()
	return r.closer.Close()
}

type s2CompressionReader struct {
	*s2.Reader
	closer io.Closer
}

func (r *s2CompressionReader) Close() error { return r.closer.Close() }

func openGoCompression(kind goCompression, args lib7zipArgs) (io.ReadCloser, error) {
	base, err := newWindowedReader(args.ctx, args.srcObj, args.windowSize, args.tr, args.options...)
	if err != nil {
		return nil, err
	}
	in := io.NopCloser(base)
	switch kind {
	case goCompressionZstd:
		zr, err := zstd.NewReader(in)
		if err != nil {
			_ = in.Close()
			return nil, err
		}
		return &zstdCompressionReader{Decoder: zr, closer: in}, nil
	case goCompressionLZ4:
		return &compressionReader{Reader: lz4.NewReader(in), closer: in}, nil
	case goCompressionBrotli:
		return &compressionReader{Reader: brotli.NewReader(in), closer: in}, nil
	case goCompressionS2:
		return &s2CompressionReader{Reader: s2.NewReader(in), closer: in}, nil
	case goCompressionLzip:
		lr, err := lzip.NewReader(in)
		if err != nil {
			_ = in.Close()
			return nil, err
		}
		return &compressionReader{Reader: lr, closer: in}, nil
	case goCompressionMinLZ:
		return &compressionReader{Reader: minlz.NewReader(in), closer: in}, nil
	}
	_ = in.Close()
	return nil, fmt.Errorf("unsupported compression method")
}

func extractGoCompressedTar(args lib7zipArgs, lib *sz.Lib, kind goCompression, depth int) error {
	if depth >= 2 {
		return fmt.Errorf("nested archive depth limit reached for %q", args.srcFile)
	}
	open := func() (io.ReadCloser, error) { return openGoCompression(kind, args) }
	r, err := open()
	if err != nil {
		return err
	}
	sra := &streamReaderAt{r: r, max: archiveBufferSize()}
	sra.regen = func() (io.ReadCloser, error) {
		_ = sra.r.Close()
		return open()
	}
	defer func() { _ = sra.r.Close() }()

	in, err := sz.NewInStream(sra, "tar", math.MaxInt64)
	if err != nil {
		return &lib7zipDecodeError{err: err}
	}
	defer in.Free()
	nested, err := lib.OpenArchiveEx(in, "", true)
	if err != nil {
		return &lib7zipDecodeError{err: err}
	}
	err = extractItems(args, lib, nested, depth+1)
	nested.Close()
	nested.Free()
	return err
}
