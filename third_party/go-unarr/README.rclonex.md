# go-unarr v0.2.5, local copy

This is a verbatim copy of `github.com/gen2brain/go-unarr` v0.2.5
(commit `7eb87112773e4a8aad4e8fcba3c61c4c24d28651`), with three added files:

- `stream.go` (module root) exposes `NewArchiveFromStream`.
- `unarrc/openstream.go` wraps unarr's private `ar_open_stream` constructor.
- `unarrc/stream_callbacks.go` exports Go callbacks for read, seek, tell and close.

No upstream file was modified. The shim exists because the public
`NewArchiveFromReader` constructor calls `io.ReadAll`, which would buffer a
whole remote archive in memory. The shim lets rclonex feed a bounded, ranged
`windowedReader` to the bundled unarr C decoder through a callback-backed
C stream.

The C stream API lives in `unarrc/external/unarr/common/unarr-imp.h` and is
marked private by upstream. This copy pins it so the shim cannot drift with an
upstream release. Replacing this directory requires re-checking those three
files against the new version.

CGO is required. The shim is only compiled when rclonex is built with
`-tags unarr`.
