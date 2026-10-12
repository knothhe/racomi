#!/bin/sh
set -eu
fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
[ "$#" -eq 2 ] || fail "usage: $0 GO NPM"
[ "$(uname -s)" = Darwin ] || fail 'source builds currently require macOS 13 or newer'
go_command=$1
npm_command=$2
command -v "$go_command" >/dev/null 2>&1 || fail 'Go is missing. Install Go 1.26.8+ from https://go.dev/dl/ (macOS does not include Go).'
command -v "$npm_command" >/dev/null 2>&1 || fail 'npm is missing. Install Node.js 22.12+ with npm from https://nodejs.org/.'
command -v node >/dev/null 2>&1 || fail 'Node.js is missing. Install Node.js 22.12+ from https://nodejs.org/.'
command -v git >/dev/null 2>&1 || fail 'Git is missing. Install Xcode Command Line Tools with: xcode-select --install'
xcrun --find clang >/dev/null 2>&1 || fail 'Xcode Command Line Tools are missing. Run: xcode-select --install'
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); if (major < 22 || (major === 22 && minor < 12)) process.exit(1)' || fail 'Node.js 22.12 or newer is required; check node --version and your PATH.'
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
required_go=$(awk '/^go / {print $2; exit}' "$project_dir/go.mod")
actual_go=$("$go_command" env GOVERSION)
if ! awk -v actual="${actual_go#go}" -v required="$required_go" 'BEGIN {
  split(actual, a, "."); split(required, r, ".");
  for (i = 1; i <= 3; i++) { if (a[i] + 0 > r[i] + 0) exit 0; if (a[i] + 0 < r[i] + 0) exit 1 }
  exit 0
}'; then
  fail "Go $required_go or newer is required (found $actual_go); update Go or set make GO=/path/to/go."
fi
printf 'Build tools ready: %s, Node %s, Xcode Command Line Tools.\n' "$actual_go" "$(node --version)"
