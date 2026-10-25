#!/bin/sh
# Write the knothhe/homebrew-tap cask for an exact published macOS archive.
# Usage: homebrew-cask.sh VERSION SHA256 CASK_FILE
# Never rolls an existing cask back to an older version.
set -eu
version=$1
sha256=$2
cask=$3
printf '%s\n' "$sha256" | grep -Eqx '[0-9a-f]{64}' || { echo "Invalid SHA-256: $sha256" >&2; exit 1; }
if [ -f "$cask" ]; then
  current=$(sed -n 's/^  version "\(.*\)"$/\1/p' "$cask")
  if [ "$current" = "$version" ]; then
    grep -qx "  sha256 \"$sha256\"" "$cask" || { echo "Published checksum changed for cask $version" >&2; exit 1; }
  elif [ "$(printf '%s\n%s\n' "$current" "$version" | sort -V | tail -n 1)" = "$current" ]; then
    echo "Cask already at newer $current; keeping it" >&2
    exit 0
  fi
fi
mkdir -p "$(dirname "$cask")"
cat > "$cask" <<EOF
cask "racomi" do
  version "$version"
  sha256 "$sha256"

  url "https://github.com/knothhe/racomi/releases/download/v#{version}/racomi-#{version}-aarch64-macos.zip"
  name "racomi"
  desc "Local Agent Skill manager"
  homepage "https://github.com/knothhe/racomi"

  livecheck do
    url :url
    strategy :github_latest
  end

  auto_updates true
  depends_on arch: :arm64
  depends_on macos: :ventura

  app "racomi.app"

  uninstall quit: "com.knothhe.racomi"
end
EOF
