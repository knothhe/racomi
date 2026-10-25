#!/bin/sh
# Sign the Linux and Windows release archives with the Sparkle EdDSA key and
# write the update.json both updaters read. Runs on macOS, where Sparkle's tools exist.
set -eu
cd "$(dirname "$0")/.."
version=$1
commit=$2
[ -n "${SPARKLE_PRIVATE_KEY:-}" ] || { echo 'SPARKLE_PRIVATE_KEY is required for a stable release' >&2; exit 1; }
sh scripts/prepare-sparkle.sh
stage=$(mktemp -d "$PWD/.cache/.update-feed.XXXXXX")
trap 'rm -rf "$stage"' EXIT
for entry in linux-amd64:racomi-$version-x86_64-linux.tar.gz windows-amd64:racomi-$version-x86_64-windows.zip; do
  archive="$PWD/bin/releases/${entry#*:}"
  [ -f "$archive" ] || { echo "Missing exact release archive: ${entry#*:}" >&2; exit 1; }
  printf '%s\n' "$SPARKLE_PRIVATE_KEY" | .cache/sparkle/bin/sign_update --ed-key-file - "$archive" > "$stage/${entry%%:*}.sig"
done
node scripts/update-feed.mjs write "$stage/update.json" "$version" "$commit" bin/releases "$stage"
cp "$stage/update.json" bin/releases/update.json
