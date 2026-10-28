#!/bin/sh
set -eu

fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
[ "$(uname -s)" = Linux ] || fail 'packaging requires Linux'
[ "$#" -eq 2 ] || fail "usage: $0 VERSION GOARCH"
version=${1#v}
architecture=$2
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$' || fail 'VERSION must be X.Y.Z with optional prerelease and build metadata'
case "$architecture" in arm64) archive_arch=aarch64 ;; amd64) archive_arch=x86_64 ;; *) fail "unsupported Linux GOARCH: $architecture" ;; esac
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
channel=${CHANNEL:-dev}
case "$channel" in dev) suffix=-dev; icon=assets/dev-icons/app-icon-dev.png ;; stable|preview) suffix=; icon=assets/appicon.png ;; *) fail 'invalid CHANNEL' ;; esac
binary="$project_dir/bin/$channel/racomi"
[ -x "$binary" ] || fail 'build bin/CHANNEL/racomi first (make dist)'
built_version=$("$binary" --build-info | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).version))')
[ "$built_version" = "$version" ] || fail 'executable version differs from release version; rebuild with make dist'
release_dir="$project_dir/bin/releases"
mkdir -p "$release_dir"
stage=$(mktemp -d "$release_dir/.racomi-package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
trap 'exit 1' HUP INT TERM
name="racomi-$version$suffix-$archive_arch-linux"
mkdir -p "$stage/$name"
cp "$binary" "$stage/$name/racomi"
cp "$project_dir/$icon" "$stage/$name/racomi.png"
cp "$project_dir/build/linux/INSTALL.txt" "$stage/$name/INSTALL.txt"
cp "$project_dir/frontend/public/THIRD_PARTY_NOTICES.txt" "$stage/$name/THIRD_PARTY_NOTICES.txt"
archive_name="$name.tar.gz"
tar -C "$stage" --owner=0 --group=0 --numeric-owner -czf "$stage/$archive_name" "$name"
mv -f "$stage/$archive_name" "$release_dir/$archive_name"
(cd "$release_dir" && sha256sum "$archive_name" > "$archive_name.sha256")
printf 'Release archive: %s\nChecksum: %s.sha256\n' "$release_dir/$archive_name" "$release_dir/$archive_name"
