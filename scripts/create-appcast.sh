#!/bin/sh
# Reuse the ordinary ZIP. Sparkle owns archive signing and appcast generation.
set -eu
cd "$(dirname "$0")/.."
version=$1
number=$2
archive="$PWD/bin/releases/racomi-$version-aarch64-macos.zip"
[ -n "${SPARKLE_PRIVATE_KEY:-}" ] || { echo 'SPARKLE_PRIVATE_KEY is required for a stable release' >&2; exit 1; }
[ -f "$archive" ] || { echo 'Missing exact release archive' >&2; exit 1; }
sh scripts/prepare-sparkle.sh
stage=$(mktemp -d "$PWD/.cache/.appcast.XXXXXX")
trap 'rm -rf "$stage"' EXIT
cp "$archive" "$stage/"
release_url="https://github.com/knothhe/racomi/releases/tag/v$version"
printf '%s\n' "$SPARKLE_PRIVATE_KEY" | .cache/sparkle/bin/generate_appcast --ed-key-file - --maximum-deltas 0 --download-url-prefix "https://github.com/knothhe/racomi/releases/download/v$version/" --link "$release_url" --full-release-notes-url "$release_url" "$stage"
printf '%s\n' "$SPARKLE_PRIVATE_KEY" | .cache/sparkle/bin/sign_update --ed-key-file - "$stage/appcast.xml"
node scripts/verify-appcast.mjs "$stage/appcast.xml" "$archive" "$version" "$number"
cp "$stage/appcast.xml" bin/releases/appcast.xml
