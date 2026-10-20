#!/bin/sh
set -eu

fail() { printf '%s\n' "racomi: $*" >&2; exit 1; }
[ "$(uname -s)" = Darwin ] || fail 'packaging requires macOS and Xcode Command Line Tools'
[ "$#" -eq 5 ] || fail "usage: $0 package|release VERSION BUILD_NUMBER MIN_MACOS GOARCH"
action=$1
version=${2#v}
build_number=$3
minimum_macos=$4
architecture=$5
case "$action" in package|release) ;; *) fail "unknown action: $action" ;; esac
printf '%s\n' "$version" | /usr/bin/grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$' || fail 'VERSION must be X.Y.Z or X.Y.Z-prerelease'
printf '%s\n' "$build_number" | /usr/bin/grep -Eq '^[1-9][0-9]*$' || fail 'BUILD_NUMBER must be a positive integer'
printf '%s\n' "$minimum_macos" | /usr/bin/grep -Eq '^[0-9]+\.[0-9]+(\.[0-9]+)?$' || fail 'invalid MACOSX_DEPLOYMENT_TARGET'
case "$architecture" in arm64) archive_arch=aarch64 ;; amd64) archive_arch=x86_64 ;; *) fail "unsupported macOS GOARCH: $architecture" ;; esac
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app_path="$project_dir/bin/racomi.app"
[ -x "$project_dir/bin/racomi" ] || fail 'build bin/racomi first (make package or make release)'
actual_arch=$(/usr/bin/lipo -archs "$project_dir/bin/racomi")
[ "$actual_arch" = "$archive_arch" ] || [ "$actual_arch" = "$architecture" ] || fail "binary architecture $actual_arch does not match GOARCH=$architecture"
stage=$(mktemp -d "$project_dir/bin/.racomi-package.XXXXXX")
old_app_moved=false
new_app_moved=false
success=false
cleanup() {
  if [ "$action" = package ] && [ "$success" = false ]; then
    if [ "$new_app_moved" = true ]; then rm -rf "$app_path"; fi
    if [ "$old_app_moved" = true ]; then mv "$stage/previous.app" "$app_path"; fi
  fi
  rm -rf "$stage"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

if [ "$action" = package ]; then
  bundle="$stage/racomi.app"
  mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
  cp "$project_dir/bin/racomi" "$bundle/Contents/MacOS/racomi"
  cp "$project_dir/build/macos/Info.plist" "$bundle/Contents/Info.plist"
  cp "$project_dir/build/macos/racomi.icns" "$bundle/Contents/Resources/racomi.icns"
  cp "$project_dir/build/macos/Assets.car" "$bundle/Contents/Resources/Assets.car"
  cp "$project_dir/build/appicon.png" "$bundle/Contents/Resources/NSApplicationIcon.png"
  /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${version%%-*}" "$bundle/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $build_number" "$bundle/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion $minimum_macos" "$bundle/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c "Add :RacomiReleaseVersion string $version" "$bundle/Contents/Info.plist"
  /usr/bin/plutil -lint "$bundle/Contents/Info.plist"
  # Apple Silicon requires a valid code signature. Ad-hoc signing is local only;
  # it is not a Developer ID signature and does not provide notarization.
  /usr/bin/codesign --force --sign - "$bundle"
  /usr/bin/codesign --verify --strict "$bundle"
  [ ! -L "$app_path" ] || fail "refusing to replace a symlink: $app_path"
  if [ -e "$app_path" ]; then
    existing_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app_path/Contents/Info.plist" 2>/dev/null) || fail 'invalid existing bin/racomi.app'
    case "$existing_id" in
      com.knothhe.racomi|local.racomi.desktop) ;;
      *) fail 'refusing to replace an unrelated bin/racomi.app' ;;
    esac
    mv "$app_path" "$stage/previous.app"
    old_app_moved=true
  fi
  mv "$bundle" "$app_path"
  new_app_moved=true
  success=true
  printf 'Packaged %s (%s, macOS %s+, local ad-hoc signature; not notarized).\n' "$app_path" "$version" "$minimum_macos"
  exit 0
fi

[ -d "$app_path" ] || fail 'build bin/racomi.app first'
packaged_version=$(/usr/libexec/PlistBuddy -c 'Print :RacomiReleaseVersion' "$app_path/Contents/Info.plist")
[ "$packaged_version" = "$version" ] || fail 'app version differs from release version; rebuild with make release'
/usr/bin/codesign --verify --strict "$app_path"
/usr/bin/ditto "$app_path" "$stage/racomi.app"
cp "$project_dir/build/macos/INSTALL.txt" "$stage/INSTALL.txt"
release_dir="$project_dir/bin/releases"
mkdir -p "$release_dir"
archive_name="racomi-$version-$archive_arch-macos.zip"
archive_path="$release_dir/$archive_name"
# ditto preserves app permissions, symlinks and macOS resources. Archive the
# directory contents so racomi.app and INSTALL.txt sit at the ZIP root.
/usr/bin/ditto -c -k --sequesterRsrc "$stage" "$release_dir/.$archive_name.tmp"
mv -f "$release_dir/.$archive_name.tmp" "$archive_path"
(cd "$release_dir" && /usr/bin/shasum -a 256 "$archive_name" > "$archive_name.sha256")
printf 'Release archive: %s\nChecksum: %s.sha256\n' "$archive_path" "$archive_path"
