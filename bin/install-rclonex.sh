#!/usr/bin/env bash

# rclonex install script for Linux, macOS and Termux.
# Installs the latest rclonex release from https://github.com/Nerqiva/rclonex/releases
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Nerqiva/rclonex/main/bin/install-rclonex.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/Nerqiva/rclonex/main/bin/install-rclonex.sh | sudo bash -s beta
#
# On Termux, no sudo is needed:
#   curl -fsSL https://github.com/Nerqiva/rclonex/releases/latest/download/install.sh | bash
#
# RCLONEX_RELEASE_BASE overrides the download base (default
# https://github.com/Nerqiva/rclonex/releases/download), e.g. for mirrors or tests.
# RCLONEX_RELEASE_TAG installs that tag without querying the GitHub API.
# RCLONEX_INSTALL_DIR installs into that directory instead of /usr/local/bin
# (or $PREFIX/bin on Termux), and checks its rclonex for the current version.
# The directory must already exist.
#
# The script is also attached to each release as "install.sh", so
#   curl -fsSL https://github.com/Nerqiva/rclonex/releases/latest/download/install.sh | sudo bash
# works for the latest stable release.

# error codes
# 0 - exited without problems
# 1 - parameters not supported or some unexpected error occurred
# 2 - OS or machine type not supported by this script
# 3 - installed version of rclonex is up to date
# 4 - required tools are not available

set -e

REPO="Nerqiva/rclonex"
install_dir=${RCLONEX_INSTALL_DIR:-}

usage() { echo "Usage: sudo -v ; curl -fsSL https://raw.githubusercontent.com/${REPO}/main/bin/install-rclonex.sh | sudo bash [-s beta]" 1>&2; exit 1; }

if [ -n "$1" ] && [ "$1" != "beta" ]; then
    usage
fi

export XDG_CONFIG_HOME=config

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t 'rclonex-install.XXXXXXXXXX')
trap 'cd / && rm -rf "$tmp_dir"' EXIT
cd "$tmp_dir"

for tool in curl tar grep cut awk head cp; do
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
    echo 'No sha256 tool found. Please install one and try again.'
    exit 4
fi

if [ -n "${RCLONEX_RELEASE_TAG:-}" ]; then
    target_tag=$RCLONEX_RELEASE_TAG
else
    releases_api="https://api.github.com/repos/${REPO}/releases?per_page=100"
    releases_json=$(curl -fsSL "$releases_api" 2>errors) || {
        echo "Failed to query $releases_api"
        exit 1
    }
    target_tag=$(printf '%s' "$releases_json" | grep -o '"tag_name":[^,]*' | cut -d'"' -f4 | grep -m1 '^v.*-')
fi
if [ -z "$target_tag" ]; then
    echo 'Failed to parse the release tag from the GitHub API response.'
    exit 1
fi

current_bin=rclonex
if [ -n "$install_dir" ] && [ -x "$install_dir/rclonex" ]; then
    current_bin="$install_dir/rclonex"
fi
installed_version=$("$current_bin" version 2>/dev/null | head -n 1 | awk '{print $NF}')
if [ "$installed_version" = "$target_tag" ]; then
    if [ -z "$install_dir" ] || { [ -f "$install_dir/libc7zip.so" ] && [ -f "$install_dir/7z.so" ] && [ -d "$install_dir/Codecs" ]; }; then
        printf "\nThe latest prerelease version of rclonex %s is already installed.\n\n" "$installed_version"
        exit 3
    fi
fi

if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ] && command -v pkg >/dev/null 2>&1; then
    is_termux=1
    OS='termux'
else
    is_termux=
    OS="$(uname)"
fi
case $OS in
  Linux)
    OS='linux'
    ;;
  Darwin)
    OS='darwin'
    ;;
  termux)
    ;;
  *)
    echo "OS not supported: $OS - download the archive for your platform from https://github.com/${REPO}/releases/tag/${target_tag}"
    exit 2
    ;;
esac

OS_type="$(uname -m)"
if [ -n "$is_termux" ]; then
    case "$OS_type" in
      aarch64|arm64)
        OS_type='arm64'
        ;;
      *)
        echo "Machine type not supported: $OS_type - the Termux build supports aarch64/arm64 only."
        exit 2
        ;;
    esac
else
    case "$OS_type" in
      x86_64|amd64)
        OS_type='amd64'
        ;;
      i?86|x86)
        OS_type='386'
        ;;
      aarch64|arm64)
        OS_type='arm64'
        ;;
      *)
        echo "Machine type not supported: $OS_type - download the archive for your platform from https://github.com/${REPO}/releases/tag/${target_tag}"
        exit 2
        ;;
    esac
fi

if [ "$OS" = 'darwin' ] && [ "$OS_type" = '386' ]; then
    echo "No darwin/386 build exists - only amd64 and arm64 builds for macOS."
    exit 2
fi

download_base="${RCLONEX_RELEASE_BASE:-https://github.com/${REPO}/releases/download}/${target_tag}"
archive="rclonex_${OS}_${OS_type}.tar.gz"
echo "Downloading rclonex ${target_tag} for ${OS}/${OS_type}..."
curl -fLO "${download_base}/${archive}"
curl -fsSL -o checksums.txt "${download_base}/checksums.txt"
expected_hash=$(grep "$archive" checksums.txt | head -n 1 | awk '{print $1}')
if [ -z "$expected_hash" ] || [ "$expected_hash" != "$(sha256_hash "$archive")" ]; then
    printf "\nChecksum verification failed for %s - aborting install.\n\n" "$archive"
    exit 1
fi

tar -xzf "$archive"

if [ -n "$install_dir" ]; then
    install_dest="$install_dir/rclonex"
elif [ -n "$is_termux" ]; then
    install_dest="$PREFIX/bin/rclonex"
else
    install_dest='/usr/local/bin/rclonex'
fi

lib_dir=$(dirname "$install_dest")
if [ -f libc7zip.so ] && [ -f 7z.so ]; then
    for lib in libc7zip.so 7z.so; do
        if ! install -m 0755 "$lib" "$lib_dir/$lib"; then
            printf '\nFailed to install %s beside %s - check that the directory is writable.\n\n' "$lib" "$install_dest"
            exit 1
        fi
    done
    if [ -d Codecs ]; then
        if ! cp -R Codecs "$lib_dir/"; then
            printf '\nFailed to install Codecs beside %s - check that the directory is writable.\n\n' "$install_dest"
            exit 1
        fi
    fi
fi

if ! install -m 0755 rclonex "$install_dest"; then
    if [ -n "$install_dir" ]; then
        echo "Failed to write $install_dest - check that the directory is writable."
    elif [ -n "$is_termux" ]; then
        echo "Failed to write $install_dest - run this script inside Termux."
    else
        echo "Failed to write $install_dest - run this script with sudo (curl ... | sudo bash)"
    fi
    exit 1
fi

version=$("$install_dest" version 2>/dev/null | head -n 1) || true

printf "\n%s has successfully installed.\n" "${version:-rclonex $target_tag}"
printf '\nNow run "rclonex config" for setup.\n\n'
exit 0
