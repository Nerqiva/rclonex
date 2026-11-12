> **rclonex** — a public fork of rclone/rclone (github.com/rclone/rclone) adding streaming remote-to-remote archive extraction to `rclonex archive extract`. Binary name: `rclonex`. See [Fork notes](#rclonex-fork-notes) below. **This is a personal project — it is nowhere near production ready. Use it at your own risk.** All rclone copyright and the MIT license (see [COPYING](COPYING)) are preserved unchanged.

# Rclone

Rclone *("rsync for cloud storage")* is a command-line program to sync files and
directories to and from different cloud storage providers.

## Storage providers

- 1Fichier
- Abrha Storage
- Akamai Netstorage
- Alibaba Cloud (Aliyun) Object Storage System (OSS)
- Amazon S3
- ArvanCloud Object Storage (AOS)
- Autonomys Auto Drive
- Bizfly Cloud Simple Storage
- Backblaze B2
- Box
- Ceph
- China Mobile Ecloud Elastic Object Storage (EOS)
- Citrix ShareFile
- Clever Cloud Cellar
- Cloudflare R2
- Cloudinary
- Cubbit DS3
- DigitalOcean Spaces
- Digi Storage
- dosya.dev
- Dreamhost
- Drime
- Dropbox
- Enterprise File Fabric
- Exaba
- Fastly Object Storage
- Fastmail Files
- FileLu
- Filen
- Files.com
- FlashBlade
- FTP
- GoFile
- Google Cloud Storage
- Google Drive
- Google Photos
- HDFS (Hadoop Distributed Filesystem)
- Hetzner Object Storage
- Hetzner Storage Box
- HiDrive
- Hitachi Content Platform (HCP)
- HTTP
- Huawei Cloud Object Storage Service(OBS)
- Huawei Drive
- iCloud Drive
- ImageKit
- Internet Archive
- Internxt
- Jottacloud
- IBM COS S3
- Impossible Cloud
- Infomaniak Swiss Backup (S3)
- Infomaniak Swiss Backup (Swift)
- Intercolo Object Storage
- IONOS Cloud
- kDrive
- Koofr
- Leviia Object Storage
- Liara Object Storage
- Linkbox
- Linode Object Storage
- Magalu Object Storage
- Mail.ru Cloud
- Memset Memstore
- MEGA
- MEGA S4 Object Storage
- Memory
- Microsoft Azure Blob Storage
- Microsoft Azure Files Storage
- Microsoft OneDrive
- Minio
- Neon Object Storage
- Nextcloud
- Blomp Cloud Storage
- OpenDrive
- OpenStack Swift
- Oracle Cloud Storage
- Oracle Object Storage
- Outscale
- OVHcloud Object Storage (Swift)
- OVHcloud Object Storage (S3-compatible)
- ownCloud
- pCloud
- Petabox
- PikPak
- Pixeldrain
- premiumize.me
- put.io
- Proton Drive
- QingStor
- Qiniu Cloud Object Storage (Kodo)
- Rabata Cloud Storage
- Quatrix
- Rackspace Cloud Files
- RackCorp Object Storage
- RelAix.Storage
- rsync.net
- Scaleway
- Scality (RING / ARTESCA)
- Seafile
- Seagate Lyve Cloud
- SeaweedFS
- Selectel Object Storage
- Servercore Object Storage
- SFTP
- Shade
- SMB / CIFS
- Spectra Logic
- Storj
- SugarSync
- Synology C2 Object Storage
- Tencent Cloud Object Storage (COS)
- Uloz.to
- US3 Object Storage
- Wasabi
- WebDAV
- Yandex Disk
- Zadara Object Storage
- Zero Services (ZERO-Z3)
- Zoho WorkDrive
- Zata.ai
- The local filesystem

Please see the full list of all storage providers and their features in the upstream rclone documentation (rclone.org/overview).

### Virtual storage providers

These backends adapt or modify other storage providers

- Alias: rename existing remotes
- Archive: read archive files
- Cache: cache remotes (DEPRECATED)
- Chunker: split large files
- Combine: combine multiple remotes into a directory tree
- Compress: compress files
- Crypt: encrypt files
- Hasher: hash files
- Union: join multiple remotes to work together

## Features

- MD5/SHA-1 hashes checked at all times for file integrity
- Timestamps preserved on files
- Partial syncs supported on a whole file basis
- Copy mode to just copy new/changed
  files
- Sync (one way) mode to make a directory
  identical
- Bisync (two way) to keep two directories in sync
  bidirectionally
- Check mode to check for file hash
  equality
- Can sync to and from network, e.g. two different cloud accounts
- Optional large file chunking (Chunker)
- Optional transparent compression (Compress)
- Optional encryption (Crypt)
- Optional FUSE mount (rclonex mount)
- Multi-threaded downloads to local disk
- Can serve local or remote files
  over HTTP/WebDAV/FTP/SFTP/DLNA

## Downloads

- rclonex (this fork, Linux/Windows/macOS binaries + checksums): [GitHub Releases](https://github.com/Nerqiva/rclonex/releases) — see [rclonex fork notes](#rclonex-fork-notes) for the download and install instructions.
- Upstream rclone (not this fork): rclone.org/downloads

## License

This is free software under the terms of the MIT license (check the
[COPYING file](/COPYING) included in this package).

# rclonex fork notes

> **This is a personal project. It is nowhere near production ready — use it at your own risk.**

`rclonex` is a public fork of rclone/rclone. The only
functional change is an extended `rclonex archive extract` command supporting streaming
remote-to-remote archive extraction (ZIP, TAR, TAR.GZ, RAR, RAR5, 7z, ISO9660, including
multi-volume archives where technically possible). Extraction streams directly from the
source remote to the destination remote — complete archives are never downloaded locally.

- **Module path:** `github.com/rclone/rclone` (kept so upstream rebases stay a fast-forward merge).
- **Binary name:** `rclonex` (built with `go build -o rclonex` / `make`).
- **Versioning:** Calver, `vYYYY.MM.MICRO-alpha.N` (see `VERSION`). Each release is cut from a
  dedicated `release/<tag>` branch; `main` tracks upstream `master` plus rclonex changes.
- **Rebasing onto upstream:** `git fetch upstream && git rebase upstream/master main`, then run
  `go test -short ./cmd/archive/... ./fs/... ./lib/...`. All rclonex-specific code lives in
  `cmd/archive/extract/` (new files), `.github/workflows/rclonex.yml`, and docs; upstream files
  are touched minimally to keep rebases mechanical.
- **Issues and releases:** releases are tagged on release branches and built by the
  `rclonex` GitHub Actions workflow; the releases page holds the binaries.

## Quick install

```console
curl -fsSL https://raw.githubusercontent.com/Nerqiva/rclonex/main/bin/install-rclonex.sh | sudo bash
```

The script detects your OS/arch, downloads the latest archive (the latest pre-release when no
stable release exists yet), verifies its checksum against `checksums.txt` and installs `rclonex`
to `/usr/local/bin` — re-running it won't re-download an up-to-date binary. `install.sh` is also
attached to every release. Linux and macOS only; on Windows download the `.zip` from the
[releases page](https://github.com/Nerqiva/rclonex/releases).

Or build from source (Go 1.26+):

```console
git clone https://github.com/Nerqiva/rclonex.git
cd rclonex
go build -o rclonex .        # or: make
sudo install -m 0755 rclonex /usr/local/bin/rclonex
rclonex version
```

**Note:** `go install github.com/rclone/rclone@latest` installs *upstream* rclone, not rclonex — the Go module path is deliberately unchanged so upstream rebases stay easy. Build from the rclonex clone as shown above, or download a release binary.

Releases are cut from `release/<tag>` branches by the `rclonex` GitHub Actions workflow. Tags with a suffix (e.g. `-alpha.N`) are published as pre-releases; plain CalVer tags are full releases and back `releases/latest`. Upstream rclone releases (rclone.org) do not include rclonex.

## What rclonex changes: `archive extract`

`rclonex archive extract` is extended to stream archives **directly from a source remote to a destination remote** — no mounting, no local staging, no full-archive memory buffer. Supported formats:

| Format | Reads | Notes |
|--------|-------|-------|
| ZIP | random access via sliding-window ranged reads (`--archive-window-size`) | per-entry CRC32 verified |
| TAR / TAR.GZ | single pass, streamed | tar header checksum verified |
| RAR / RAR5 | streamed (solid or not) | encrypted via `--archive-password` |
| 7z | random access via sliding-window ranged reads | encrypted via `--archive-password` |
| Multi-volume RAR (`.part1.rar`… and old `.r00`… naming) | volumes streamed in order | encrypted multi-volume supported |
| Split 7z (`.7z.001`, `.7z.002`, …) | concatenated verified ranged reads | volumes must be contiguous from `.001` |
| ISO9660 (`.iso`) | random access via sliding-window ranged reads (`--archive-window-size`) | Rock Ridge and Joliet extensions supported; the format has no checksums, per-entry byte counts are verified |

Spanned/multi-disk ZIPs and multi-part TAR archives are not supported (the upstream decoders don't support them either) — they fail with an explicit error.

### Usage

```console
# extract a ZIP from a remote to a remote (or a local path)
rclonex archive extract remote:archive.zip dest:extracted

# multi-volume RAR (point at the first volume)
rclonex archive extract remote:mv.part1.rar dest:extracted
rclonex archive extract remote:oldname.rar dest:extracted   # old .r00 naming

# split 7z (point at the .001 file)
rclonex archive extract remote:split.7z.001 dest:extracted

# encrypted RAR / 7z (password applies to multi-volume sets too)
rclonex archive extract --archive-password 's3cret' remote:enc.rar dest:extracted

# extract only some files
rclonex archive extract --include "docs/**" remote:archive.zip dest:extracted

# shrink the sliding window (or disable caching entirely)
rclonex archive extract --archive-window-size 4194304 remote:archive.zip dest:extracted
rclonex archive extract --archive-window-size 0 remote:archive.zip dest:extracted

# keep going when one entry fails (e.g. corrupt entry inside an archive)
rclonex archive extract --archive-continue-on-error remote:broken.tar dest:extracted
```

### Flags added by rclonex

| Flag | Default | Meaning |
|------|---------|---------|
| `--archive-password` | empty | Password for encrypted RAR and 7z archives (applies to multi-volume sets too) |
| `--archive-continue-on-error` | `false` | Continue extraction when an entry fails, instead of aborting |
| `--archive-window-size` | `16777216` (16 MiB) | Sliding-window cache size in bytes for random-access formats (ZIP, 7z, ISO9660); `0` disables caching — every read performs a direct ranged fetch of exactly the requested span |

### Guarantees and limits

- **No local staging, ever.** Random-access formats are read through a sliding window of ranged reads; single-pass formats are streamed. There is no code path that downloads the archive to local disk first.
- **Verified ranged reads.** For cached reads the remote is required to return exactly the requested byte count — a remote that ignores the `Range` header fails with an explicit error (`remote does not appear to support range reads`) instead of silently serving wrong data.
- **Integrity.** Decoder checksums (ZIP CRC32, RAR CRC, 7z CRC, tar header checksum) plus a per-entry extracted-byte-count cross-check; partial files are removed on mismatch. ISO9660 has no checksums — entries are verified by byte count only.
- **Progress.** Transfers from the source remote and per-file destination transfers are reported as usual; `--stats`/`--progress` work unchanged. `--bwlimit` and `--low-level-retries` flow through unchanged.
- Archive entry names are sanitized against path traversal (Zip Slip) exactly as upstream does.

See `rclonex archive extract --help` (or the upstream archive extract docs) for the full option list; upstream-compatible flags such as `--include`/`--exclude` work as documented there.

## Credits and license

`rclonex` is a fork of rclone/rclone by Nick Craig-Wood and the rclone contributors. **All rclone copyright belongs to The rclone contributors**; rclone is released under the [MIT license](COPYING) and that license and copyright notice are preserved unchanged in this repository.

The rclonex changes (the archive extract work described above) are © 2026 the rclonex contributors and are released under the same MIT license.

`rclonex` is not affiliated with, endorsed by, or a release of the rclone project; it is an independent fork. Report rclonex-specific issues to [Nerqiva/rclonex](https://github.com/Nerqiva/rclonex/issues); report issues with unrelated rclone functionality to upstream rclone.

With thanks to Nick Craig-Wood, the rclone maintainers, and the many rclone contributors and packagers whose work rclonex is built on.
