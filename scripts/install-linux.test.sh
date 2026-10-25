#!/bin/sh
# Exercise destructive paths against an isolated fixture, never the real HOME.
set -eu
[ "$(uname -s)" = Linux ] || { echo 'Linux install checks skipped on this platform'; exit 0; }
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/racomi-install-test.XXXXXX")
test_dir=$(CDPATH= cd -P -- "$test_dir" && pwd)
trap 'rm -rf "$test_dir"' EXIT
trap 'exit 1' HUP INT TERM
test_home="$test_dir/home"
bin_dir="$test_home/local bin"
desktop_dir="$test_home/share/applications"
icon_dir="$test_home/share/icons/hicolor"
data_dir="$test_home/.config/racomi"
source_binary="$test_dir/source/racomi"
mkdir -p "$(dirname "$source_binary")" "$data_dir"
fixture() {
  printf '#!/bin/sh\n[ "$1" = --build-info ] && printf %s\\n %s\n' "'%s'" "'{\"name\":\"racomi\",\"id\":\"$1\",\"build\":\"$2\"}'" > "$source_binary"
  chmod 755 "$source_binary"
}
fixture com.knothhe.racomi first
printf 'keep user data\n' > "$data_dir/state.json"

invoke() { HOME="$test_home" XDG_CONFIG_HOME= RACOMI_DATA_DIR= CHANNEL=${channel:-stable} sh "$project_dir/scripts/install-linux.sh" "$1" "$source_binary" "$bin_dir" "$desktop_dir" "$icon_dir"; }
expect_failure() {
  if invoke "$1" > "$test_dir/error.log" 2>&1; then echo "expected refusal: $2" >&2; exit 1; fi
}

invoke install > "$test_dir/install.log"
desktop="$desktop_dir/com.knothhe.racomi.desktop"
icon="$icon_dir/512x512/apps/com.knothhe.racomi.png"
test -x "$bin_dir/racomi"
test -s "$icon"
grep -Fqx "Exec=\"$bin_dir/racomi\"" "$desktop"
grep -Fqx 'StartupWMClass=com.knothhe.racomi' "$desktop"
grep -Fqx 'Icon=com.knothhe.racomi' "$desktop"
test -z "$(find "$bin_dir" "$desktop_dir" "$icon_dir" -name '.racomi-install.*')"

fixture com.knothhe.racomi second
invoke install > "$test_dir/upgrade.log"
"$bin_dir/racomi" --build-info | grep -Fq '"build":"second"'

# A Dev build never replaces the stable command, and stable never takes Dev's.
fixture com.knothhe.racomi.dev dev
expect_failure install 'stable channel from a Dev executable'
channel=dev invoke install > "$test_dir/dev.log"
test -x "$bin_dir/racomi-dev"
"$bin_dir/racomi" --build-info | grep -Fq '"build":"second"'
channel=dev invoke uninstall > "$test_dir/dev-uninstall.log"
test ! -e "$bin_dir/racomi-dev"
test -x "$bin_dir/racomi"
fixture com.knothhe.racomi third

# Files without our desktop entry belong to someone else.
mv "$desktop" "$test_dir/desktop.saved"
expect_failure install 'unmanaged command'
expect_failure uninstall 'unmanaged command'
test -x "$bin_dir/racomi"
mv "$test_dir/desktop.saved" "$desktop"

cp "$desktop" "$test_dir/desktop.saved"
sed -i '/^X-Racomi-Managed=/d' "$desktop"
expect_failure install 'unrelated desktop entry'
cp "$test_dir/desktop.saved" "$desktop"

sed -i "s|^Exec=.*|Exec=\"$test_home/elsewhere/racomi\"|" "$desktop"
expect_failure uninstall 'desktop entry of another installation'
cp "$test_dir/desktop.saved" "$desktop"

mv "$bin_dir/racomi" "$test_dir/racomi.saved"
ln -s "$test_dir/racomi.saved" "$bin_dir/racomi"
expect_failure install 'command symlink'
rm "$bin_dir/racomi"
mv "$test_dir/racomi.saved" "$bin_dir/racomi"

# Paths that a desktop entry Exec line would reinterpret are refused.
if HOME="$test_home" CHANNEL=stable sh "$project_dir/scripts/install-linux.sh" install "$source_binary" "$test_home/b\$in" "$desktop_dir" "$icon_dir" > "$test_dir/error.log" 2>&1; then
  echo 'expected refusal: special character in command path' >&2; exit 1
fi
if HOME="$test_home" CHANNEL=stable sh "$project_dir/scripts/install-linux.sh" install "$source_binary" relative "$desktop_dir" "$icon_dir" > "$test_dir/error.log" 2>&1; then
  echo 'expected refusal: relative path' >&2; exit 1
fi
if HOME="$test_home" CHANNEL=stable RACOMI_DATA_DIR="$bin_dir" sh "$project_dir/scripts/install-linux.sh" install "$source_binary" "$bin_dir" "$desktop_dir" "$icon_dir" > "$test_dir/error.log" 2>&1; then
  echo 'expected refusal: installation inside data directory' >&2; exit 1
fi

invoke uninstall > "$test_dir/uninstall.log"
test ! -e "$bin_dir/racomi"
test ! -e "$desktop"
test ! -e "$icon"
test "$(cat "$data_dir/state.json")" = 'keep user data'
invoke uninstall > "$test_dir/uninstall-again.log"
echo 'Linux install checks passed'
