## Windows x64

Download `racomi-*-x86_64-windows.zip` and its `.sha256` file. Requires Windows
10/11 x64 and Microsoft Edge WebView2 Runtime. Compare the checksum with
`Get-FileHash <ZIP path> -Algorithm SHA256`, extract the ZIP, then open
`racomi.exe`. The Windows EXE is unsigned. Exit the app before replacing it.
Each ZIP includes installation instructions. Git is needed for Git sources.

## macOS

Requires macOS 13 or newer. Download `racomi-*-aarch64-macos.zip` for Apple
Silicon or `racomi-*-x86_64-macos.zip` for Intel, and the matching `.sha256`.
Verify with `shasum -a 256 -c racomi-*-macos.zip.sha256`, then unzip and drag
racomi.app to Applications or ~/Applications.

The Mac packages use an ad-hoc signature without Apple notarization. Confirm
the official download and checksum before choosing System Settings → Privacy
& Security → Open Anyway. Each Mac ZIP includes detailed Gatekeeper guidance.

Updates and uninstalling keep user data and deployed skills on both platforms.
