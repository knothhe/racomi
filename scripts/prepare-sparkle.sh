#!/bin/sh
# Pinned upstream binary distribution. No signing keys are read here.
set -eu
cd "$(dirname "$0")/.."
version=$(node -p 'require("./build/updates.json").sparkleVersion')
checksum=$(node -p 'require("./build/updates.json").sparkleSHA256')
destination="$PWD/.cache/sparkle"
if [ -f "$destination/.version" ] && [ "$(cat "$destination/.version")" = "$version:$checksum" ] && [ -x "$destination/bin/sign_update" ]; then exit 0; fi
mkdir -p .cache
stage=$(mktemp -d "$PWD/.cache/.sparkle.XXXXXX")
trap 'rm -rf "$stage"' EXIT
curl --fail --location --retry 2 --proto '=https' "https://github.com/sparkle-project/Sparkle/releases/download/$version/Sparkle-$version.tar.xz" -o "$stage/archive.tar.xz"
printf '%s  %s\n' "$checksum" "$stage/archive.tar.xz" | shasum -a 256 -c -
mkdir "$stage/distribution"
tar -xf "$stage/archive.tar.xz" -C "$stage/distribution"
test -d "$stage/distribution/Sparkle.framework"
printf '%s\n' "$version:$checksum" > "$stage/distribution/.version"
rm -rf "$destination"
mv "$stage/distribution" "$destination"
