#!/bin/sh
# Exercise destructive paths against an isolated fixture, never the real HOME.
set -eu
[ "$(uname -s)" = Darwin ] || { echo 'macOS install checks skipped on this platform'; exit 0; }
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/racomi-install-test.XXXXXX")
trap 'rm -rf "$test_dir"' EXIT
trap 'exit 1' HUP INT TERM
source_app="$test_dir/source/racomi.app"
test_home="$test_dir/home"
app_dir="$test_home/User's Applications"
bin_dir="$test_home/local bin"
data_dir="$test_home/Library/Application Support/racomi"
mkdir -p "$source_app/Contents/MacOS" "$data_dir"
cp "$project_dir/build/macos/Info.plist" "$source_app/Contents/Info.plist"
printf '#!/bin/sh\nexit 0\n' > "$source_app/Contents/MacOS/racomi"
chmod 755 "$source_app/Contents/MacOS/racomi"
printf 'keep user data\n' > "$data_dir/state.json"

invoke() { HOME="$test_home" sh "$project_dir/scripts/install-macos.sh" "$1" "$source_app" "$app_dir" "$bin_dir"; }
expect_failure() {
  if invoke "$1" > "$test_dir/error.log" 2>&1; then echo "expected refusal: $2" >&2; exit 1; fi
}

invoke install > "$test_dir/install.log"
test -x "$app_dir/racomi.app/Contents/MacOS/racomi"
test -x "$bin_dir/racomi"
sh -n "$bin_dir/racomi"
# Read the literal target without invoking open or launching the fixture app.
launcher_target=$(sed -n '3p' "$bin_dir/racomi")
printf '%s\n' "$launcher_target" > "$test_dir/target.sh"
printf 'printf "%%s" "$app_path"\n' >> "$test_dir/target.sh"
test "$(sh "$test_dir/target.sh")" = "$app_dir/racomi.app"
if sh "$bin_dir/racomi" --unexpected > "$test_dir/args.log" 2>&1; then echo 'launcher must reject arguments' >&2; exit 1; fi

printf 'old build\n' > "$app_dir/racomi.app/Contents/old-file"
/usr/libexec/PlistBuddy -c 'Set :CFBundleIdentifier local.racomi.desktop' "$app_dir/racomi.app/Contents/Info.plist"
printf 'new build\n' > "$source_app/Contents/new-file"
invoke install > "$test_dir/upgrade.log"
test ! -e "$app_dir/racomi.app/Contents/old-file"
test -f "$app_dir/racomi.app/Contents/new-file"
test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app_dir/racomi.app/Contents/Info.plist")" = com.knothhe.racomi

# Fail the final launcher rename after moving the app. The previous complete
# bundle and launcher must remain available when an installation cannot finish.
mkdir -p "$test_dir/failing-tools"
cat > "$test_dir/failing-tools/mv" <<'SH'
#!/bin/sh
if [ "$#" -eq 3 ] && [ "$1" = -f ]; then exit 1; fi
exec /bin/mv "$@"
SH
chmod 755 "$test_dir/failing-tools/mv"
printf 'keep old bundle on failure\n' > "$app_dir/racomi.app/Contents/rollback-marker"
if PATH="$test_dir/failing-tools:$PATH" invoke install > "$test_dir/rollback.log" 2>&1; then
  echo 'expected installation failure at launcher rename' >&2; exit 1
fi
test -f "$app_dir/racomi.app/Contents/rollback-marker"
test -x "$bin_dir/racomi"

# A user-owned command must block both replacement and uninstall, before either
# operation touches the application. An unrelated bundle is also protected.
cp "$bin_dir/racomi" "$test_dir/launcher"
printf '#!/bin/sh\necho unrelated\n' > "$bin_dir/racomi"
expect_failure install 'foreign command on install'
expect_failure uninstall 'foreign command on uninstall'
test -f "$app_dir/racomi.app/Contents/new-file"
cp "$test_dir/launcher" "$bin_dir/racomi"
/usr/libexec/PlistBuddy -c 'Set :CFBundleIdentifier example.unrelated' "$app_dir/racomi.app/Contents/Info.plist"
expect_failure uninstall 'foreign bundle'
/usr/libexec/PlistBuddy -c 'Set :CFBundleIdentifier com.knothhe.racomi' "$app_dir/racomi.app/Contents/Info.plist"

invoke uninstall > "$test_dir/uninstall.log"
test ! -e "$app_dir/racomi.app"
test ! -e "$bin_dir/racomi"
test "$(cat "$data_dir/state.json")" = 'keep user data'
invoke uninstall > "$test_dir/uninstall-again.log"

ln -s "$source_app" "$app_dir/racomi.app"
expect_failure install 'symlink bundle on install'
expect_failure uninstall 'symlink bundle on uninstall'
test -x "$source_app/Contents/MacOS/racomi"
printf 'macOS install checks passed (install, upgrade, rollback, path quoting, unrelated files, symlinks, idempotent uninstall, data retention).\n'
