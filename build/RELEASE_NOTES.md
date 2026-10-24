## macOS Apple Silicon

Requires macOS 13 or newer. Download `racomi-*-aarch64-macos.zip` and the
matching `.sha256`. Verify with `shasum -a 256 -c racomi-*-macos.zip.sha256`,
then unzip and drag racomi.app to Applications or ~/Applications.

Official stable builds include signed Sparkle updates in Settings → About.
Older versions need one manual installation to receive the updater.
Dev builds use a separate identity and data directory, and do not connect to
the stable update feed. Intel Macs are outside this CI release.

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

## Windows x64

Requires Windows 10/11 x64 and Microsoft Edge WebView2 Runtime. Download
`racomi-*-x86_64-windows.zip` and its matching `.sha256`, compare it with
`Get-FileHash <ZIP> -Algorithm SHA256`, extract, and run `racomi.exe` from a
writable user folder. Git is needed for Git sources. No MinGW DLLs are needed.

Lightweight mode reopens from the system tray. Open at login uses a per-user
registration; Dev and release installations have separate data and identities.
Official stable builds verify Ed25519-signed ZIPs and release identity before
saving changes, exiting, replacing the EXE with a backup, and restarting.
The EXE is not Authenticode-signed. The ZIP includes installation guidance.

Preview releases on all platforms update manually and do not connect to the
stable update feed.

Updates and uninstalling keep user data and deployed skills.
