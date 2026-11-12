> **rclonex** — a public fork of [rclone/rclone](https://github.com/rclone/rclone) adding streaming remote-to-remote archive extraction to `rclone archive extract`. Binary name: `rclonex`. See [Fork notes](#rclonex-fork-notes) below. **This is a personal project — it is nowhere near production ready. Use it at your own risk.** All rclone copyright and the MIT license (see [COPYING](COPYING)) are preserved unchanged.

<!-- markdownlint-disable-next-line first-line-heading no-inline-html -->
[<img src="https://rclone.org/img/logo_on_light__horizontal_color.svg" width="50%" alt="rclone logo">](https://rclone.org/#gh-light-mode-only)
<!-- markdownlint-disable-next-line no-inline-html -->
[<img src="https://rclone.org/img/logo_on_dark__horizontal_color.svg" width="50%" alt="rclone logo">](https://rclone.org/#gh-dark-mode-only)

[Website](https://rclone.org) |
[Documentation](https://rclone.org/docs/) |
[Download](https://rclone.org/downloads/) |
[Contributing](CONTRIBUTING.md) |
[Changelog](https://rclone.org/changelog/) |
[Installation](https://rclone.org/install/) |
[Forum](https://forum.rclone.org/)

[![Build Status](https://github.com/rclone/rclone/workflows/build/badge.svg)](https://github.com/rclone/rclone/actions?query=workflow%3Abuild)
[![Go Report Card](https://goreportcard.com/badge/github.com/rclone/rclone)](https://goreportcard.com/report/github.com/rclone/rclone)
[![GoDoc](https://godoc.org/github.com/rclone/rclone?status.svg)](https://godoc.org/github.com/rclone/rclone)
[![Docker Pulls](https://img.shields.io/docker/pulls/rclone/rclone)](https://hub.docker.com/r/rclone/rclone)

# Rclone

Rclone *("rsync for cloud storage")* is a command-line program to sync files and
directories to and from different cloud storage providers.

## Storage providers

- 1Fichier [:page_facing_up:](https://rclone.org/fichier/)
- Abrha Storage [:page_facing_up:](https://rclone.org/s3/#abrhastorage)
- Akamai Netstorage [:page_facing_up:](https://rclone.org/netstorage/)
- Alibaba Cloud (Aliyun) Object Storage System (OSS) [:page_facing_up:](https://rclone.org/s3/#alibaba-oss)
- Amazon S3 [:page_facing_up:](https://rclone.org/s3/)
- ArvanCloud Object Storage (AOS) [:page_facing_up:](https://rclone.org/s3/#arvan-cloud-object-storage-aos)
- Autonomys Auto Drive [:page_facing_up:](https://rclone.org/s3/#autonomys)
- Bizfly Cloud Simple Storage [:page_facing_up:](https://rclone.org/s3/#bizflycloud)
- Backblaze B2 [:page_facing_up:](https://rclone.org/b2/)
- Box [:page_facing_up:](https://rclone.org/box/)
- Ceph [:page_facing_up:](https://rclone.org/s3/#ceph)
- China Mobile Ecloud Elastic Object Storage (EOS) [:page_facing_up:](https://rclone.org/s3/#china-mobile-ecloud-eos)
- Citrix ShareFile [:page_facing_up:](https://rclone.org/sharefile/)
- Clever Cloud Cellar [:page_facing_up:](https://rclone.org/s3/#clever-cloud-cellar)
- Cloudflare R2 [:page_facing_up:](https://rclone.org/s3/#cloudflare-r2)
- Cloudinary [:page_facing_up:](https://rclone.org/cloudinary/)
- Cubbit DS3 [:page_facing_up:](https://rclone.org/s3/#Cubbit)
- DigitalOcean Spaces [:page_facing_up:](https://rclone.org/s3/#digitalocean-spaces)
- Digi Storage [:page_facing_up:](https://rclone.org/koofr/#digi-storage)
- dosya.dev [:page_facing_up:](https://rclone.org/dosya/)
- Dreamhost [:page_facing_up:](https://rclone.org/s3/#dreamhost)
- Drime [:page_facing_up:](https://rclone.org/s3/#drime)
- Dropbox [:page_facing_up:](https://rclone.org/dropbox/)
- Enterprise File Fabric [:page_facing_up:](https://rclone.org/filefabric/)
- Exaba [:page_facing_up:](https://rclone.org/s3/#exaba)
- Fastly Object Storage [:page_facing_up:](https://rclone.org/s3/#fastly)
- Fastmail Files [:page_facing_up:](https://rclone.org/webdav/#fastmail-files)
- FileLu [:page_facing_up:](https://rclone.org/filelu/)
- Filen [:page_facing_up:](https://rclone.org/filen/)
- Files.com [:page_facing_up:](https://rclone.org/filescom/)
- FlashBlade [:page_facing_up:](https://rclone.org/s3/#pure-storage-flashblade)
- FTP [:page_facing_up:](https://rclone.org/ftp/)
- GoFile [:page_facing_up:](https://rclone.org/gofile/)
- Google Cloud Storage [:page_facing_up:](https://rclone.org/googlecloudstorage/)
- Google Drive [:page_facing_up:](https://rclone.org/drive/)
- Google Photos [:page_facing_up:](https://rclone.org/googlephotos/)
- HDFS (Hadoop Distributed Filesystem) [:page_facing_up:](https://rclone.org/hdfs/)
- Hetzner Object Storage [:page_facing_up:](https://rclone.org/s3/#hetzner)
- Hetzner Storage Box [:page_facing_up:](https://rclone.org/sftp/#hetzner-storage-box)
- HiDrive [:page_facing_up:](https://rclone.org/hidrive/)
- Hitachi Content Platform (HCP) [:page_facing_up:](https://rclone.org/s3/#hcp)
- HTTP [:page_facing_up:](https://rclone.org/http/)
- Huawei Cloud Object Storage Service(OBS) [:page_facing_up:](https://rclone.org/s3/#huawei-obs)
- Huawei Drive [:page_facing_up:](https://rclone.org/huaweidrive/)
- iCloud Drive [:page_facing_up:](https://rclone.org/iclouddrive/)
- ImageKit [:page_facing_up:](https://rclone.org/imagekit/)
- Internet Archive [:page_facing_up:](https://rclone.org/internetarchive/)
- Internxt [:page_facing_up:](https://rclone.org/internxt/)
- Jottacloud [:page_facing_up:](https://rclone.org/jottacloud/)
- IBM COS S3 [:page_facing_up:](https://rclone.org/s3/#ibm-cos-s3)
- Impossible Cloud [:page_facing_up:](https://rclone.org/s3/#impossible-cloud)
- Infomaniak Swiss Backup (S3) [:page_facing_up:](https://rclone.org/s3/#infomaniak)
- Infomaniak Swiss Backup (Swift) [:page_facing_up:](https://rclone.org/swift/)
- Intercolo Object Storage [:page_facing_up:](https://rclone.org/s3/#intercolo)
- IONOS Cloud [:page_facing_up:](https://rclone.org/s3/#ionos)
- kDrive [:page_facing_up:](https://rclone.org/kdrive/)
- Koofr [:page_facing_up:](https://rclone.org/koofr/)
- Leviia Object Storage [:page_facing_up:](https://rclone.org/s3/#leviia)
- Liara Object Storage [:page_facing_up:](https://rclone.org/s3/#liara-object-storage)
- Linkbox [:page_facing_up:](https://rclone.org/linkbox)
- Linode Object Storage [:page_facing_up:](https://rclone.org/s3/#linode)
- Magalu Object Storage [:page_facing_up:](https://rclone.org/s3/#magalu)
- Mail.ru Cloud [:page_facing_up:](https://rclone.org/mailru/)
- Memset Memstore [:page_facing_up:](https://rclone.org/swift/)
- MEGA [:page_facing_up:](https://rclone.org/mega/)
- MEGA S4 Object Storage [:page_facing_up:](https://rclone.org/s3/#mega)
- Memory [:page_facing_up:](https://rclone.org/memory/)
- Microsoft Azure Blob Storage [:page_facing_up:](https://rclone.org/azureblob/)
- Microsoft Azure Files Storage [:page_facing_up:](https://rclone.org/azurefiles/)
- Microsoft OneDrive [:page_facing_up:](https://rclone.org/onedrive/)
- Minio [:page_facing_up:](https://rclone.org/s3/#minio)
- Neon Object Storage [:page_facing_up:](https://rclone.org/s3/#neon)
- Nextcloud [:page_facing_up:](https://rclone.org/webdav/#nextcloud)
- Blomp Cloud Storage [:page_facing_up:](https://rclone.org/swift/)
- OpenDrive [:page_facing_up:](https://rclone.org/opendrive/)
- OpenStack Swift [:page_facing_up:](https://rclone.org/swift/)
- Oracle Cloud Storage [:page_facing_up:](https://rclone.org/swift/)
- Oracle Object Storage [:page_facing_up:](https://rclone.org/oracleobjectstorage/)
- Outscale [:page_facing_up:](https://rclone.org/s3/#outscale)
- OVHcloud Object Storage (Swift) [:page_facing_up:](https://rclone.org/swift/)
- OVHcloud Object Storage (S3-compatible) [:page_facing_up:](https://rclone.org/s3/#ovhcloud)
- ownCloud [:page_facing_up:](https://rclone.org/webdav/#owncloud)
- pCloud [:page_facing_up:](https://rclone.org/pcloud/)
- Petabox [:page_facing_up:](https://rclone.org/s3/#petabox)
- PikPak [:page_facing_up:](https://rclone.org/pikpak/)
- Pixeldrain [:page_facing_up:](https://rclone.org/pixeldrain/)
- premiumize.me [:page_facing_up:](https://rclone.org/premiumizeme/)
- put.io [:page_facing_up:](https://rclone.org/putio/)
- Proton Drive [:page_facing_up:](https://rclone.org/protondrive/)
- QingStor [:page_facing_up:](https://rclone.org/qingstor/)
- Qiniu Cloud Object Storage (Kodo) [:page_facing_up:](https://rclone.org/s3/#qiniu)
- Rabata Cloud Storage [:page_facing_up:](https://rclone.org/s3/#Rabata)
- Quatrix [:page_facing_up:](https://rclone.org/quatrix/)
- Rackspace Cloud Files [:page_facing_up:](https://rclone.org/swift/)
- RackCorp Object Storage [:page_facing_up:](https://rclone.org/s3/#RackCorp)
- RelAix.Storage [:page_facing_up:](https://rclone.org/s3/#relaix)
- rsync.net [:page_facing_up:](https://rclone.org/sftp/#rsync-net)
- Scaleway [:page_facing_up:](https://rclone.org/s3/#scaleway)
- Scality (RING / ARTESCA) [:page_facing_up:](https://rclone.org/s3/#scality)
- Seafile [:page_facing_up:](https://rclone.org/seafile/)
- Seagate Lyve Cloud [:page_facing_up:](https://rclone.org/s3/#lyve)
- SeaweedFS [:page_facing_up:](https://rclone.org/s3/#seaweedfs)
- Selectel Object Storage [:page_facing_up:](https://rclone.org/s3/#selectel)
- Servercore Object Storage [:page_facing_up:](https://rclone.org/s3/#servercore)
- SFTP [:page_facing_up:](https://rclone.org/sftp/)
- Shade [:page_facing_up:](https://rclone.org/shade/)
- SMB / CIFS [:page_facing_up:](https://rclone.org/smb/)
- Spectra Logic [:page_facing_up:](https://rclone.org/s3/#spectralogic)
- Storj [:page_facing_up:](https://rclone.org/storj/)
- SugarSync [:page_facing_up:](https://rclone.org/sugarsync/)
- Synology C2 Object Storage [:page_facing_up:](https://rclone.org/s3/#synology-c2)
- Tencent Cloud Object Storage (COS) [:page_facing_up:](https://rclone.org/s3/#tencent-cos)
- Uloz.to [:page_facing_up:](https://rclone.org/ulozto/)
- US3 Object Storage [:page_facing_up:](https://rclone.org/s3/#us3)
- Wasabi [:page_facing_up:](https://rclone.org/s3/#wasabi)
- WebDAV [:page_facing_up:](https://rclone.org/webdav/)
- Yandex Disk [:page_facing_up:](https://rclone.org/yandex/)
- Zadara Object Storage [:page_facing_up:](https://rclone.org/s3/#zadara)
- Zero Services (ZERO-Z3) [:page_facing_up:](https://rclone.org/s3/#zero-z3)
- Zoho WorkDrive [:page_facing_up:](https://rclone.org/zoho/)
- Zata.ai [:page_facing_up:](https://rclone.org/s3/#Zata)
- The local filesystem [:page_facing_up:](https://rclone.org/local/)

Please see [the full list of all storage providers and their features](https://rclone.org/overview/)

### Virtual storage providers

These backends adapt or modify other storage providers

- Alias: rename existing remotes [:page_facing_up:](https://rclone.org/alias/)
- Archive: read archive files [:page_facing_up:](https://rclone.org/archive/)
- Cache: cache remotes (DEPRECATED) [:page_facing_up:](https://rclone.org/cache/)
- Chunker: split large files [:page_facing_up:](https://rclone.org/chunker/)
- Combine: combine multiple remotes into a directory tree [:page_facing_up:](https://rclone.org/combine/)
- Compress: compress files [:page_facing_up:](https://rclone.org/compress/)
- Crypt: encrypt files [:page_facing_up:](https://rclone.org/crypt/)
- Hasher: hash files [:page_facing_up:](https://rclone.org/hasher/)
- Union: join multiple remotes to work together [:page_facing_up:](https://rclone.org/union/)

## Features

- MD5/SHA-1 hashes checked at all times for file integrity
- Timestamps preserved on files
- Partial syncs supported on a whole file basis
- [Copy](https://rclone.org/commands/rclone_copy/) mode to just copy new/changed
  files
- [Sync](https://rclone.org/commands/rclone_sync/) (one way) mode to make a directory
  identical
- [Bisync](https://rclone.org/bisync/) (two way) to keep two directories in sync
  bidirectionally
- [Check](https://rclone.org/commands/rclone_check/) mode to check for file hash
  equality
- Can sync to and from network, e.g. two different cloud accounts
- Optional large file chunking ([Chunker](https://rclone.org/chunker/))
- Optional transparent compression ([Compress](https://rclone.org/compress/))
- Optional encryption ([Crypt](https://rclone.org/crypt/))
- Optional FUSE mount ([rclone mount](https://rclone.org/commands/rclone_mount/))
- Multi-threaded downloads to local disk
- Can [serve](https://rclone.org/commands/rclone_serve/) local or remote files
  over HTTP/WebDAV/FTP/SFTP/DLNA

## Installation & documentation

Please see the [rclone website](https://rclone.org/) for:

- [Installation](https://rclone.org/install/)
- [Documentation & configuration](https://rclone.org/docs/)
- [Changelog](https://rclone.org/changelog/)
- [FAQ](https://rclone.org/faq/)
- [Storage providers](https://rclone.org/overview/)
- [Forum](https://forum.rclone.org/)
- ...and more

## Downloads

- rclonex (this fork, Linux/Windows/macOS binaries + checksums): [GitHub Releases](https://github.com/Nerqiva/rclonex/releases) — see [rclonex fork notes](#rclonex-fork-notes) for the download and install instructions.
- Upstream rclone (not this fork): <https://rclone.org/downloads/>

## License

This is free software under the terms of the MIT license (check the
[COPYING file](/COPYING) included in this package).

# rclonex fork notes

> **This is a personal project. It is nowhere near production ready — use it at your own risk.**

`rclonex` is a public fork of [rclone/rclone](https://github.com/rclone/rclone). The only
functional change is an extended `rclone archive extract` command supporting streaming
remote-to-remote archive extraction (ZIP, TAR, TAR.GZ, RAR, RAR5, 7z, including
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

## Downloading and installing rclonex on Linux

Pre-built static binaries are attached to each [GitHub Release](https://github.com/Nerqiva/rclonex/releases).
The current pre-release is `v2026.11.0-alpha.1`:

```console
# pick: amd64, arm64 or 386
TAG=v2026.11.0-alpha.1
ARCH=amd64
curl -LO "https://github.com/Nerqiva/rclonex/releases/download/$TAG/rclonex_${TAG}_linux_${ARCH}.tar.gz"
curl -LO "https://github.com/Nerqiva/rclonex/releases/download/$TAG/checksums.txt"

# verify the checksum (files listed in checksums.txt are named rclonex_<ver>_<os>_<arch>.tar.gz)
grep "linux_${ARCH}.tar.gz" checksums.txt | sha256sum -c -

# install system-wide (needs sudo); use ~/.local/bin instead to avoid sudo
tar -xzf "rclonex_${TAG}_linux_${ARCH}.tar.gz"
sudo install -m 0755 rclonex /usr/local/bin/rclonex

# sanity check
rclonex version
```

Or build from source (Go 1.26+):

```console
git clone https://github.com/Nerqiva/rclonex.git
cd rclonex
go build -o rclonex .        # or: make
sudo install -m 0755 rclonex /usr/local/bin/rclonex
rclonex version
```

**Note:** `go install github.com/rclone/rclone@latest` installs *upstream* rclone, not rclonex — the Go module path is deliberately unchanged so upstream rebases stay easy. Build from the rclonex clone as shown above, or download a release binary.

Releases are pre-releases cut from `release/<tag>` branches by the `rclonex` GitHub Actions workflow. Upstream rclone releases (rclone.org) do not include rclonex.

## What rclonex changes: `archive extract`

`rclone archive extract` is extended to stream archives **directly from a source remote to a destination remote** — no mounting, no local staging, no full-archive memory buffer. Supported formats:

| Format | Reads | Notes |
|--------|-------|-------|
| ZIP | random access via sliding-window ranged reads (`--archive-window-size`) | per-entry CRC32 verified |
| TAR / TAR.GZ | single pass, streamed | tar header checksum verified |
| RAR / RAR5 | streamed (solid or not) | encrypted via `--archive-password` |
| 7z | random access via sliding-window ranged reads | encrypted via `--archive-password` |
| Multi-volume RAR (`.part1.rar`… and old `.r00`… naming) | volumes streamed in order | encrypted multi-volume supported |
| Split 7z (`.7z.001`, `.7z.002`, …) | concatenated verified ranged reads | volumes must be contiguous from `.001` |

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
| `--archive-window-size` | `16777216` (16 MiB) | Sliding-window cache size in bytes for random-access formats (ZIP, 7z); `0` disables caching — every read performs a direct ranged fetch of exactly the requested span |

### Guarantees and limits

- **No local staging, ever.** Random-access formats are read through a sliding window of ranged reads; single-pass formats are streamed. There is no code path that downloads the archive to local disk first.
- **Verified ranged reads.** For cached reads the remote is required to return exactly the requested byte count — a remote that ignores the `Range` header fails with an explicit error (`remote does not appear to support range reads`) instead of silently serving wrong data.
- **Integrity.** Decoder checksums (ZIP CRC32, RAR CRC, 7z CRC, tar header checksum) plus a per-entry extracted-byte-count cross-check; partial files are removed on mismatch.
- **Progress.** Transfers from the source remote and per-file destination transfers are reported as usual; `--stats`/`--progress` work unchanged. `--bwlimit` and `--low-level-retries` flow through unchanged.
- Archive entry names are sanitized against path traversal (Zip Slip) exactly as upstream does.

See `rclonex archive extract --help` (or the upstream [archive extract docs](https://rclone.org/commands/rclone_archive_extract/)) for the full option list; upstream-compatible flags such as `--include`/`--exclude` work as documented there.

## Credits and license

`rclonex` is a fork of [rclone/rclone](https://github.com/rclone/rclone) by Nick Craig-Wood and the rclone contributors. **All rclone copyright belongs to The rclone contributors**; rclone is released under the [MIT license](COPYING) and that license and copyright notice are preserved unchanged in this repository.

The rclonex changes (the archive extract work described above) are © 2026 the rclonex contributors and are released under the same MIT license.

`rclonex` is not affiliated with, endorsed by, or a release of the rclone project; it is an independent fork. Report rclonex-specific issues to [Nerqiva/rclonex](https://github.com/Nerqiva/rclonex/issues); report issues with unrelated rclone functionality to upstream rclone.

With thanks to Nick Craig-Wood, the rclone maintainers, and the many rclone contributors and packagers whose work rclonex is built on.
