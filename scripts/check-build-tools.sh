#!/bin/sh
set -eu
fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
[ "$#" -eq 2 ] || fail "usage: $0 GO NPM"
platform=$(uname -s)
case "$platform" in Darwin|Linux) ;; *) fail 'source builds currently require macOS 13 or newer, or Linux' ;; esac
go_command=$1
npm_command=$2
if [ "$platform" = Darwin ]; then go_hint=' (macOS does not include Go)'; git_hint='Install Xcode Command Line Tools with: xcode-select --install'; else go_hint=''; git_hint='Install Git with your distribution package manager.'; fi
command -v "$go_command" >/dev/null 2>&1 || fail "Go is missing. Install Go 1.26.8+ from https://go.dev/dl/$go_hint."
command -v "$npm_command" >/dev/null 2>&1 || fail 'npm is missing. Install Node.js 22.12+ with npm from https://nodejs.org/.'
command -v node >/dev/null 2>&1 || fail 'Node.js is missing. Install Node.js 22.12+ from https://nodejs.org/.'
command -v git >/dev/null 2>&1 || fail "Git is missing. $git_hint"
if [ "$platform" = Darwin ]; then
  xcrun --find clang >/dev/null 2>&1 || fail 'Xcode Command Line Tools are missing. Run: xcode-select --install'
else
  # Wails links GTK and WebKitGTK through cgo; either toolkit generation works.
  pkg_config=${PKG_CONFIG:-pkg-config}
  gtk=${GTK:-3}
  case "$gtk" in
    4) gtk_modules='gtk4 webkitgtk-6.0' ;;
    3) gtk_modules='gtk+-3.0 webkit2gtk-4.1' ;;
    *) fail 'GTK must be 3 or 4' ;;
  esac
  command -v "${CC:-cc}" >/dev/null 2>&1 || fail 'A C compiler is missing. Install gcc (Arch: base-devel; Debian/Ubuntu: build-essential; Fedora: gcc).'
  command -v "$pkg_config" >/dev/null 2>&1 || fail 'pkg-config is missing. Install pkgconf (Arch/Fedora) or pkg-config (Debian/Ubuntu).'
  # shellcheck disable=SC2086
  "$pkg_config" --exists $gtk_modules libsoup-3.0 || fail "GTK $gtk development files are missing ($gtk_modules libsoup-3.0). Install GTK3: Arch webkit2gtk-4.1; Debian/Ubuntu libgtk-3-dev libwebkit2gtk-4.1-dev; Fedora gtk3-devel webkit2gtk4.1-devel. Or GTK4: Arch webkitgtk-6.0; Debian/Ubuntu libgtk-4-dev libwebkitgtk-6.0-dev; Fedora gtk4-devel webkitgtk6.0-devel."
fi
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
if [ "$platform" = Darwin ]; then
  printf 'Build tools ready: %s, Node %s, Xcode Command Line Tools.\n' "$actual_go" "$(node --version)"
else
  printf 'Build tools ready: %s, Node %s, GTK %s with WebKitGTK %s.\n' "$actual_go" "$(node --version)" "$("$pkg_config" --modversion "${gtk_modules% *}")" "$("$pkg_config" --modversion "${gtk_modules#* }")"
fi
