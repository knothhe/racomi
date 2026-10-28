#!/bin/sh
# Copy a built executable, desktop entry and icon without touching user data or
# files that belong to other applications.
set -eu

fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
[ "$(uname -s)" = Linux ] || fail 'this installer requires Linux'
[ "$#" -eq 5 ] || fail "usage: $0 install|uninstall SOURCE_BINARY BIN_DIR DESKTOP_DIR ICON_DIR"
action=$1
source_binary=$2
bin_dir=${3%/}
desktop_dir=${4%/}
icon_dir=${5%/}
case "$action" in install|uninstall) ;; *) fail "unknown action: $action" ;; esac
for dir in "$bin_dir" "$desktop_dir" "$icon_dir"; do
 case "$dir" in /*) ;; *) fail "installation directories must be absolute paths: $dir" ;; esac
 case "/$dir/" in */../*|*/./*) fail 'installation paths must not contain dot segments' ;; esac
done
channel=${CHANNEL:-stable}
case "$channel" in
 dev) app_name='racomi Dev'; command_name=racomi-dev; app_id=com.knothhe.racomi.dev; data_name=racomi-dev; icon_source=assets/dev-icons/app-icon-dev.png ;;
 stable|preview) app_name=racomi; command_name=racomi; app_id=com.knothhe.racomi; data_name=racomi; icon_source=assets/appicon.png ;;
 *) fail 'invalid CHANNEL' ;;
esac
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
command_path=$bin_dir/$command_name
desktop_path=$desktop_dir/$app_id.desktop
# hicolor has no 1024px directory; icon themes scale the larger source down.
icon_path=$icon_dir/512x512/apps/$app_id.png
# Desktop entry Exec values treat these characters specially; refuse rather than
# risk an entry that launches something else.
case "$command_path" in *'"'*|*'`'*|*'$'*|*'\'*|*'%'*|*'
'*) fail "unsupported character in command path: $command_path" ;; esac
data_root=${RACOMI_DATA_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/$data_name}
for target in "$command_path" "$desktop_path" "$icon_path"; do
 case "$target/" in "${data_root%/}/"*) fail "installation overlaps data directory: $target" ;; esac
 [ ! -L "$target" ] || fail "refusing to replace or remove a symlink: $target"
done
managed_marker='X-Racomi-Managed=make install'

# A desktop entry carrying the marker and this exact command proves ownership of
# the command and icon. Without one, never replace or remove existing files.
owned=false
if [ -e "$desktop_path" ]; then
 [ -f "$desktop_path" ] || fail "not a regular file: $desktop_path"
 grep -Fqx "$managed_marker" "$desktop_path" || fail "refusing to replace or remove an unrelated desktop entry: $desktop_path"
 grep -Fqx "Exec=\"$command_path\"" "$desktop_path" || fail "desktop entry belongs to another installation: $desktop_path"
 owned=true
fi
if [ "$owned" = false ]; then
 [ ! -e "$command_path" ] || fail "refusing to replace or remove an unrelated command: $command_path"
 [ ! -e "$icon_path" ] || fail "refusing to replace or remove an unrelated icon: $icon_path"
fi
[ ! -e "$command_path" ] || [ -f "$command_path" ] || fail "not a regular file: $command_path"

refresh_caches() {
 if command -v update-desktop-database >/dev/null 2>&1; then update-desktop-database -q "$desktop_dir" 2>/dev/null || true; fi
 if command -v gtk-update-icon-cache >/dev/null 2>&1 && [ -f "$icon_dir/index.theme" ]; then gtk-update-icon-cache -q -t "$icon_dir" 2>/dev/null || true; fi
}

if [ "$action" = uninstall ]; then
 if [ "$owned" = true ]; then
  rm -f "$command_path" "$icon_path"
  rm -f "$desktop_path"
  refresh_caches
 fi
 printf 'Removed %s, its desktop entry and icon (if installed).\nUser data and deployed skills have been kept.\n' "$command_path"
 printf 'A running racomi keeps working until it quits.\n'
 exit 0
fi

[ -f "$source_binary" ] && [ -x "$source_binary" ] || fail "build the executable first: $source_binary"
"$source_binary" --build-info 2>/dev/null | grep -Fq "\"id\":\"$app_id\"" || fail "executable does not belong to the $channel channel: $source_binary"
[ -s "$project_dir/$icon_source" ] || fail "missing icon: $icon_source"
mkdir -p "$bin_dir" "$desktop_dir" "$(dirname "$icon_path")"
# Stage on each destination filesystem so every rename is atomic. The desktop
# entry lands before the command, so a failed install never leaves a command
# that a later install would refuse to replace.
command_stage=''
desktop_stage=''
icon_stage=''
cleanup() { for stage in "$command_stage" "$desktop_stage" "$icon_stage"; do [ -z "$stage" ] || rm -f "$stage"; done; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
command_stage=$(mktemp "$bin_dir/.racomi-install.XXXXXX")
desktop_stage=$(mktemp "$desktop_dir/.racomi-install.XXXXXX")
icon_stage=$(mktemp "$(dirname "$icon_path")/.racomi-install.XXXXXX")
cp "$source_binary" "$command_stage"
chmod 755 "$command_stage"
cp "$project_dir/$icon_source" "$icon_stage"
chmod 644 "$icon_stage"
cat > "$desktop_stage" <<EOF
[Desktop Entry]
Type=Application
Name=$app_name
Comment=A lightweight local agent skill manager
Exec="$command_path"
TryExec=$command_path
Icon=$app_id
Terminal=false
Categories=Development;Utility;
StartupNotify=true
StartupWMClass=$app_id
$managed_marker
EOF
chmod 644 "$desktop_stage"
mv -f "$icon_stage" "$icon_path"; icon_stage=''
mv -f "$desktop_stage" "$desktop_path"; desktop_stage=''
mv -f "$command_stage" "$command_path"; command_stage=''
refresh_caches
printf 'Installed %s\nDesktop entry: %s\n' "$command_path" "$desktop_path"
case ":${PATH:-}:" in
 *":$bin_dir:"*) ;;
 *) printf 'To use %s by name, add %s to your shell PATH.\n' "$command_name" "$bin_dir" ;;
esac
printf 'Upgrade: run make install again, then restart a running racomi.\nUninstall: make uninstall (keeps user data and deployed skills).\n'
