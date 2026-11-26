package unarr

import (
	"github.com/gen2brain/go-unarr/unarrc"
)

// StreamOps supplies the callbacks used by NewArchiveFromStream. Read and Seek
// must report failures as errors so the caller can tell a remote I/O failure
// from a decoder failure.
type StreamOps = unarrc.StreamOps

// NewArchiveFromStream returns an Archive that reads from Go callbacks instead
// of buffering the archive in memory. ops must stay valid until Close is called.
func NewArchiveFromStream(ops *StreamOps) (*Archive, error) {
	if ops == nil {
		return nil, ErrOpenFile
	}
	a := new(Archive)
	a.stream = unarrc.OpenStream(unarrc.RegisterStream(ops))
	if a.stream == nil {
		return nil, ErrOpenFile
	}
	if err := a.open(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}
