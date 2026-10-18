#!/bin/sh
# Installs the pingstack command from its GitHub release.
#
#   curl -fsSL https://pingstack.io/install.sh | sh
#
# PINGSTACK_VERSION=v1.0.0 picks a release (default: the latest).
# PINGSTACK_INSTALL_DIR picks where it goes (default: /usr/local/bin when
# writable, otherwise ~/.local/bin).
set -eu

repo="pingstack-io/pingstack-ping"

fail() {
  echo "pingstack install: $*" >&2
  exit 1
}

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported system $(uname -s); download a build from https://github.com/$repo/releases" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported processor $(uname -m); download a build from https://github.com/$repo/releases" ;;
esac

version="${PINGSTACK_VERSION:-latest}"
if [ "$version" = latest ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$version"
fi

if [ -n "${PINGSTACK_INSTALL_DIR:-}" ]; then
  dir="$PINGSTACK_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi

command -v curl >/dev/null 2>&1 || fail "curl is required"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

archive="pingstack_${os}_${arch}.tar.gz"
echo "Downloading $archive ($version)..."
curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "couldn't download $base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "couldn't download the checksums"

expected="$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$expected" ] || fail "$archive isn't in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$archive" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)"
fi
[ "$actual" = "$expected" ] || fail "checksum mismatch for $archive; not installing"

tar -xzf "$tmp/$archive" -C "$tmp" pingstack
mkdir -p "$dir"
mv "$tmp/pingstack" "$dir/pingstack"
chmod 755 "$dir/pingstack"

echo "Installed $("$dir/pingstack" version) to $dir/pingstack"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH to run it as pingstack." ;;
esac
