//go:build !plan9

package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"

	"github.com/nwaples/rardecode/v2"
)

func TestIsRAR4(t *testing.T) {
	rar4 := append(append([]byte(nil), rar4Signature...), []byte("payload")...)
	if !isRAR4(bytes.NewReader(rar4)) {
		t.Fatal("RAR4 signature not detected")
	}
	rar5 := []byte("Rar!\x1a\x07\x01\x00payload")
	if isRAR4(bytes.NewReader(rar5)) {
		t.Fatal("RAR5 signature detected as RAR4")
	}
	if isRAR4(bytes.NewReader([]byte("not an archive"))) {
		t.Fatal("plain data detected as RAR4")
	}
}

func TestIsRecoverableRAR4DecodeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"bad checksum", rardecode.ErrBadFileChecksum, true},
		{"wrapped short file", fmt.Errorf("handling file: x: %w", rardecode.ErrShortFile), true},
		{"corrupt header", rardecode.ErrCorruptBlockHeader, true},
		{"encrypted file", rardecode.ErrArchivedFileEncrypted, false},
		{"encrypted archive", rardecode.ErrArchiveEncrypted, false},
		{"multivolume", rardecode.ErrMultiVolume, false},
		{"context canceled", context.Canceled, false},
		{"deadline", fmt.Errorf("fetch: %w", context.DeadlineExceeded), false},
		{"range unsupported", errRangeNotSupported, false},
		{"permission", &os.PathError{Op: "open", Path: "/secret", Err: syscall.EACCES}, false},
		{"remote failure", errors.New("failed to open file"), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isRecoverableRAR4DecodeError(test.err); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
