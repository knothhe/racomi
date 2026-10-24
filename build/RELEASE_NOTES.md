## macOS Apple Silicon

Requires macOS 13 or newer. Download `racomi-*-aarch64-macos.zip` and the
matching `.sha256`. Verify with `shasum -a 256 -c racomi-*-macos.zip.sha256`,
then unzip and drag racomi.app to Applications or ~/Applications.

Official stable builds include signed Sparkle updates in Settings → About.
Older versions need one manual installation to receive the updater.
Dev builds use a separate identity and data directory, and do not connect to
the stable update feed. Windows, Linux and Intel are outside this CI release.

The host app uses an ad-hoc signature without Apple notarization. Confirm the
official download and checksum before choosing System Settings → Privacy &
Security → Open Anyway. The ZIP includes detailed installation guidance.

Updates and uninstalling keep user data and deployed skills.
