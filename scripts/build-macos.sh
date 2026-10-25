#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
channel=$1
version=$2
number=$3
architecture=$4
minimum=$5
executor=${BUILD_EXECUTOR:-local}
case "$channel" in dev|stable|preview) ;; *) echo 'invalid CHANNEL' >&2; exit 1 ;; esac
case "$executor" in local|ci) ;; *) echo 'invalid BUILD_EXECUTOR' >&2; exit 1 ;; esac
node --input-type=module -e 'import {parse} from "./scripts/release.mjs"; parse(process.argv[1])' "$version"
# Make captures source identity before generators touch tracked derived assets.
# Direct script callers fall back to the same source inspection here.
commit=${SOURCE_COMMIT:-unknown}
dirty=${SOURCE_DIRTY:-true}
if [ -z "${SOURCE_COMMIT:-}" ]; then
 if commit=$(git rev-parse HEAD 2>/dev/null); then
  if [ -z "$(git status --porcelain --untracked-files=normal)" ]; then dirty=false; fi
 else
  commit=unknown
 fi
fi
case "$dirty" in true|false) ;; *) echo 'invalid source state' >&2; exit 1 ;; esac
if [ "$executor" = ci ]; then
 [ "$channel" != dev ] && [ "$dirty" = false ] && [ "$commit" != unknown ] || { echo 'CI release requires a clean release identity' >&2; exit 1; }
fi
feed=''
key=''
if [ "$channel" = stable ]; then
 feed=$(node -p 'require("./build/updates.json").feedURL')
 key=$(node -p 'require("./build/updates.json").publicKey')
fi
mkdir -p "bin/$channel"
flags="-s -w -extldflags=-mmacosx-version-min=$minimum -X racomi/internal/buildinfo.Version=$version -X racomi/internal/buildinfo.Number=$number -X racomi/internal/buildinfo.Commit=$commit -X racomi/internal/buildinfo.Dirty=$dirty -X racomi/internal/buildinfo.Channel=$channel -X racomi/internal/buildinfo.Executor=$executor -X racomi/internal/buildinfo.FeedURL=$feed -X racomi/internal/buildinfo.PublicKey=$key"
oauth_client_id=${RACOMI_CLOUDFLARE_CLIENT_ID:-}
case "$oauth_client_id" in *[!A-Za-z0-9._-]*) echo 'invalid RACOMI_CLOUDFLARE_CLIENT_ID' >&2; exit 1 ;; esac
flags="$flags -X racomi/internal/desktop.cloudflareOAuthClientID=$oauth_client_id"
CGO_ENABLED=1 GOARCH="$architecture" "${GO:-go}" build -trimpath -tags production -ldflags="$flags" -o "bin/$channel/racomi" .
