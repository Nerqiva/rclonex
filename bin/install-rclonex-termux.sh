#!/usr/bin/env bash

# rclonex install script for Termux on aarch64. No root required.
# Installs the latest rclonex release from https://github.com/Nerqiva/rclonex/releases
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Nerqiva/rclonex/main/bin/install-rclonex-termux.sh | bash
#   curl -fsSL https://github.com/Nerqiva/rclonex/releases/latest/download/install-termux.sh | bash -s beta
#
# RCLONEX_RELEASE_BASE overrides the download base (default https://github.com/Nerqiva/rclonex/releases/download).
# RCLONEX_RELEASE_TAG installs that tag without querying the GitHub API.
#
# The script is also attached to each release as "install-termux.sh".

# error codes
# 0 - exited without problems
# 1 - parameters not supported or some unexpected error occurred
# 2 - Termux or machine type not supported by this script
# 3 - installed version of rclonex is up to date
# 4 - required tools are not available

set -e

REPO="Nerqiva/rclonex"

usage() { echo "Usage: curl -fsSL https://raw.githubusercontent.com/${REPO}/main/bin/install-rclonex-termux.sh | bash [-s beta]" 1>&2; exit 1; }

# check for beta flag
if [ -n "$1" ] && [ "$1" != "beta" ]; then
    usage
fi

if [ -n "$1" ]; then
    install_beta="beta "
fi

# Termux installs into $PREFIX and never needs root
if [ -z "${PREFIX:-}" ] || [ ! -d "$PREFIX/bin" ] || ! command -v pkg >/dev/null 2>&1; then
    echo 'Run this script inside Termux: $PREFIX/bin must exist and pkg must be on PATH.' >&2
    exit 2
fi

case "$(uname -m)" in
    aarch64|arm64) ;;
    *)
        echo "Termux installer supports aarch64/arm64 only, not $(uname -m)." >&2
        exit 2
        ;;
esac

# Keep rclone's config inside the Termux home
export XDG_CONFIG_HOME=config

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t 'rclonex-install.XXXXXXXXXX')
trap 'cd / && rm -rf "$tmp_dir"' EXIT
cd "$tmp_dir"

# check required tools
for tool in curl tar grep cut awk head install; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf "\nRequired tool '%s' not found. Please install it and try again.\n\n" "$tool"
        exit 4
    fi
done

# Termux may ship no coreutils, so accept the common sha256 implementations
if command -v sha256sum >/dev/null 2>&1; then
    sha256_hash() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    sha256_hash() { shasum -a 256 "$1" | awk '{print $1}'; }
elif command -v openssl >/dev/null 2>&1; then
    sha256_hash() { openssl dgst -sha256 -r "$1" | awk '{print $1}'; }
elif command -v busybox >/dev/null 2>&1 && busybox sha256sum </dev/null >/dev/null 2>&1; then
    sha256_hash() { busybox sha256sum "$1" | awk '{print $1}'; }
elif command -v toybox >/dev/null 2>&1 && toybox sha256sum </dev/null >/dev/null 2>&1; then
    sha256_hash() { toybox sha256sum "$1" | awk '{print $1}'; }
else
    echo 'No sha256 tool found. Run "pkg install coreutils" and try again.'
    exit 4
fi

# resolve the target release tag
if [ -n "$RCLONEX_RELEASE_TAG" ]; then
    target_tag=$RCLONEX_RELEASE_TAG
else
    # GitHub returns pretty printed JSON, so parse with grep/cut rather than requiring jq
    if [ -n "$install_beta" ]; then
        releases_api="https://api.github.com/repos/${REPO}/releases?per_page=1"
        releases_json=$(curl -fsSL "$releases_api" 2>errors) || {
            echo "Failed to query $releases_api"
            exit 1
        }
    else
        releases_json=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>errors) || {
            # no stable release yet - install the newest pre-release instead
            echo 'No stable release exists yet - installing the latest pre-release instead.'
            install_beta="beta "
            releases_api="https://api.github.com/repos/${REPO}/releases?per_page=1"
            releases_json=$(curl -fsSL "$releases_api" 2>errors) || {
                echo "Failed to query $releases_api"
                exit 1
            }
        }
    fi
    target_tag=$(printf '%s' "$releases_json" | grep -m1 -o '"tag_name":[^,]*' | cut -d'"' -f4)
    if [ -z "$target_tag" ]; then
        echo 'Failed to parse the release tag from the GitHub API response.'
        exit 1
    fi
fi

# check installed version of rclonex to determine if update is necessary
installed_version=$(rclonex version 2>/dev/null | head -n 1 | awk '{print $NF}')
if [ "$installed_version" = "$target_tag" ]; then
    printf "\nThe latest %sversion of rclonex %s is already installed.\n\n" "$install_beta" "$installed_version"
    exit 3
fi

# download and verify
download_base="${RCLONEX_RELEASE_BASE:-https://github.com/${REPO}/releases/download}/${target_tag}"
archive="rclonex_termux_arm64.tar.gz"
echo "Downloading rclonex ${target_tag} for termux/arm64..."
curl -fLO "${download_base}/${archive}"
curl -fsSL -o checksums.txt "${download_base}/checksums.txt"
expected_hash=$(grep "$archive" checksums.txt | head -n 1 | awk '{print $1}')
if [ -z "$expected_hash" ] || [ "$expected_hash" != "$(sha256_hash "$archive")" ]; then
    printf "\nChecksum verification failed for %s - aborting install.\n\n" "$archive"
    exit 1
fi

tar -xzf "$archive"

# install
if ! install -m 0755 rclonex "$PREFIX/bin/rclonex"; then
    echo "Failed to write ${PREFIX}/bin/rclonex - run this script inside Termux."
    exit 1
fi

version=$(rclonex version 2>/dev/null | head -n 1) || true

printf "\n%s has successfully installed.\n" "${version:-rclonex $target_tag}"
printf '\nNow run "rclonex config" for setup.\n\n'
exit 0
