#!/bin/sh
# Exercise the release installer against local fixture releases and stub
# package managers, never the network, the real HOME or the real system.
set -eu
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || { echo 'Release installer checks skipped on this platform'; exit 0; }
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/racomi-installer-test.XXXXXX")
test_dir=$(CDPATH= cd -P -- "$test_dir" && pwd)
trap 'rm -rf "$test_dir"' EXIT
trap 'exit 1' HUP INT TERM
test_home="$test_dir/home"
bin_dir="$test_home/.local/bin"
desktop="$test_home/.local/share/applications/com.knothhe.racomi.desktop"
icon="$test_home/.local/share/icons/hicolor/512x512/apps/com.knothhe.racomi.png"
data_dir="$test_home/.config/racomi"
releases="$test_dir/releases"
stubs="$test_dir/stubs"
calls="$test_dir/calls.log"
mkdir -p "$data_dir" "$stubs"
printf 'keep user data\n' > "$data_dir/state.json"

release() {
  name="racomi-$1-x86_64-linux"
  stage="$test_dir/stage/$name"
  mkdir -p "$stage" "$releases/download/v$1"
  printf '#!/bin/sh\n[ "$1" = --build-info ] && printf %s\\n %s\n' "'%s'" "'{\"name\":\"racomi\",\"id\":\"${2:-com.knothhe.racomi}\",\"version\":\"$1\"}'" > "$stage/racomi"
  chmod 755 "$stage/racomi"
  printf 'png\n' > "$stage/racomi.png"
  tar -C "$test_dir/stage" -czf "$releases/download/v$1/$name.tar.gz" "$name"
  (cd "$releases/download/v$1" && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
}
release 1.0.0
release 1.1.0
release 1.2.0 com.knothhe.racomi.dev
release 1.3.0
printf 'tampered\n' >> "$releases/download/v1.3.0/racomi-1.3.0-x86_64-linux.tar.gz"

# Stubs record what would run as root; nothing reports as installed.
stub() { printf '#!/bin/sh\n%s\n' "$2" > "$stubs/$1"; chmod 755 "$stubs/$1"; }
stub sudo 'printf "sudo %s\n" "$*" >> "$CALLS"; exec "$@"'
stub pacman 'printf "pacman %s\n" "$*" >> "$CALLS"; [ "$1" != -Q ]'
stub apt-get 'printf "apt-get %s\n" "$*" >> "$CALLS"'
stub dpkg-query 'exit 1'
os_release() { printf 'ID=%s\nID_LIKE="%s"\n' "$1" "$2" > "$test_dir/os-release"; }

invoke() {
  HOME="$test_home" XDG_CONFIG_HOME= XDG_DATA_HOME= RACOMI_DATA_DIR= RACOMI_BIN_DIR= CALLS="$calls" \
    PATH="$stubs:$PATH" RACOMI_RELEASES_URL="file://$releases" RACOMI_OS_RELEASE="$test_dir/os-release" \
    sh "$project_dir/scripts/install.sh" "$@"
}
expect_failure() {
  reason=$1; shift
  if invoke "$@" > "$test_dir/error.log" 2>&1; then echo "expected refusal: $reason" >&2; exit 1; fi
}
version() { "$bin_dir/racomi" --build-info | grep -Fq "\"version\":\"$1\""; }

# Each distribution family selects its own package manager and package name.
os_release endeavouros arch
invoke --version 1.0.0 > "$test_dir/install.log"
grep -Fqx 'sudo pacman -S --needed --noconfirm webkit2gtk-4.1' "$calls"
version 1.0.0
grep -Fqx "Exec=\"$bin_dir/racomi\"" "$desktop"
grep -Fqx 'X-Racomi-Managed=make install' "$desktop"
test -s "$icon"
test -z "$(find "$test_home" -name '.racomi-install.*')"

: > "$calls"
os_release linuxmint 'ubuntu debian'
invoke --version v1.1.0 > "$test_dir/upgrade.log"
grep -Fqx 'sudo apt-get install -y --no-install-recommends libwebkit2gtk-4.1-0' "$calls"
version 1.1.0

os_release plan9 ''
expect_failure 'unrecognised distribution' --version 1.1.0
invoke --skip-deps --version 1.1.0 > "$test_dir/skip.log"

expect_failure 'checksum mismatch' --skip-deps --version 1.3.0
expect_failure 'missing release' --skip-deps --version 9.9.9
expect_failure 'Dev executable' --skip-deps --version 1.2.0
expect_failure 'invalid version' --skip-deps --version '1.0;rm'
version 1.1.0

# A command copied by hand from a release is adopted; anything else is not.
rm "$desktop"
invoke --skip-deps --version 1.0.0 > "$test_dir/adopt.log"
version 1.0.0
test -f "$desktop"
rm "$desktop"
printf '#!/bin/sh\nexit 0\n' > "$bin_dir/racomi"
expect_failure 'unrelated command' --skip-deps --version 1.0.0
expect_failure 'unrelated command' --uninstall
rm "$bin_dir/racomi"
expect_failure 'unrelated icon' --skip-deps --version 1.0.0
rm "$icon"
invoke --skip-deps --version 1.0.0 > "$test_dir/reinstall.log"

sed -i '/^X-Racomi-Managed=/d' "$desktop"
expect_failure 'unrelated desktop entry' --uninstall
printf 'X-Racomi-Managed=make install\n' >> "$desktop"

expect_failure 'unknown option' --bogus

invoke --uninstall > "$test_dir/uninstall.log"
test ! -e "$bin_dir/racomi"
test ! -e "$desktop"
test ! -e "$icon"
test "$(cat "$data_dir/state.json")" = 'keep user data'
invoke --uninstall > "$test_dir/uninstall-again.log"
echo 'Release installer checks passed'
