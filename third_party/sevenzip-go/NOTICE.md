# lib7zip notices

The cgo binding in this directory is the NyaMisty fork of `github.com/itchio/sevenzip-go` (MIT). `build-libs.sh` downloads NyaMisty/libc7zip (MIT) and p7zip 16.02 (7-Zip LGPL 2.1 plus the 7-Zip extra terms), then builds `libc7zip.so` and the p7zip `7z.so` from the same source tree.

The `linux/amd64` release archive bundles these dynamically linked LGPL components. Full licence texts are in `licenses/`. The corresponding source for the exact versions used is available from https://github.com/NyaMisty/libc7zip (tag v1.8.0) and from the p7zip 16.02 tree that `build-libs.sh` downloads during configuration. You may modify and relink the libraries; `build-libs.sh` reproduces the build used for the release.

The 7-Zip RAR codec has extra terms that restrict its use. It is built for local testing but is not included in release archives.
