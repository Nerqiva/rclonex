#!/usr/bin/env bash

# rclonex install script.
# Installs the latest rclonex release from https://github.com/Nerqiva/rclonex/releases
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Nerqiva/rclonex/main/bin/install-rclonex.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/Nerqiva/rclonex/main/bin/install-rclonex.sh | sudo bash -s beta
#
# The script is also attached to each release as "install.sh", so
#   curl -fsSL https://github.com/Nerqiva/rclonex/releases/latest/download/install.sh | sudo bash
# works for the latest stable release.

# error codes
# 0 - exited without problems
# 1 - parameters not supported or some unexpected error occurred
# 2 - OS not supported by this script
# 3 - installed version of rclonex is up to date
# 4 - required tools are not available

set -e

REPO="Nerqiva/rclonex"

usage() { echo "Usage: sudo -v ; curl -fsSL https://raw.githubusercontent.com/${REPO}/main/bin/install-rclonex.sh | sudo bash [-s beta]" 1>&2; exit 1; }

#check for beta flag
if [ -n "$1" ] && [ "$1" != "beta" ]; then
    usage
fi

if [ -n "$1" ]; then
    install_beta="beta "
fi

# Keep rclone's config out of root's home when run under sudo
export XDG_CONFIG_HOME=config

#create tmp directory and move to it with macOS compatibility fallback
tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t 'rclonex-install.XXXXXXXXXX')
trap 'cd / && rm -rf "$tmp_dir"' EXIT
cd "$tmp_dir"

# check required tools
for tool in curl tar grep cut awk head; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf "\nRequired tool '%s' not found. Please install it and try again.\n\n" "$tool"
        exit 4
    fi
done

# sha256 checker: sha256sum (Linux) or shasum (macOS)
if command -v sha256sum >/dev/null 2>&1; then
    sha256_check() { sha256sum -c -; }
elif command -v shasum >/dev/null 2>&1; then
    sha256_check() { shasum -a 256 -c -; }
else
    echo 'Neither sha256sum nor shasum found. Please install one of them and try again.'
    exit 4
fi

# resolve the target release tag (GitHub returns pretty printed JSON, so parse
# with grep/cut rather than requiring jq)
if [ -n "$install_beta" ]; then
    releases_api="https://api.github.com/repos/${REPO}/releases?per_page=1"
else
    releases_api="https://api.github.com/repos/${REPO}/releases/latest"
fi
if ! releases_json=$(curl -fsSL "$releases_api" 2>errors); then
    printf "\nFailed to query %s\n" "$releases_api"
    if [ -z "$install_beta" ]; then
        echo "No stable release exists yet - run again with '-s beta' to install the latest pre-release."
    fi
    exit 1
fi
target_tag=$(printf '%s' "$releases_json" | grep -m1 -o '"tag_name":[^,]*' | cut -d'"' -f4)
if [ -z "$target_tag" ]; then
    echo 'Failed to parse the release tag from the GitHub API response.'
    exit 1
fi

#check installed version of rclonex to determine if update is necessary
installed_version=$(rclonex version 2>/dev/null | head -n 1 | awk '{print $2}')
if [ "$installed_version" = "$target_tag" ]; then
    printf "\nThe latest %sversion of rclonex %s is already installed.\n\n" "$install_beta" "$installed_version"
    exit 3
fi

#detect the platform
OS="$(uname)"
case $OS in
  Linux)
    OS='linux'
    ;;
  Darwin)
    OS='darwin'
    ;;
  *)
    echo "OS not supported: $OS - download the archive for your platform from https://github.com/${REPO}/releases/tag/${target_tag}"
    exit 2
    ;;
esac

OS_type="$(uname -m)"
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

if [ "$OS" = 'darwin' ] && [ "$OS_type" = '386' ]; then
    echo "No darwin/386 build exists - only amd64 and arm64 builds for macOS."
    exit 2
fi

#download and verify
download_base="https://github.com/${REPO}/releases/download/${target_tag}"
archive="rclonex_${OS}_${OS_type}.tar.gz"
echo "Downloading rclonex ${target_tag} for ${OS}/${OS_type}..."
curl -fLO "${download_base}/${archive}"
curl -fsSL -o checksums.txt "${download_base}/checksums.txt"
if ! grep "${archive}" checksums.txt | sha256_check; then
    printf "\nChecksum verification failed for %s - aborting install.\n\n" "$archive"
    exit 1
fi

tar -xzf "$archive"

#install
if ! install -m 0755 rclonex /usr/local/bin/rclonex; then
    echo 'Failed to write /usr/local/bin/rclonex - run this script with sudo (curl ... | sudo bash)'
    exit 1
fi

#update version variable post install
version=$(rclonex version 2>/dev/null | head -n 1) || true

printf "\n%s has successfully installed.\n" "${version:-rclonex $target_tag}"
printf '\nNow run "rclonex config" for setup.\n\n'
exit 0
