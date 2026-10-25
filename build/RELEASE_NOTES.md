## Downloads

| Platform | File | Requirements |
| --- | --- | --- |
| macOS Apple Silicon | `racomi-*-aarch64-macos.zip` | macOS 13+ |
| Linux x86_64 | `racomi-*-x86_64-linux.tar.gz` | GTK 3, WebKit2GTK 4.1 |
| Windows x64 | `racomi-*-x86_64-windows.zip` | Windows 10/11, WebView2 Runtime |

Verify each download with its `.sha256` file, then follow the `INSTALL.txt`
inside the package. Git is needed for Git sources.

- **macOS**: drag racomi.app to Applications. The app is ad-hoc signed and not
  notarized; if blocked, choose System Settings → Privacy & Security → Open Anyway.
- **Linux / Windows**: extract and run `racomi` / `racomi.exe` from a folder
  you can write, such as `~/.local/bin`. The Windows EXE is not Authenticode-signed.

Stable builds check signed updates in Settings → About. Preview releases update
manually. Updates and uninstalling keep user data and deployed skills.
