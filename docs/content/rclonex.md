---
title: "rclonex fork notes"
description: "What rclonex adds to rclone: streaming remote-to-remote archive extraction"
versionIntroduced: v2026.11.0-alpha.1
---

# rclonex fork notes

`rclonex` is a public fork of [rclone/rclone](https://github.com/rclone/rclone) — a personal project, nowhere near production ready. Against a
stock rclone it adds exactly one functional change: an **extended `rclone archive extract`**
that streams archives directly from a source remote to a destination remote, without mounting
and without ever staging the complete archive (or the extracted tree) on local disk.

- Binary name: `rclonex` (`make` or `go build -o rclonex`)
- Versioning: Calver `vYYYY.MM.MICRO-alpha.N` (see `VERSION`), one release branch per release
  (`release/<tag>`), tags `v<calver>`
- Module path: kept as `github.com/rclone/rclone` so upstream rebases stay mechanical
- Version string: `fs/versiontag.go` is fork-maintained like upstream does at releases

## What changed, at a glance

| Area | Upstream rclone | rclonex |
|------|-----------------|---------|
| `archive extract` ZIP/7z reads | `archives` decoders fed by one global seeker over `operations.Open` (`Seek+Read` per read — one HTTP range reopen *per ReadAt*) | sliding-window verified range-read cache (`--archive-window-size`, default 16 MiB, `0` disables); a remote that ignores `Range` fails with an explicit error — **never** a silent local download |
| Progress | one transfer for the whole archive | same, plus per-file transfers created by `Rcat` |
| Integrity | decoder CRCs/checksums | same, plus per-entry extracted-byte-count cross-check (partial uploads removed on mismatch) |
| Multi-volume archives | unsupported | RAR/RAR5 (`.partN.rar` and old `.r00` style) via rardecode `Name`+`FS` over the source remote; split 7z (`.001` …) via a concatenated half-ranged reader. Solid archives supported (they're ReaderAt-based) |
| Encrypted archives | no way to pass a password | `--archive-password` (RAR and 7z) |
| Error tolerance | none (abort) | `--archive-continue-on-error` (map to the decoders' `ContinueOnError`) |

## Safety rules kept from upstream

- Archive entry names are sanitized with `sanitize.Path` (path traversal / Zip Slip protection) before being joined to the destination; the archive root entry (`./`) is skipped.
- `--dry-run`, filters (`--include`/`--exclude`), directory creation, modtime preservation, and metadata behavior are unchanged.
- `ArchiveExtract` keeps its signature; only the read path, options plumbing (via context, mirroring the filter config) and progress accounting changed.
- Upstream files touched: `cmd/archive/extract/extract.go`, `fs/versiontag.go`, `Makefile`, `VERSION`, `README.md`, plus the new file `docs/content/rclonex.md` and `.github/workflows/rclonex.yml`. Everything else is new code under `cmd/archive/extract/`.

## Hard failures (by design)

- A remote whose ranged opens return wrong/missing byte counts → extraction fails with an explicit error naming the format/remote; there is **no** code path that downloads the archive locally first, ever.
- Spanned/multi-disk ZIPs and multi-part TAR archives → rejected up front (the upstream decoders don't support them either).
- 7z multi-volume sets are expected as `name.7z.001`, `.002`, … contiguous from `.001`; RAR volumes are resolved relative to the first volume's directory (both naming schemes).
- Missing volumes, corrupt entries (decoder CRC), or short extracted byte counts → clear error; with `--archive-continue-on-error` the decoder logs and continues where the format supports it.

## Rebase guide

```bash
git remote add upstream https://github.com/rclone/rclone.git   # once
git fetch upstream
git rebase upstream/master main
go test -short -timeout 20m ./...
```

rclonex-specific code lives in `cmd/archive/extract/{reader,multivolume}.go` and
`cmd/archive/extract/*_test.go`; upstream-only conflicts should be limited to `extract.go`'s
`ArchiveExtract` and flag registration. Keep the upstream files' logic intact when resolving.

## Resource usage notes

- RAM: one 16 MiB window per in-flight random-access read (decoders are single-threaded), plus decoder buffers (7z solid blocks can be large — inherent to the format/decoder), plus `--buffer-size` for uploads.
- For multi-volume 7z the window cache is per underlying volume and allocated lazily; the total is bounded by (window size × volumes actually read), typically small.
- HTTP transactions on high-latency remotes: the window cache turns thousands of tiny ranged reads into a handful of sequential requests.
- Network: archives download exactly once (no local spool). Post-upload integrity is whatever the destination rclone already does (`--ignore-checksum=false` semantics, hash picked by destination).

## Not included (deliberately)

- No local spool / temp-file fallback — by requirement, unsupported remote capabilities hard-fail instead.
- No changes to any other command, backend, or the rc API.
- No new dependencies (uses `mholt/archives` already vendored by upstream).
