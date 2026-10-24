## macOS Apple Silicon

Requires macOS 13 or newer. Download `racomi-*-aarch64-macos.zip` and the
matching `.sha256`. Verify with `shasum -a 256 -c racomi-*-macos.zip.sha256`,
then unzip and drag racomi.app to Applications or ~/Applications.

Official stable builds include signed Sparkle updates in Settings → About.
Older versions need one manual installation to receive the updater.
Dev builds use a separate identity and data directory, and do not connect to
the stable update feed. Windows and Intel are outside this CI release.

The host app uses an ad-hoc signature without Apple notarization. Confirm the
official download and checksum before choosing System Settings → Privacy &
Security → Open Anyway. The ZIP includes detailed installation guidance.

## Linux x86_64

Download `racomi-*-x86_64-linux.tar.gz` and the matching `.sha256`. It needs
GTK 3 and WebKit2GTK 4.1 (Arch `webkit2gtk-4.1`, Debian/Ubuntu
`libwebkit2gtk-4.1-0`, Fedora `webkit2gtk4.1`). Verify with
`sha256sum -c racomi-*-linux.tar.gz.sha256`, extract, and place `racomi` in a
folder you can write, such as ~/.local/bin. Stable builds check signed updates
in Settings → About and replace the executable in place.

Updates and uninstalling keep user data and deployed skills.
