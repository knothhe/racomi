#!/bin/sh
# Copy a built app without deleting user data or unrelated applications/commands.
set -eu

fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
[ "$(uname -s)" = Darwin ] || fail 'installation requires macOS 13 or newer'
[ "$#" -eq 4 ] || fail "usage: $0 install|uninstall SOURCE_APP APP_DIR BIN_DIR"
action=$1
source_app=$2
app_dir=$3
bin_dir=$4
case "$action" in install|uninstall) ;; *) fail "unknown action: $action" ;; esac
case "$app_dir" in /*) ;; *) fail 'APP_DIR must be an absolute path' ;; esac
case "$bin_dir" in /*) ;; *) fail 'BIN_DIR must be an absolute path' ;; esac
channel=${CHANNEL:-stable}
case "$channel" in
 dev) app_name='racomi Dev'; command_name=racomi-dev; expected_id=com.knothhe.racomi.dev ;;
 stable|preview) app_name=racomi; command_name=racomi; expected_id=com.knothhe.racomi ;;
 *) fail 'invalid CHANNEL' ;;
esac
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app_path=${app_dir%/}/$app_name.app
launcher_path=${bin_dir%/}/$command_name
# Normalize standard macOS aliases, but reject user-controlled symlink ancestors.
check_parents() {
 parent=$1
 while [ "$parent" != / ]; do
  case "$parent" in /var|/tmp) ;; *) [ ! -L "$parent" ] || fail "symlink ancestor: $parent" ;; esac
  parent=$(dirname "$parent")
 done
}
canonical_path() {
 case "/$1/" in */../*|*/./*) fail 'installation and data paths must not contain dot segments' ;; esac
 candidate=$1
 suffix=''
 while [ ! -e "$candidate" ]; do
  suffix="/$(basename "$candidate")$suffix"
  candidate=$(dirname "$candidate")
 done
 if [ -d "$candidate" ]; then
  resolved=$(CDPATH= cd -P -- "$candidate" && pwd)
 else
  resolved=$(CDPATH= cd -P -- "$(dirname "$candidate")" && pwd)/$(basename "$candidate")
 fi
 printf '%s%s' "$resolved" "$suffix"
}
check_parents "$app_dir"; check_parents "$bin_dir"
app_dir=$(canonical_path "$app_dir")
bin_dir=$(canonical_path "$bin_dir")
app_path=${app_dir%/}/$app_name.app
launcher_path=${bin_dir%/}/$command_name
case "$channel" in dev) data_name=racomi-dev ;; *) data_name=racomi ;; esac
data_root=${RACOMI_DATA_DIR:-"$HOME/Library/Application Support/$data_name"}
data_root=$(canonical_path "$data_root")
case "$app_path/" in "$data_root/"* ) fail 'installation overlaps data directory' ;; esac
case "$data_root/" in "$app_path/"* ) fail 'data directory overlaps installation' ;; esac
case "$app_path" in "$HOME/Library/Application Support/"*|"$HOME/Library/LaunchAgents/"*) fail 'installation overlaps user data or registration directories' ;; esac
launcher_marker="# $command_name launcher (managed by make install)"

check_app() {
  [ ! -L "$1" ] || fail "refusing to replace or remove a symlink: $1"
  [ -d "$1" ] && [ -x "$1/Contents/MacOS/racomi" ] || fail "not a racomi app bundle: $1"
  bundle_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$1/Contents/Info.plist" 2>/dev/null) || fail "invalid app metadata: $1"
  # Recognize the previous identifier so existing installations can be upgraded.
  case "$bundle_id" in
    "$expected_id") ;;
    local.racomi.desktop) [ "$channel" != dev ] || fail "refusing stable installation from Dev" ;;
    *) fail "refusing to replace or remove a different app: $1" ;;
  esac
}

if [ -e "$app_path" ] || [ -L "$app_path" ]; then check_app "$app_path"; fi
if [ -e "$launcher_path" ] || [ -L "$launcher_path" ]; then
  [ ! -L "$launcher_path" ] && [ -f "$launcher_path" ] || fail "refusing to replace or remove an unrelated launcher: $launcher_path"
  /usr/bin/grep -Fqx "$launcher_marker" "$launcher_path" || fail "refusing to replace or remove an unrelated command: $launcher_path"
  expected_quoted_app=$(printf '%s' "$app_path" | sed "s/'/'\\\\''/g")
  /usr/bin/grep -Fqx "app_path='$expected_quoted_app'" "$launcher_path" || fail "launcher belongs to another installation: $launcher_path"
fi

quit_target() {
 if [ -d "$app_path" ]; then
  actual_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app_path/Contents/Info.plist")
  /usr/bin/osascript -l JavaScript "$project_dir/scripts/quit-macos.js" "$actual_id" "$app_path" >/dev/null || fail 'normal exit failed; installation has been preserved'
 fi
}
cleanup_login() {
 check_parents "$HOME/Library/LaunchAgents"
 for label in "$expected_id" $(if [ "$channel" != dev ]; then printf '%s' local.racomi.desktop; fi); do
  record="$HOME/Library/LaunchAgents/$label.plist"
  [ ! -L "$record" ] || fail "refusing symlink registration: $record"
  if [ -e "$record" ]; then
   recorded_exe=$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments:0' "$record" 2>/dev/null) || fail "cannot read login registration: $record"
   if [ "$(canonical_path "$recorded_exe")" = "$app_path/Contents/MacOS/racomi" ]; then
    # RunAtLoad records do not need bootstrap; unload if launchd owns this one.
    uid=$(id -u)
    if /bin/launchctl print "gui/$uid/$label" >/dev/null 2>&1; then
     active_program=$(/bin/launchctl print "gui/$uid/$label" | /usr/bin/awk '/^[[:space:]]*program = / { sub(/^[[:space:]]*program = /, ""); print; exit }')
     [ -n "$active_program" ] && [ "$(canonical_path "$active_program")" = "$app_path/Contents/MacOS/racomi" ] || fail "active login job belongs to another or unknown installation: $label"
     /bin/launchctl bootout "gui/$uid" "$record" || fail "login deregistration failed; retain registration for retry: $record"
    fi
    rm -f "$record" || fail "could not remove login registration: $record"
   else
    printf 'Preserved registration owned by another installation: %s\n' "$record" >&2
   fi
  fi
 done
}

if [ "$action" = uninstall ]; then
  quit_target
  cleanup_login
  if [ -d "$app_path" ]; then rm -rf "$app_path"; fi
  if [ -f "$launcher_path" ]; then rm -f "$launcher_path"; fi
  printf 'Removed %s and %s (if installed).\nUser data and deployed skills have been kept.\n' "$app_path" "$launcher_path"
  exit 0
fi

check_app "$source_app"
mkdir -p "$app_dir" "$bin_dir"
# Stage on each destination filesystem, then replace the complete app so upgrades
# do not leave files from an older build behind. Roll back on a failed rename.
app_stage=$(mktemp -d "$app_dir/.racomi-install.XXXXXX")
launcher_stage=''
old_app_moved=false
new_app_moved=false
success=false
cleanup() {
  if [ "$success" = false ]; then
    if [ "$new_app_moved" = true ]; then rm -rf "$app_path"; fi
    if [ "$old_app_moved" = true ]; then mv "$app_stage/previous.app" "$app_path"; fi
  fi
  rm -rf "$app_stage"
  if [ -n "$launcher_stage" ]; then rm -f "$launcher_stage"; fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
/usr/bin/ditto "$source_app" "$app_stage/racomi.app"
launcher_stage=$(mktemp "$bin_dir/.racomi-launcher.XXXXXX")
# The command opens the GUI app through Launch Services, preserving its bundle
# identity and Dock icon. Quote custom installation paths as literal shell text.
quoted_app=$(printf '%s' "$app_path" | sed "s/'/'\\\\''/g")
{
  printf '#!/bin/sh\n%s\n' "$launcher_marker"
  printf "app_path='%s'\n" "$quoted_app"
  printf '%s\n' 'if [ "$#" -ne 0 ]; then' '  echo "racomi opens the desktop app and does not accept command-line arguments." >&2' '  exit 2' 'fi' 'exec /usr/bin/open "$app_path"'
} > "$launcher_stage"
chmod 755 "$launcher_stage"
quit_target
if [ -d "$app_path" ]; then
  mv "$app_path" "$app_stage/previous.app"
  old_app_moved=true
fi
mv "$app_stage/racomi.app" "$app_path"
new_app_moved=true
mv -f "$launcher_stage" "$launcher_path"
launcher_stage=''
success=true
printf 'Installed %s\nLauncher: %s\nOpen the app from Finder or run: "%s"\n' "$app_path" "$launcher_path" "$launcher_path"
case ":${PATH:-}:" in
  *":$bin_dir:"*) ;;
  *) printf 'To use racomi by name, add %s to your shell PATH.\n' "$bin_dir" ;;
esac
printf 'Upgrade: quit racomi, then run make install again.\nUninstall: make uninstall (keeps user data and deployed skills).\n'
printf 'For download/Gatekeeper help, see README.md → macOS 安装 or the Release installation notes.\n'
