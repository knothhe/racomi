#!/bin/sh
# One-step installer for the official Linux release:
#   curl -fsSL https://github.com/knothhe/racomi/releases/latest/download/install.sh | sh
# Installs the runtime libraries with the distribution's package manager, then
# a verified release into ~/.local/bin where the in-app updater can replace it.
# Pass options after `sh -s --`: --version X.Y.Z, --skip-deps, --uninstall.
set -eu

# Everything runs from main, so a truncated download executes nothing.
fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
say() { printf '%s\n' "racomi: $*"; }

usage() {
 cat <<'EOF'
Usage: install.sh [--version X.Y.Z] [--skip-deps] [--uninstall]

  --version X.Y.Z  install this release instead of the latest stable one
  --skip-deps      do not install WebKit2GTK 4.1 and Git with the package manager
  --uninstall      remove the command, desktop entry and icon; keep user data

Environment: RACOMI_BIN_DIR (default ~/.local/bin), XDG_DATA_HOME.
EOF
}

app_id=com.knothhe.racomi
managed_marker='X-Racomi-Managed=make install'
releases_url=${RACOMI_RELEASES_URL:-https://github.com/knothhe/racomi/releases}

fetch() {
 if command -v curl >/dev/null 2>&1; then
  curl -fsSL --retry 3 --proto-redir =https -o "$2" "$1"
 elif command -v wget >/dev/null 2>&1; then
  wget -q -O "$2" "$1"
 else
  fail 'curl or wget is required'
 fi
}

# GitHub redirects releases/latest to releases/tag/vX.Y.Z, which avoids the
# rate-limited API and needs no JSON parser.
latest_version() {
 if command -v curl >/dev/null 2>&1; then
  location=$(curl -fsSLI --retry 3 -o /dev/null -w '%{url_effective}' "$releases_url/latest") || fail 'cannot reach GitHub Releases'
 elif command -v wget >/dev/null 2>&1; then
  location=$(wget -q -S --spider "$releases_url/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1 | tr -d '\r')
 else
  fail 'curl or wget is required'
 fi
 case "$location" in */releases/tag/v*) printf '%s\n' "${location##*/releases/tag/v}" ;; *) fail 'no published release found' ;; esac
}

# Prints "manager package..." for the missing runtime packages, or nothing.
missing_packages() {
 os_release=${RACOMI_OS_RELEASE:-/etc/os-release}
 [ -r "$os_release" ] || os_release=/usr/lib/os-release
 [ -r "$os_release" ] || return 2
 ids=$(. "$os_release"; printf '%s %s' "${ID:-}" "${ID_LIKE:-}")
 case " $ids " in
  *' arch '*) manager=pacman; webkit=webkit2gtk-4.1 ;;
  *' debian '*|*' ubuntu '*) manager=apt-get; webkit=libwebkit2gtk-4.1-0 ;;
  *' suse '*|*' opensuse '*) manager=zypper; webkit=libwebkit2gtk-4_1-0 ;;
  *' fedora '*|*' rhel '*|*' centos '*) manager=dnf; webkit=webkit2gtk4.1 ;;
  *) return 2 ;;
 esac
 command -v "$manager" >/dev/null 2>&1 || return 2
 packages=''
 for package in "$webkit" git; do
  if [ "$package" = git ] && command -v git >/dev/null 2>&1; then continue; fi
  case "$manager" in
   pacman) pacman -Q "$package" >/dev/null 2>&1 && continue ;;
   apt-get) dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q 'ok installed' && continue ;;
   *) rpm -q "$package" >/dev/null 2>&1 && continue ;;
  esac
  packages="$packages $package"
 done
 [ -z "$packages" ] || printf '%s%s\n' "$manager" "$packages"
}

install_dependencies() {
 status=0
 missing=$(missing_packages) || status=$?
 if [ "$status" -ne 0 ]; then
  say 'unrecognised distribution; install WebKit2GTK 4.1 and Git yourself, then rerun with --skip-deps'
  fail 'package names: Arch webkit2gtk-4.1, Debian/Ubuntu libwebkit2gtk-4.1-0, Fedora webkit2gtk4.1, openSUSE libwebkit2gtk-4_1-0'
 fi
 [ -n "$missing" ] || return 0
 set -- $missing
 manager=$1; shift
 if [ "$(id -u)" -eq 0 ]; then
  as_root=''
 elif command -v sudo >/dev/null 2>&1; then
  as_root=sudo
 elif command -v doas >/dev/null 2>&1; then
  as_root=doas
 else
  fail "install these packages as root, then rerun with --skip-deps: $*"
 fi
 say "installing $* with $manager (requires administrator rights)"
 # Package managers must not read the piped script from standard input.
 case "$manager" in
  pacman) $as_root pacman -S --needed --noconfirm "$@" </dev/null ;;
  apt-get) $as_root apt-get update </dev/null && $as_root apt-get install -y --no-install-recommends "$@" </dev/null ;;
  zypper) $as_root zypper --non-interactive install "$@" </dev/null ;;
  dnf) $as_root dnf install -y "$@" </dev/null ;;
 esac || fail "package installation failed: $*"
}

refresh_caches() {
 if command -v update-desktop-database >/dev/null 2>&1; then update-desktop-database -q "$desktop_dir" 2>/dev/null || true; fi
 if command -v gtk-update-icon-cache >/dev/null 2>&1 && [ -f "$icon_dir/index.theme" ]; then gtk-update-icon-cache -q -t "$icon_dir" 2>/dev/null || true; fi
}

main() {
 action=install
 version=''
 skip_deps=false
 while [ "$#" -gt 0 ]; do
  case "$1" in
   --version) [ "$#" -ge 2 ] || fail '--version needs a value'; version=${2#v}; shift ;;
   --version=*) version=${1#--version=}; version=${version#v} ;;
   --skip-deps) skip_deps=true ;;
   --uninstall) action=uninstall ;;
   -h|--help) usage; exit 0 ;;
   *) usage >&2; fail "unknown option: $1" ;;
  esac
  shift
 done
 [ "$(uname -s)" = Linux ] || fail 'this installer requires Linux; see the README for macOS and Windows'
 [ -n "${HOME:-}" ] || fail 'HOME is not set'

 bin_dir=${RACOMI_BIN_DIR:-$HOME/.local/bin}
 bin_dir=${bin_dir%/}
 data_home=${XDG_DATA_HOME:-$HOME/.local/share}
 desktop_dir=${data_home%/}/applications
 icon_dir=${data_home%/}/icons/hicolor
 for dir in "$bin_dir" "$desktop_dir" "$icon_dir"; do
  case "$dir" in /*) ;; *) fail "installation directories must be absolute paths: $dir" ;; esac
  case "/$dir/" in */../*|*/./*) fail 'installation paths must not contain dot segments' ;; esac
 done
 command_path=$bin_dir/racomi
 desktop_path=$desktop_dir/$app_id.desktop
 icon_path=$icon_dir/512x512/apps/$app_id.png
 case "$command_path" in *'"'*|*'`'*|*'$'*|*'\'*|*'%'*|*'
'*) fail "unsupported character in command path: $command_path" ;; esac
 data_root=${RACOMI_DATA_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/racomi}
 for target in "$command_path" "$desktop_path" "$icon_path"; do
  case "$target/" in "${data_root%/}/"*) fail "installation overlaps data directory: $target" ;; esac
  [ ! -L "$target" ] || fail "refusing to replace or remove a symlink: $target"
 done

 # Ours if the managed desktop entry points at this command (make install
 # CHANNEL=stable shares it), or if the command is a racomi release that was
 # copied by hand as the README used to describe.
 owned=false
 if [ -e "$desktop_path" ]; then
  [ -f "$desktop_path" ] || fail "not a regular file: $desktop_path"
  grep -Fqx "$managed_marker" "$desktop_path" || fail "refusing to replace or remove an unrelated desktop entry: $desktop_path"
  grep -Fqx "Exec=\"$command_path\"" "$desktop_path" || fail "desktop entry belongs to another installation: $desktop_path"
  owned=true
 elif [ -f "$command_path" ] && [ -x "$command_path" ] && "$command_path" --build-info 2>/dev/null </dev/null | grep -Fq "\"id\":\"$app_id\""; then
  owned=true
 fi
 if [ "$owned" = false ]; then
  [ ! -e "$command_path" ] || fail "refusing to replace or remove an unrelated command: $command_path"
  [ ! -e "$icon_path" ] || fail "refusing to replace or remove an unrelated icon: $icon_path"
 fi
 [ ! -e "$command_path" ] || [ -f "$command_path" ] || fail "not a regular file: $command_path"

 if [ "$action" = uninstall ]; then
  if [ "$owned" = true ]; then
   rm -f "$command_path" "$icon_path" "$desktop_path"
   refresh_caches
  fi
  say "removed $command_path, its desktop entry and icon (if installed)"
  say 'user data and deployed skills have been kept; a running racomi keeps working until it quits'
  exit 0
 fi

 [ "$(uname -m)" = x86_64 ] || fail "official Linux builds are x86_64 only; build from source on $(uname -m)"
 [ "$skip_deps" = true ] || install_dependencies
 command -v sha256sum >/dev/null 2>&1 || fail 'sha256sum is required'
 command -v tar >/dev/null 2>&1 || fail 'tar is required'
 [ -n "$version" ] || version=$(latest_version)
 printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$' || fail "invalid version: $version"

 work=$(mktemp -d "${TMPDIR:-/tmp}/racomi-install.XXXXXX")
 command_stage=''
 desktop_stage=''
 icon_stage=''
 cleanup() { for stage in "$command_stage" "$desktop_stage" "$icon_stage"; do [ -z "$stage" ] || rm -f "$stage"; done; rm -rf "$work"; }
 trap cleanup EXIT
 trap 'exit 1' HUP INT TERM

 name="racomi-$version-x86_64-linux"
 say "downloading racomi $version"
 fetch "$releases_url/download/v$version/$name.tar.gz" "$work/$name.tar.gz" || fail "download failed: $name.tar.gz"
 fetch "$releases_url/download/v$version/$name.tar.gz.sha256" "$work/$name.tar.gz.sha256" || fail "download failed: $name.tar.gz.sha256"
 (cd "$work" && sha256sum -c --status "$name.tar.gz.sha256") || fail "checksum mismatch: $name.tar.gz"
 mkdir "$work/extract"
 tar -xzf "$work/$name.tar.gz" -C "$work/extract" --no-same-owner
 binary=$work/extract/$name/racomi
 icon=$work/extract/$name/racomi.png
 [ -f "$binary" ] && [ -s "$icon" ] || fail "unexpected archive layout: $name.tar.gz"
 chmod 755 "$binary"
 build_info=$("$binary" --build-info 2>&1 </dev/null) || fail "racomi cannot start; check that WebKit2GTK 4.1 is installed: $build_info"
 printf '%s\n' "$build_info" | grep -Fq "\"id\":\"$app_id\"" || fail 'downloaded executable is not an official racomi release'

 mkdir -p "$bin_dir" "$desktop_dir" "$(dirname "$icon_path")"
 # Same staging order as make install: the desktop entry lands before the
 # command, so a failed install never leaves a command it would later refuse.
 command_stage=$(mktemp "$bin_dir/.racomi-install.XXXXXX")
 desktop_stage=$(mktemp "$desktop_dir/.racomi-install.XXXXXX")
 icon_stage=$(mktemp "$(dirname "$icon_path")/.racomi-install.XXXXXX")
 cp "$binary" "$command_stage"
 chmod 755 "$command_stage"
 cp "$icon" "$icon_stage"
 chmod 644 "$icon_stage"
 cat > "$desktop_stage" <<EOF
[Desktop Entry]
Type=Application
Name=racomi
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
 say "installed racomi $version to $command_path"
 case ":${PATH:-}:" in
  *":$bin_dir:"*) ;;
  *) say "add $bin_dir to your shell PATH to run racomi by name" ;;
 esac
 say 'later updates arrive in Settings -> About; restart a running racomi to use this version'
 say 'uninstall: rerun this script with --uninstall (keeps user data and deployed skills)'
}

main "$@"
