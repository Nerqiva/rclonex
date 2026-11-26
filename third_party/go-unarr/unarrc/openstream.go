package unarrc

/*
#include "unarr.h"
#include "external/unarr/common/unarr-imp.h"

extern void goUnarrClose(void *data);
extern size_t goUnarrRead(void *data, void *buffer, size_t count);
extern unsigned char goUnarrSeek(void *data, off64_t offset, int origin);
extern off64_t goUnarrTell(void *data);

static bool go_unarr_seek(void *data, off64_t offset, int origin) {
	return goUnarrSeek(data, offset, origin) != 0;
}

static ar_stream *go_unarr_open_stream(void *data) {
	ar_stream *s = ar_open_stream(data, goUnarrClose, goUnarrRead, go_unarr_seek, goUnarrTell);
	return s;
}
*/
import "C"

import "unsafe"

// OpenStream opens an unarr stream backed by the Go callbacks registered for
// data. It returns nil when unarr cannot allocate the stream.
func OpenStream(data unsafe.Pointer) *Stream {
	ret := C.go_unarr_open_stream(data)
	v := *(**Stream)(unsafe.Pointer(&ret))
	return v
}
