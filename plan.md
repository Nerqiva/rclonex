# Plan: lib7zip as the main archive engine in rclonex

## 1. Goal and constraints

Use the NyaMisty fork of `github.com/itchio/sevenzip-go` as the main archive engine in `rclonex`, for the archive formats rclone supports today plus the formats 7-Zip supports.

Hard requirements:

- No loss of existing rclone functionality.
- No mount and no disk staging for extraction. Extraction must stream from remote objects into destination objects.
- cgo enabled on all release targets.
- Use the NyaMisty fork, not plain upstream (upstream lacks password and multi-volume support).
- Switch all archive formats, not only 7z.
- Do not add `.7z` to the `backend/archive` virtual filesystem.

Desirable but not mandatory:

- A single-file install. Sidecar library files are acceptable if needed.

Motivation: some ZIP and RAR files fail to extract with no clear cause, in rclonex and in rclone stable. An engine swap was requested as the fix.

## 2. Decisions already made

| Decision | Choice |
|---|---|
| Fork | NyaMisty sevenzip-go, vendored under `third_party/sevenzip-go` |
| Module path | `github.com/itchio/sevenzip-go` via a `replace` directive |
| Targets | cgo on every release target |
| C delivery | Vendored Go bindings; runtime libraries may be sidecars |
| Format scope | All formats, converted in one wave with parity tests |
| Virtual filesystem | No `.7z` archiver in `backend/archive` |

## 3. Current state of the experimental branch

Branch: `experimental/lib7zip-engine`.

Commits already on the branch:

- `b47b68c2b` archive extract: add lib7zip engine for 7z archives
- `849458664` go: add checksum for sevenzip-go dependency
- `919e4c512` archive extract: tolerate missing 7z filetime symbol

Files added or changed:

- `third_party/sevenzip-go/` — vendored fork source, with `LibraryPath` override in `sz/libpath.go` and an optional filetime symbol in `sz/glue.c`.
- `cmd/archive/extract/lib7zip_common.go` — shared argument type, format dispatch, and `errLib7zipUnavailable`.
- `cmd/archive/extract/lib7zip.go` — cgo engine: ranged `InStream`, remote-backed multi-volume callbacks, per-entry streaming through `io.Pipe` into `entryHandler`, password, mtime fallback.
- `cmd/archive/extract/lib7zip_stub.go` — non-cgo stub.
- `cmd/archive/extract/extract.go` — dispatch for `.7z` and `.7z.001`, with fallback to the previous extractor.
- `go.mod`, `go.sum` — dependency and replace directive.

Verification already done:

- `go build ./...` passes with cgo.
- `CGO_ENABLED=0 go build ./...` passes.
- `go test ./cmd/archive/extract/...` passes with and without cgo.
- The new engine itself is **not** yet verified end to end because of the library mismatch described below.

## 4. Critical library finding

The fork does not compile 7-Zip into the Go binary. `sz/glue.c` uses `dlopen` and loads these at runtime:

- Linux: `libc7zip.so` and `7z.so`
- macOS: `libc7zip.dylib` and `7z.so`
- Windows: `c7zip.dll` and `7z.dll`

It looks only beside the executable, not in `PATH`, `LD_LIBRARY_PATH`, or `DYLD_LIBRARY_PATH`.

Facts established in this session:

- `github.com/itchio/libc7zip` current release removed `archive_open_ex`, so the fork cannot initialize with it.
- `github.com/NyaMisty/libc7zip` master and its v1.8.0 release export `archive_open_ex` but not `item_get_filetime_property`. The vendored glue was patched to make that symbol optional and the Go side falls back to a uint64 tick property.
- With either library pair available here, `archive_open_ex` segfaults during `OpenArchiveEx`. The likely cause is a mismatch between the vendored struct layout and the library build, or between `libc7zip` and the `7z.so` used.
- `RCLONE_7ZIP_LIB` overrides the library path; it is read in `lib7zip.go` and assigned to `sz.LibraryPath`.

Until a matched library pair is available, extraction falls back to the previous engine. That fallback is deliberate and keeps existing tests green.

## 5. Architecture that the engine must satisfy

### 5.1 Streaming extraction path

`cmd/archive/extract/extract.go:171` `withArchiveStream` opens the remote object through `operations.Open` and wraps it in an accounting reader.

- `newWindowedReader` in `reader.go:68` implements `io.Reader`, `io.ReaderAt`, and `io.Seeker` over ranged remote reads with exact byte-count checks. It does not implement `io.Closer`; `lib7zipReader` adds a no-op `Close` to satisfy `sz.ReaderAtCloser`.
- `entryHandler` in `rar4fallback.go:15` has the shape `func(nameInArchive string, size int64, modTime time.Time, isDir bool, open func() (io.ReadCloser, error)) error`. It sanitizes names, applies filters and skip-existing, creates directories, streams with `operations.Rcat`, verifies byte counts, and removes partial uploads on mismatch.
- `lib7zip.go` creates one `io.Pipe` per item. The pipe writer becomes an `sz.OutStream`; the pipe reader is returned to `entryHandler` from a goroutine. Extraction writes into the pipe, upload reads out of it. This preserves the no-mount, no-disk property.
- Multi-volume uses `collectSevenZipVolumes` from `multivolume.go:172` and a remote-backed `MultiVolumeCallbackFuncs`. No volume is opened from local disk.

### 5.2 Interfaces still to satisfy for the other formats

- `archives.Extraction` / `archives.Extractor`: `Extract(ctx, io.Reader, archives.FileHandler) error`.
- `archives.FileInfo` with `NameInArchive`, `Size()`, `ModTime()`, `IsDir()`, and `Open() (fs.File, error)`.
- Password from `--archive-password` through `applyOptions` at `extract.go:412-461`.
- Encryption diagnostics must replace the `sevenzip.ReadError` check at `extract.go:394-403`.
- `ctx` cancellation and exact-size verification must be honoured.
- `sanitize.Path` protection at `destPath` must be preserved.

## 6. Failure analysis from the previous session

The reported "no clear cause" failures are mostly outside the decoder. Do not assume the engine swap fixes them.

ZIP:

- `extract.go:305` calls `archives.Identify(ctx, "", in)` with no filename, so prefixed or self-extracting ZIPs get `no formats matched` before any decoder runs.
- Encrypted ZIPs are rejected at `extract.go:433-439`, with raw checksum or unsupported-algorithm errors.
- `obj.Size()` is trusted at `reader.go:68-74`; a stale size gives `zip: not a valid zip file` or a CRC error.
- Exact byte-count range checks at `reader.go:154-205` blame range support for padded or short ranges.
- Spanned ZIPs fail generically.
- Partial uploads remain when a decoder error comes back from `Rcat`; cleanup only runs for byte-count mismatch at `extract.go:276-281`.

RAR:

- Most release binaries have no unarr fallback because it needs `cgo && unarr` (`rar4fallback_cgo.go:1`, `.github/workflows/rclonex.yml:126-133`).
- `fs/operations/operations.go:1498` uses `%v` instead of `%w` on the spool path, destroying the error chain. Large files on remotes without `PutStream` never reach the fallback. This is the strongest match for "some files fail".
- The unarr fallback only reads the first volume (`rar4fallback_cgo.go:140-149`).
- Volumes are guessed by name in `multivolume.go:84-110`, not listed. Gaps stop early.
- A missing volume at a boundary becomes `io.EOF`, so extraction can look successful with fewer files.
- Encrypted RAR4 fallback gets no password; RAR5 never reaches the fallback.
- Errors are mostly generic, wrapped as `handling file: X: ...`.

An engine swap changes decoder coverage, encrypted ZIP, RAR5, solid archives, and the dual-decoder split. It does not change error-chain loss, size and range strictness, missing-volume-as-EOF, partial-upload cleanup, or generic wrapping. Those must be fixed separately.

## 7. Remaining work

### Step 1: Obtain a matched library pair

Goal: a `libc7zip` plus `7z.so`/`7z.dll` pair that opens archives without crashing.

- Build NyaMisty libc7zip and its matching 7-Zip engine from the same source tree. The NyaMisty CMake build produced `libc7zip.so` but not `7z.so`; the `7z.so` used in testing came from itchio libc7zip and is probably the wrong version.
- Compare `sz/glue.h` struct layouts with the library's `include/libc7zip.h` before running.
- Verify with `RCLONE_7ZIP_LIB=<path>` and a compiled test binary run from `cmd/archive/extract`, with `7z.so` beside the binary.
- Keep the fallback until the engine passes the 7z fixture tests.

### Step 2: Prove the 7z path end to end

- Run `TestArchiveExtractPassword`, `TestExtractSevenZipMultiVolume`, `TestExtractSevenZipMultiVolumePassword`, and `TestArchiveExtractLargeSevenZipMultiVolume` with the matched libraries.
- Confirm password, encrypted filenames, multi-volume, mtime, byte counts, and error text.
- Add a parity test comparing extracted bytes and mtimes against the bodgit path.

### Step 3: Convert the remaining formats

- Replace the ZIP, RAR, TAR, and ISO paths in `withArchiveStream` with the new engine.
- Carry over existing behaviour: RAR4 recovery, ISO Rock Ridge/Joliet with `.rclonelink`, RAR volume naming schemes, windowed ZIP reads.
- Add a corpus-based parity test per format. Do not ship a format that is not green.
- Keep bodgit as fallback only where the engine cannot run.

### Step 4: Apply the plumbing fixes

- `fs/operations/operations.go:1498`: use `%w`.
- `extract.go:305`: pass the archive filename to `archives.Identify`.
- `reader.go:68-74`: verify `obj.Size()` against served bytes and name the remote on mismatch.
- `multivolume.go`: report missing volumes by name, never as plain EOF.
- `extract.go:269-285`: remove partial uploads on any decoder error.
- Improve errors to name the format, remote, and real cause.

### Step 5: Release engineering

- Enable cgo for all nine release targets in `.github/workflows/rclonex.yml`.
- Add MinGW for Windows, a macOS toolchain, NDK for Termux, and 386 support.
- Decide sidecar packaging. Single-file installs are desirable but not required. If sidecars are used, update the release archives, checksums, install script, and Dockerfile.
- Add licence notices for 7-Zip LGPL 2.1 plus extra terms, lib7zip MPL 2.0, and the LGPL 2.1 UTF conversion code.
- Run `make quicktest`, `make racequicktest` for touched packages, and `golangci-lint run ./...`.

## 8. Where to start in a fresh session

Read this file, then:

1. `cmd/archive/extract/lib7zip.go` and `lib7zip_common.go`.
2. `cmd/archive/extract/extract.go` around the dispatch at line 306.
3. `cmd/archive/extract/reader.go` and `multivolume.go`.
4. `third_party/sevenzip-go/sz/glue.go` and `glue.c`.
5. `/data/home/projects/rclone-7zip/archive/open.go`, `archive/extract.go`, and `rclone_utils/rcatsize.go` for password, multi-volume, and stream handling guidance.

Do not start by converting more formats. Start with the library pair, because the current engine silently falls back and cannot prove any behaviour until it loads.

## 9. Implementation status

Steps 1, 2 and 4 are done on this branch. The 7z path is the default when the sidecars are present and passes password, encrypted-header, encrypted-content, multi-volume, mtime and byte-count parity tests.

Build the matched Linux sidecars with `third_party/sevenzip-go/build-libs.sh [build-dir] [output-dir]`. Place `libc7zip.so`, `7z.so` and `Codecs/Rar.so` beside the `rclonex` binary, or set `RCLONE_7ZIP_LIB` to the absolute path of `libc7zip.so`. The `7z.so` built by p7zip 16.02 must match the library; an apt 25.01 `7z.so` segfaults in `OpenArchiveEx`. The build script adds the missing `item_get_filetime_property` export so modification times match the previous engine. Without the sidecars, extraction still falls back to the previous extractor and keeps working.

An independent review found two regressions in the Step 4 fixes, both fixed and covered by tests: `uploadEntry` no longer deletes a pre-existing destination object when `Rcat` fails without returning an object, and `archives.Identify` now sniffs content before falling back to the filename so a misnamed archive is still decoded correctly. Encryption diagnostics were left as they were because the library reports a wrong password as `ErrNotSupportedArchive` and encrypted-content failures as short entries; the tests require the `--archive-password` hint in both cases.

Step 3 is blocked, not skipped. The p7zip 16.02 `7z.so` registers only the 7z handler: opening generated ZIP and TAR streams through the same binding fails with `EOF` (reproduced with a standalone Go probe using the matched library pair; the itchio release library segfaults in `OpenArchiveEx`). Converting those formats, plus RAR and ISO, would lose verified behaviour, so they remain on the previous extractor until a multi-format plugin is available and green in a parity suite.

Step 5 is partial. CI builds and packages the sidecars for `linux/amd64` and runs the engine tests in a dedicated job. `third_party/sevenzip-go/NOTICE.md` records the 7-Zip LGPL 2.1 plus extra terms, lib7zip MPL 2.0 and the LGPL 2.1 UTF conversion code. The other release targets keep their current cgo settings; cross-compiled lib7zip libraries for those targets are not in place.
