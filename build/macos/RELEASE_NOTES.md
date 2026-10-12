Requires **macOS 13 or newer**. Download the ZIP for your Mac:

| Mac | Download |
| --- | --- |
| Apple Silicon (M series) | `racomi-*-aarch64-macos.zip` |
| Intel | `racomi-*-x86_64-macos.zip` |

Download the matching `.sha256` file and run `shasum -a 256 -c racomi-*-macos.zip.sha256` in your download directory. Unzip and drag **racomi.app** into **Applications** (or `~/Applications`). Quit the old version before replacing it. Existing data and deployed skills are kept. Git is needed to import and refresh Git sources.

These packages use a local ad-hoc signature; they have no Developer ID signature or Apple notarization. macOS may report “cannot be verified” or “is damaged and can't be opened”. Confirm the official download and checksum first, then open the app once and choose **System Settings → Privacy & Security → Open Anyway**. For a trusted download still blocked by quarantine, remove that mark once:

```sh
xattr -dr com.apple.quarantine /Applications/racomi.app
# If installed in ~/Applications:
xattr -dr com.apple.quarantine "$HOME/Applications/racomi.app"
```

See [Apple's guidance](https://support.apple.com/en-us/102445). Each ZIP includes bilingual `INSTALL.txt` with installation and uninstall instructions.
