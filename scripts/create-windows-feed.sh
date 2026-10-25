#!/bin/sh
# Sign the Windows release archive with the Sparkle EdDSA key and write the JSON
# feed the Windows updater reads. Runs on macOS, where Sparkle's tools exist.
set -eu
cd "$(dirname "$0")/.."
version=$1
commit=$2
archive="$PWD/bin/releases/racomi-$version-x86_64-windows.zip"
[ -n "${SPARKLE_PRIVATE_KEY:-}" ] || { echo 'SPARKLE_PRIVATE_KEY is required for a stable release' >&2; exit 1; }
[ -f "$archive" ] || { echo 'Missing exact Windows release archive' >&2; exit 1; }
sh scripts/prepare-sparkle.sh
stage=$(mktemp -d "$PWD/.cache/.windows-feed.XXXXXX")
trap 'rm -rf "$stage"' EXIT
printf '%s\n' "$SPARKLE_PRIVATE_KEY" | .cache/sparkle/bin/sign_update --ed-key-file - "$archive" > "$stage/signature"
node scripts/verify-windows-feed.mjs write "$stage/windows-update.json" "$version" "$commit" bin/releases "$stage/signature"
cp "$stage/windows-update.json" bin/releases/windows-update.json
