# rclonex roadmap and task tracking

rclonex is a private fork of [rclone/rclone](https://github.com/rclone/rclone) whose only
functional change is an extended `rclone archive extract` with streaming remote-to-remote
archive extraction. This file tracks the work items. GitHub issues/PRs cannot be used with the
current access token (403 — the token may create repos and push, but not issues/PRs), so tasks
are tracked here and referenced from commits; once the token is fixed this file will be
superseded by GitHub issues and the same task IDs will be reused.

## Task list

| ID | Task | Status | Commit |
|----|------|--------|--------|
| T1 | Fork bootstrap: private repo, Calver version, binary renamed `rclonex`, CI (`rclonex.yml`), fork notes | done | 66bd982fc |
| T2 | Windowed verified range-read cache for random-access formats (ZIP, 7z); no silent fallback when remote ignores Range | done | 6bc6d9c7e |
| T3 | Extraction hardening: per-entry integrity check (extracted bytes vs declared size, partial file removed on mismatch), `--archive-password`, `--archive-continue-on-error`, `--archive-window-size`, context-scoped options | done | 6bc6d9c7e |
| T4 | Multi-volume RAR/RAR5 streamed from remotes (rardecode `Name`+`FS` over the source remote, new and old volume naming) | done | 81cc2faa5 |
| T5 | Multi-volume 7z (`.7z.001` split volumes) via concatenated verified ranged reads over remote objects | done | 81cc2faa5 |
| T6 | Test fixtures + integration tests for all six formats, encrypted and multi-volume variants, corrupt-archive and range-ignoring remotes | done | 6bc6d9c7e + 81cc2faa5 |
| T7 | Documentation: fork/design notes, updated `archive extract` page, rebase guide, resource/limits discussion | done | this commit |
| T8 | CI: full-suite regression on push, memory-bounded extraction test job, release workflow wiring (Calver tag → release branch → binaries + checksums) | done | 0dad1d9d7 + 8a75e7fe3 + f27ce4043 (green run 43221982203) |
| T9 | First Calver alpha release `v2026.11.0-alpha.1` from `release/v2026.11.0-alpha.1` (release branch per version; the bootstrap pins the same string) | done | release/v2026.11.0-alpha.1 |

Note: the GitHub Actions release job runs on tag push; the GitHub API/token in this environment cannot create issues or PRs (403) and may not be able to create the GitHub release itself — workflow artifacts (binaries + checksums) are always produced; see the release workflow run for the release job result.

## Requirements coverage

- **ZIP, TAR, TAR.GZ, RAR, RAR5, 7z (incl. multi-volume)**: TAR end-to-end verified by tests; ZIP/7z/RAR fixtures committed; multi-volume via T4/T5.
- **Read directly from remotes, no mounting, no local staging**: extraction path never writes the archive to disk; random-access formats use ranged opens; single-pass formats stream.
- **Streamed extraction to destination remotes**: `operations.Rcat` streams the decoded entry to the destination; no full entry buffered (rely on decoder buffering).
- **RAM/storage minimization**: sliding-window cache (default 16 MiB) for random-access formats; hard fetch verification avoids giant spurious buffers; `--archive-window-size 0` available.
- **Random access / solid archives**: ZIP and 7z solid extraction via `io.ReaderAt` + `io.Seeker` over the windowed reader.
- **Progress reporting**: source-archive transfer (bytes downloaded, known total size) + per-file transfers created by `Rcat`; `--stats`/`--progress` work unchanged.
- **Integrity**: decoder CRCs/checksums (ZIP CRC32, RAR CRC, 7z CRC, tar header checksum) + extracted-byte-count cross-check per entry + range-read exactness verification + `Rcat` short-write detection; partial files removed on mismatch. (Per-entry uploaded hashes are whatever the destination rclone does by default.)
- **Error handling**: corrupt archives fail with decoder errors; range-ignoring remotes fail with `errRangeNotSupported`-wrapped messages; missing RAR/7z volumes fail with explicit errors; unsupported spanned ZIP / multi-part TAR are rejected with clear messages (documented).
- **Resource limits**: `--bwlimit` and `--low-level-retries` flow through unchanged; window cache bounds RAM; one fetch in flight (decoders are single-threaded); destination upload honors `--buffer-size` as before.
- **Never silently fall back to downloading locally**: enforced by construction — there is no local-staging code path; a remote that can't serve exactly the requested range fails the extraction.
- **Attribution/license**: `COPYING` unchanged; README fork notes; upstream file modifications kept minimal (`extract.go`, `versiontag.go`, `Makefile`, `README.md`, `VERSION`).

## Known limitations (documented in the command help and fork docs)

- Spanned/multi-disk ZIPs and multi-part TAR archives (`.tar.gz.001`) are not supported (upstream decoders don't support them either) — explicit error, no fallback.
- 7z multi-volume requires the volumes to be named `base.7z.001`, `base.7z.002`, ... contiguous from `.001`.
- RAR multi-volume requires sibling volumes of the first volume to share the directory prefix; decoded with sequential volumes opened via the remote `fs.FS` adapter.
- Writes never happen to the source; destination directories are created as upstream rclone does.

## Release process (T9)

1. Merge current work to `main`.
2. `git checkout -b release/v2026.11.0-alpha.1` from `main` (one release branch per version).
3. Ensure `VERSION` and `fs/versiontag.go` carry the Calver string; tag `v2026.11.0-alpha.1`.
4. Push branch and tag; the `rclonex` workflow builds `linux/windows/darwin` (amd64/arm64/386) binaries named `rclonex`, attaches SHA256 checksums, and creates the (prerelease) GitHub release.
5. Known gap: releases via `gh` CLI may hit the same token permission issue as issues/PRs — if so, binaries/checksums are still produced as workflow artifacts and the release is created manually.
