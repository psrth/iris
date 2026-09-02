#!/bin/sh
# Installs iris from its GitHub release.
#
#   curl -fsSL https://iris-tl.dev/install.sh | sh
#
# IRIS_VERSION=v1.0.0 pins a release (default: latest).
# IRIS_INSTALL_DIR=/path overrides the target directory.
set -eu

repo=psrth/iris
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
	linux | darwin) ;;
	*) echo "iris: unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) echo "iris: unsupported architecture: $arch" >&2; exit 1 ;;
esac

version=${IRIS_VERSION:-latest}
if [ "$version" = latest ]; then
	base="https://github.com/$repo/releases/latest/download"
else
	base="https://github.com/$repo/releases/download/$version"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
file="iris_${os}_${arch}.tar.gz"
for f in "$file" checksums.txt; do
	curl -fsL -o "$tmp/$f" "$base/$f" || { echo "iris: no release $version for $os/$arch at $base/$f" >&2; exit 1; }
done

if command -v sha256sum >/dev/null 2>&1; then
	sum=$(sha256sum "$tmp/$file" | cut -d' ' -f1)
else
	sum=$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1)
fi
expected=$(grep " $file\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] && [ "$sum" = "$expected" ] || { echo "iris: checksum mismatch for $file" >&2; exit 1; }
tar -xzf "$tmp/$file" -C "$tmp" iris

dir=${IRIS_INSTALL_DIR:-}
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
install -m 755 "$tmp/iris" "$dir/iris"
echo "installed $("$dir/iris" -version) to $dir/iris"
case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "add $dir to your PATH" ;;
esac
