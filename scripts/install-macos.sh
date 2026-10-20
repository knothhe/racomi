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
app_path=${app_dir%/}/racomi.app
launcher_path=${bin_dir%/}/racomi
launcher_marker='# racomi launcher (managed by make install)'

check_app() {
  [ ! -L "$1" ] || fail "refusing to replace or remove a symlink: $1"
  [ -d "$1" ] && [ -x "$1/Contents/MacOS/racomi" ] || fail "not a racomi app bundle: $1"
  bundle_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$1/Contents/Info.plist" 2>/dev/null) || fail "invalid app metadata: $1"
  # Recognize the previous identifier so existing installations can be upgraded.
  case "$bundle_id" in
    com.knothhe.racomi|local.racomi.desktop) ;;
    *) fail "refusing to replace or remove a different app: $1" ;;
  esac
}

if [ -e "$app_path" ] || [ -L "$app_path" ]; then check_app "$app_path"; fi
if [ -e "$launcher_path" ] || [ -L "$launcher_path" ]; then
  [ ! -L "$launcher_path" ] && [ -f "$launcher_path" ] || fail "refusing to replace or remove an unrelated launcher: $launcher_path"
  /usr/bin/grep -Fqx "$launcher_marker" "$launcher_path" || fail "refusing to replace or remove an unrelated command: $launcher_path"
fi

if [ "$action" = uninstall ]; then
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
