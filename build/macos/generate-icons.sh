#!/bin/sh
# The editable Icon Composer document is the single source for every app icon.
set -eu

case "${1:-all}" in
  all) ;;
  *) echo "usage: $0 [all]" >&2; exit 2 ;;
esac

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
native_icon="$project_dir/assets/macos/AppIcon.icon"
catalog_file="$project_dir/assets/macos/Assets.car"
light_icon="$project_dir/assets/appicon.png"
dark_icon="$project_dir/assets/appicon-dark.png"
icns_file="$project_dir/assets/macos/racomi.icns"

developer_dir=$(xcode-select -p 2>/dev/null || true)
ictool=${RACOMI_ICTOOL:-"$developer_dir/../Applications/Icon Composer.app/Contents/Executables/ictool"}
if [ ! -x "$ictool" ] || ! xcrun --find actool >/dev/null 2>&1; then
  # Retain compiled release assets on machines without a complete Xcode install.
  if [ -s "$catalog_file" ] && [ -s "$light_icon" ] && [ -s "$dark_icon" ] && [ -s "$icns_file" ]; then
    echo "Icon Composer or actool unavailable; keeping the existing compiled icons" >&2
    touch "$catalog_file" "$light_icon" "$dark_icon" "$icns_file"
    exit 0
  fi
  echo "Xcode with Icon Composer and actool is required to create missing icon assets" >&2
  exit 1
fi

# Stage on the output filesystem so each final rename is atomic.
temporary_dir=$(mktemp -d "$project_dir/build/macos/.racomi-icons.XXXXXX")
trap 'rm -rf "$temporary_dir"' 0

for rendition in Default Dark; do
  case "$rendition" in
    Default) icon_name=appicon ;;
    Dark) icon_name=appicon-dark ;;
  esac
  "$ictool" "$native_icon" --export-image --output-file "$temporary_dir/$icon_name-full.png" \
    --platform macOS --rendition "$rendition" --width 1024 --height 1024 --scale 1
  # Composer exports a full-canvas tile. Padding before resizing gives the
  # legacy ICNS and runtime PNG the same 10% clear margin as macOS app icons.
  sips -p 1280 1280 "$temporary_dir/$icon_name-full.png" --out "$temporary_dir/$icon_name-padded.png" >/dev/null
  sips -z 1024 1024 "$temporary_dir/$icon_name-padded.png" --out "$temporary_dir/$icon_name.png" >/dev/null
done

iconset_dir="$temporary_dir/racomi.iconset"
mkdir -p "$iconset_dir"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$temporary_dir/appicon.png" --out "$iconset_dir/icon_${size}x${size}.png" >/dev/null
  retina_size=$((size * 2))
  sips -z "$retina_size" "$retina_size" "$temporary_dir/appicon.png" --out "$iconset_dir/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset_dir" -o "$temporary_dir/racomi.icns"

catalog_dir="$temporary_dir/catalog"
mkdir -p "$catalog_dir"
xcrun actool "$native_icon" --compile "$catalog_dir" --platform macosx \
  --minimum-deployment-target "${MACOSX_DEPLOYMENT_TARGET:-13.0}" \
  --app-icon AppIcon --output-partial-info-plist "$catalog_dir/partial.plist"
if [ ! -s "$catalog_dir/Assets.car" ]; then
  echo "actool did not produce an appearance catalog" >&2
  exit 1
fi

# Replace each destination only after every renderer has completed successfully.
mv "$catalog_dir/Assets.car" "$catalog_file"
mv "$temporary_dir/appicon.png" "$light_icon"
mv "$temporary_dir/appicon-dark.png" "$dark_icon"
mv "$temporary_dir/racomi.icns" "$icns_file"
# Secondary Make targets must be at least as new as their catalog prerequisite.
touch "$light_icon" "$dark_icon" "$icns_file"
