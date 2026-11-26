//go:build !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/nwaples/rardecode/v2"
)

// entryHandler extracts one archive entry. It is shared by every extraction
// path in this package.
type entryHandler func(nameInArchive string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error

// errRAR4FallbackUnavailable is returned by the fallback when this build does
// not include the unarr decoder.
var errRAR4FallbackUnavailable = errors.New("RAR4 fallback is not compiled into this build (build with CGO_ENABLED=1 -tags unarr)")

// rar4Signature is the RAR 4.x archive magic. The archives library maps RAR4
// and RAR5 to the same format, so the signature has to be checked separately.
var rar4Signature = []byte("Rar!\x1a\x07\x00")

// isRAR4 reports whether in starts with a RAR4 signature. It leaves in
// positioned at the start again.
func isRAR4(in io.ReadSeeker) bool {
	var signature [7]byte
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return false
	}
	if _, err := io.ReadFull(in, signature[:]); err != nil {
		return false
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return false
	}
	return bytes.Equal(signature[:], rar4Signature)
}

// isRecoverableRAR4DecodeError reports whether err is a rardecode decoder
// failure that another RAR4 decoder might be able to recover from.
//
// It deliberately only accepts decoder sentinels. Remote I/O, destination
// I/O, permissions, cancellation and infrastructure errors never match, even
// when they are wrapped by the extraction library.
func isRecoverableRAR4DecodeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, errRangeNotSupported) {
		return false
	}
	recoverable := []error{
		rardecode.ErrBadFileChecksum,
		rardecode.ErrShortFile,
		rardecode.ErrCorruptBlockHeader,
		rardecode.ErrCorruptFileHeader,
		rardecode.ErrBadHeaderCRC,
		rardecode.ErrDecoderOutOfData,
		rardecode.ErrUnexpectedArcEnd,
		rardecode.ErrInvalidFileBlock,
		rardecode.ErrCorruptPPM,
		rardecode.ErrHuffDecodeFailed,
		rardecode.ErrInvalidLengthTable,
		rardecode.ErrTooManyFilters,
		rardecode.ErrInvalidFilter,
		rardecode.ErrUnsupportedDecoder,
		rardecode.ErrUnknownDecoder,
		rardecode.ErrMultipleDecoders,
	}
	for _, sentinel := range recoverable {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}
