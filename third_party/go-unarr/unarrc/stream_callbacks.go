package unarrc

/*
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

extern void goUnarrClose(void *data);
extern size_t goUnarrRead(void *data, void *buffer, size_t count);
extern unsigned char goUnarrSeek(void *data, int64_t offset, int origin);
extern int64_t goUnarrTell(void *data);
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// StreamOps holds the Go callbacks backing a stream opened by OpenStream.
type StreamOps struct {
	Close func()
	Read  func(p []byte) (int, error)
	Seek  func(offset int64, whence int) (int64, error)
	Tell  func() int64
}

var (
	streamSeq atomic.Uint64
	streamOps sync.Map // uintptr -> *StreamOps
)

// RegisterStream registers ops and returns the opaque handle passed to the C
// stream callbacks. The handle stays valid until the stream is closed.
func RegisterStream(ops *StreamOps) unsafe.Pointer {
	if ops == nil {
		return nil
	}
	id := uintptr(streamSeq.Add(1))
	streamOps.Store(id, ops)
	return unsafe.Pointer(id)
}

func lookupStream(data unsafe.Pointer) *StreamOps {
	v, ok := streamOps.Load(uintptr(data))
	if !ok {
		return nil
	}
	ops, _ := v.(*StreamOps)
	return ops
}

//export goUnarrClose
func goUnarrClose(data unsafe.Pointer) {
	ops := lookupStream(data)
	streamOps.Delete(uintptr(data))
	if ops != nil && ops.Close != nil {
		ops.Close()
	}
}

//export goUnarrRead
func goUnarrRead(data unsafe.Pointer, buffer unsafe.Pointer, count C.size_t) C.size_t {
	ops := lookupStream(data)
	if ops == nil || ops.Read == nil || count == 0 {
		return 0
	}
	p := unsafe.Slice((*byte)(buffer), int(count))
	n, _ := ops.Read(p)
	if n < 0 {
		n = 0
	}
	if uintptr(n) > uintptr(count) {
		n = int(count)
	}
	return C.size_t(n)
}

//export goUnarrSeek
func goUnarrSeek(data unsafe.Pointer, offset C.int64_t, origin C.int) bool {
	ops := lookupStream(data)
	if ops == nil || ops.Seek == nil {
		return false
	}
	_, err := ops.Seek(int64(offset), int(origin))
	return err == nil
}

//export goUnarrTell
func goUnarrTell(data unsafe.Pointer) C.int64_t {
	ops := lookupStream(data)
	if ops == nil || ops.Tell == nil {
		return -1
	}
	return C.int64_t(ops.Tell())
}
