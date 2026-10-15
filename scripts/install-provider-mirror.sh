#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
#
# Download a terraform-provider-simplelogin GitHub release into an OpenTofu
# filesystem mirror (packed layout), verifying SHA256 checksums.
#
# Usage:
#   scripts/install-provider-mirror.sh <version> <mirror-dir> [platform...]
#
#   version     release version without "v", e.g. 0.1.0
#   mirror-dir  mirror root, e.g. ./.terraform-mirror
#   platform    os_arch, default: the current platform. Pass several to
#               support multiple machines, e.g. linux_amd64 linux_arm64
#
# Environment:
#   RELEASE_BASE_URL  override the download location (default: the GitHub
#                     release of <version>); file:// URLs work for testing.
#
# Afterwards, point OpenTofu at the mirror with a CLI configuration file
# (see examples/infrastructure/tofurc.example).
set -euo pipefail

if [[ $# -lt 2 ]]; then
  sed -n '4,20p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
fi

version="${1#v}"
mirror="$2"
shift 2

name="terraform-provider-simplelogin"
source_addr="registry.terraform.io/qnimbus/simplelogin"
base_url="${RELEASE_BASE_URL:-https://github.com/qnimbus/${name}/releases/download/v${version}}"

platforms=("$@")
if [[ ${#platforms[@]} -eq 0 ]]; then
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "unsupported architecture $(uname -m)" >&2; exit 1 ;;
  esac
  platforms=("${os}_${arch}")
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
else
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

sums="${name}_${version}_SHA256SUMS"
curl -fsSL -o "$tmp/$sums" "$base_url/$sums"

dest="$mirror/$source_addr"
mkdir -p "$dest"

for platform in "${platforms[@]}"; do
  zip="${name}_${version}_${platform}.zip"
  expected="$(awk -v f="$zip" '$2 == f { print $1 }' "$tmp/$sums")"
  if [[ -z "$expected" ]]; then
    echo "no checksum for $zip in $sums (platform not released?)" >&2
    exit 1
  fi
  curl -fsSL -o "$tmp/$zip" "$base_url/$zip"
  actual="$(sha256 "$tmp/$zip")"
  if [[ "$actual" != "$expected" ]]; then
    echo "checksum mismatch for $zip: expected $expected, got $actual" >&2
    exit 1
  fi
  mv "$tmp/$zip" "$dest/$zip"
  echo "installed $dest/$zip"
done
