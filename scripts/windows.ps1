param(
    [ValidateSet('help', 'doctor', 'deps', 'bootstrap', 'icons', 'frontend', 'frontend-dev', 'dev', 'check', 'test', 'test-install', 'build', 'package', 'dist', 'install', 'uninstall', 'run')]
    [string]$Action = 'help',
    [string]$Go = 'go', [string]$Npm = 'npm.cmd', [string]$Node = 'node',
    [string]$CC = 'gcc', [string]$Windres = 'windres', [string]$Arch = 'amd64',
    [string]$AppDir = '',
    [string]$Version = '', [string]$BuildNumber = '1', [int]$Port = 9245,
    [ValidateSet('dev', 'stable', 'preview')][string]$Channel = 'dev',
    [string]$ShortcutPath = ''
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Import-Module Microsoft.PowerShell.Utility -Global
$root = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $root
$appName = if ($Channel -eq 'dev') { 'racomi Dev' } else { 'racomi' }
$installID = if ($Channel -eq 'dev') { 'com.knothhe.racomi.dev' } else { 'com.knothhe.racomi' }
$windowsRunKey = 'Software\Microsoft\Windows\CurrentVersion\Run'
if (!$AppDir) { $AppDir = Join-Path $env:LOCALAPPDATA $(if ($Channel -eq 'dev') { 'Programs/racomi-dev' } else { 'Programs/racomi' }) }
if (!$ShortcutPath) { $ShortcutPath = Join-Path $env:APPDATA "Microsoft/Windows/Start Menu/Programs/$appName.lnk" }

function Invoke-Tool([string]$Command, [string[]]$Arguments) {
    # Windows PowerShell's legacy native argument marshaller drops embedded quotes.
    # PowerShell 7's standard marshaller preserves them itself.
    if ($PSVersionTable.PSVersion.Major -lt 7) { $Arguments = @($Arguments | ForEach-Object { $_.Replace('"', '\"') }) }
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Command failed (exit $LASTEXITCODE)" }
}

function Test-Tools {
    if ($env:OS -ne 'Windows_NT') { throw 'This script requires Windows.' }
    if ($Arch -ne 'amd64') { throw 'Windows builds currently support GOARCH=amd64 only (use an x86_64 MinGW-w64 toolchain).' }
    foreach ($command in @($Go, $Npm, $Node, $CC, $Windres, 'git')) {
        if (!(Get-Command $command -ErrorAction SilentlyContinue)) { throw "Missing build tool: $command. See README.md (Windows source builds)." }
    }
    $required = [regex]::Match((Get-Content go.mod -Raw), '(?m)^go (\S+)').Groups[1].Value
    $actual = (& $Go env GOVERSION) -replace '^go', ''
    if ($LASTEXITCODE -ne 0 -or [version]$actual -lt [version]$required) { throw "Go $required or newer is required." }
    $nodeVersion = (& $Node --version) -replace '^v', ''
    if ($LASTEXITCODE -ne 0 -or [version]$nodeVersion -lt [version]'22.12.0') { throw 'Node.js 22.12 or newer is required.' }
    $triple = & $CC -dumpmachine
    if ($LASTEXITCODE -ne 0 -or $triple -notmatch '^x86_64-w64-mingw32') { throw "Expected x86_64 MinGW-w64 GCC, found: $triple" }
    Write-Host "Build tools ready: Go $actual, Node $(& $Node --version), $triple."
}

function Set-BuildEnvironment {
    $env:GOOS = 'windows'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '1' # go-sqlite3 must not fall back to its non-CGO stub.
    $env:CC = $CC
}

function Invoke-GoTests([string]$Pattern) {
    # Go normally strips transient test executables, unlike go test -c. On
    # this Windows host SAC blocked those binaries, while tests retaining
    # symbols and DWARF ran successfully. Keep test builds debuggable without
    # changing Windows security policy or excluding any test packages.
    Invoke-Tool $Go @('test', '-count=1', '-timeout', '5m', '-ldflags', '-s=false -w=false', $Pattern)
}

function Install-Dependencies {
    Test-Tools
    Invoke-Tool $Npm @('ci', '--prefix', 'frontend')
    Invoke-Tool $Go @('mod', 'download')
}

function Build-Frontend {
    # The web icon is checked in; never invoke macOS sips/actool on Windows.
    Invoke-Tool $Npm @('run', 'build', '--prefix', 'frontend')
}

function Get-SHA256([string]$Path) {
    $stream = [System.IO.File]::OpenRead($Path)
    $hasher = [System.Security.Cryptography.SHA256]::Create()
    try { return [System.BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-', '').ToLowerInvariant() }
    finally { $hasher.Dispose(); $stream.Dispose() }
}

function New-WindowsResources {
    if (!$script:Version) { $script:Version = (Get-Content frontend/package.json -Raw | ConvertFrom-Json).version }
    if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$') { throw "Invalid version: $Version" }
    $parts = @([int]$Matches[1], [int]$Matches[2], [int]$Matches[3])
    if ($BuildNumber -notmatch '^\d+$' -or [long]$BuildNumber -gt 65535 -or ($parts | Where-Object { $_ -gt 65535 })) { throw 'Windows version components and BUILD_NUMBER must be in 0..65535.' }
    $numericVersion = ($parts + [int]$BuildNumber) -join ','
    New-Item -ItemType Directory -Force bin/windows | Out-Null
    # ICO directory entries contain PNG images, supported since Windows Vista.
    Add-Type -AssemblyName System.Drawing
    $iconSource = if ($Channel -eq 'dev') { 'build/dev-icons/app-icon-dev.png' } else { 'build/appicon.png' }
    $source = [System.Drawing.Image]::FromFile((Join-Path $root $iconSource))
    try {
        $images = @(foreach ($size in @(16, 32, 48, 64, 128, 256)) {
            $bitmap = New-Object System.Drawing.Bitmap($size, $size)
            $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
            $stream = New-Object System.IO.MemoryStream
            try {
                $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
                $graphics.DrawImage($source, 0, 0, $size, $size)
                $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
                [pscustomobject]@{ Size = $size; Bytes = $stream.ToArray() }
            } finally { $graphics.Dispose(); $bitmap.Dispose(); $stream.Dispose() }
        })
        $file = [System.IO.File]::Create((Join-Path $root 'bin/windows/racomi.ico'))
        $writer = New-Object System.IO.BinaryWriter($file)
        try {
            $writer.Write([uint16]0); $writer.Write([uint16]1); $writer.Write([uint16]$images.Count)
            $offset = 6 + 16 * $images.Count
            foreach ($image in $images) {
                $dimension = $image.Size % 256
                $writer.Write([byte]$dimension); $writer.Write([byte]$dimension)
                $writer.Write([uint16]0); $writer.Write([uint16]1); $writer.Write([uint16]32)
                $writer.Write([uint32]$image.Bytes.Length); $writer.Write([uint32]$offset)
                $offset += $image.Bytes.Length
            }
            foreach ($image in $images) { $writer.Write([byte[]]$image.Bytes) }
        } finally { $writer.Dispose(); $file.Dispose() }
    } finally { $source.Dispose() }
    # Keep resource references relative and ASCII so non-ASCII checkout paths
    # are never lossy-converted when writing the RC file for windres.
    $manifest = 'build/windows/racomi.manifest'
    $icon = 'bin/windows/racomi.ico'
    @"
1 ICON "$icon"
1 24 "$manifest"
1 VERSIONINFO
FILEVERSION $numericVersion
PRODUCTVERSION $numericVersion
FILEOS 0x40004
FILETYPE 1
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904b0"
    BEGIN
      VALUE "FileDescription", "$appName"
      VALUE "FileVersion", "$Version.$BuildNumber"
      VALUE "InternalName", "racomi"
      VALUE "OriginalFilename", "racomi.exe"
      VALUE "ProductName", "$appName"
      VALUE "ProductVersion", "$Version"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x0409, 1200
  END
END
"@ | Set-Content -Encoding ascii bin/windows/racomi.rc
}

function Build-App {
    Test-Tools
    Set-BuildEnvironment
    if (!$script:Version) { $script:Version = (Get-Content frontend/package.json -Raw | ConvertFrom-Json).version }
    Invoke-Tool $Node @('--input-type=module', '-e', 'import {parse} from "./scripts/release.mjs"; parse(process.argv[1])', $Version)
    $executor = if ($env:BUILD_EXECUTOR) { $env:BUILD_EXECUTOR } else { 'local' }
    if ($executor -notin @('local', 'ci')) { throw 'Invalid BUILD_EXECUTOR' }
    $commit = (& git rev-parse HEAD)
    if ($LASTEXITCODE -ne 0) { throw 'Unable to read source commit.' }
    $status = & git status --porcelain --untracked-files=normal
    if ($LASTEXITCODE -ne 0) { throw 'Unable to inspect source state.' }
    $dirty = if ($status) { 'true' } else { 'false' }
    if ($executor -eq 'ci' -and ($Channel -eq 'dev' -or $dirty -ne 'false')) { throw 'CI release requires a clean release identity' }
    $feed = ''; $key = ''
    if ($Channel -eq 'stable') {
        $updates = Get-Content build/updates.json -Raw | ConvertFrom-Json
        $feed = $updates.windowsFeedURL; $key = $updates.publicKey
    }
    Build-Frontend
    New-WindowsResources
    # Go automatically links architecture-specific resources from the package directory.
    $resourceName = "racomi_windows_$Arch.syso"
    $resource = Join-Path $root $resourceName
    if (Test-Path -LiteralPath $resource) { throw "Resource already exists: $resource" }
    try {
        Invoke-Tool $Windres @('--target=pe-x86-64', '-i', 'bin/windows/racomi.rc', '-o', $resourceName, '-O', 'coff')
        # Newer MinGW versions insert their own manifest. Suppress that default
        # when linking our manifest, so the EXE has a single unambiguous resource.
        $specs = (& $CC -dumpspecs) -join "`n"
        if ($LASTEXITCODE -ne 0) { throw 'Unable to read GCC specs.' }
        $specs = $specs.Replace('%{!shared:%:if-exists(default-manifest.o%s)}', '')
        [System.IO.File]::WriteAllText((Join-Path $root 'bin/windows/gcc.specs'), $specs + "`n", [System.Text.Encoding]::ASCII)
        $flags = '-s -w -H windowsgui -extldflags "-static -specs=bin/windows/gcc.specs"'
        foreach ($entry in @("Version=$Version", "Number=$BuildNumber", "Commit=$commit", "Dirty=$dirty", "Channel=$Channel", "Executor=$executor", "FeedURL=$feed", "PublicKey=$key")) {
            $flags += " -X racomi/internal/buildinfo.$entry"
        }
        Invoke-Tool $Go @('build', '-trimpath', '-tags', 'production', '-ldflags', $flags, '-o', 'bin/racomi.exe', '.')
    } finally {
        if (Test-Path -LiteralPath $resource) { Remove-Item -LiteralPath $resource }
    }
    Write-Host "Built $root/bin/racomi.exe"
}

function Get-InstallPaths {
    if (![System.IO.Path]::IsPathRooted($AppDir) -or ![System.IO.Path]::IsPathRooted($ShortcutPath)) { throw 'APP_DIR and ShortcutPath must be absolute paths.' }
    $directory = [System.IO.Path]::GetFullPath($AppDir)
    # Reject reparse points in the entire destination chain before writing/removing files.
    foreach ($path in @($directory, (Split-Path $ShortcutPath -Parent))) {
        for ($current = $path; $current; $current = Split-Path $current -Parent) {
            if ((Test-Path -LiteralPath $current) -and ((Get-Item -LiteralPath $current -Force).Attributes -band [System.IO.FileAttributes]::ReparsePoint)) { throw "Install path contains a reparse point: $current" }
        }
    }
    $exe = Join-Path $directory 'racomi.exe'
    $marker = Join-Path $directory '.racomi-install'
    if (Test-Path -LiteralPath $exe) {
        $item = Get-Item -LiteralPath $exe -Force
        if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -or $item.VersionInfo.ProductName -ne $appName) { throw "Refusing to replace/remove an unrelated file: $exe" }
    }
    foreach ($path in @($marker, $ShortcutPath)) {
        if ((Test-Path -LiteralPath $path) -and ((Get-Item -LiteralPath $path -Force).Attributes -band [System.IO.FileAttributes]::ReparsePoint)) { throw "Refusing a reparse point: $path" }
    }
    if ((Test-Path -LiteralPath $marker) -and (Get-Content -LiteralPath $marker -Raw) -ne $installID) { throw "Unrelated install marker: $marker" }
    if (Test-Path -LiteralPath $ShortcutPath) {
        $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($ShortcutPath)
        if ($shortcut.TargetPath -ne $exe) { throw "Unrelated shortcut: $ShortcutPath" }
    }
    return @{ Directory = $directory; Exe = $exe; Marker = $marker }
}

function Install-App {
    $paths = Get-InstallPaths
    New-Item -ItemType Directory -Force -Path $paths.Directory | Out-Null
    # A failed copy (e.g. a running app) leaves the previous executable intact.
    $stage = Join-Path $paths.Directory ('.racomi-' + [guid]::NewGuid() + '.exe')
    try {
        Copy-Item -LiteralPath (Join-Path $root 'bin/racomi.exe') -Destination $stage
        if (Test-Path -LiteralPath $paths.Exe) { [System.IO.File]::Replace($stage, $paths.Exe, [System.Management.Automation.Language.NullString]::Value) }
        else { Move-Item -LiteralPath $stage -Destination $paths.Exe }
    } finally { if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage } }
    [System.IO.File]::WriteAllText($paths.Marker, $installID)
    New-Item -ItemType Directory -Force -Path (Split-Path $ShortcutPath -Parent) | Out-Null
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($ShortcutPath)
    $shortcut.TargetPath = $paths.Exe
    $shortcut.WorkingDirectory = $paths.Directory
    $shortcut.IconLocation = $paths.Exe + ',0'
    $shortcut.Save()
    Write-Host "Installed $($paths.Exe)`nStart menu shortcut: $ShortcutPath"
}

function Uninstall-App {
    $paths = Get-InstallPaths
    # Only remove this channel's registration when it names this exact EXE.
    $login = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($windowsRunKey, $true)
    if ($login) {
        try {
            if ($login.GetValue($installID) -eq ('"' + $paths.Exe + '"')) { $login.DeleteValue($installID) }
        } finally { $login.Dispose() }
    }
    foreach ($file in @($paths.Exe, $paths.Marker, $ShortcutPath)) {
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
    }
    # Preserve unrelated files as well as user data and deployed skills.
    if ((Test-Path -LiteralPath $paths.Directory) -and !(Get-ChildItem -LiteralPath $paths.Directory -Force)) { Remove-Item -LiteralPath $paths.Directory }
    Write-Host 'Uninstalled racomi. User data and deployed skills have been kept.'
}

# Allow the installer regression tests to reuse the same functions without dispatch.
if ($MyInvocation.InvocationName -eq '.') { return }

try {
    switch ($Action) {
        'help' { Write-Host @'
doctor        Check Windows build tools (Go, Node/npm, Git, MinGW-w64 GCC/windres)
deps          Install locked dependencies (alias: bootstrap)
build         Build bin/racomi.exe (Windows x64, embedded icon and manifest)
package       Build the standalone Windows executable
dist          Build ZIP and SHA-256 in bin/releases
install       Install dependencies and selected channel (Dev by default)
uninstall     Remove installed executable and Start menu shortcut; preserve user data
frontend      Build frontend and check TypeScript
frontend-dev  Start Vite (VITE_PORT=9245)
dev           Run desktop shell against Vite
check         Frontend/installer/release tests, Go vet and all Go tests
test          Run Go internal tests
test-install  Verify install, replacement and uninstall in temporary directories
run           Build and launch the application
'@ }
        'doctor' { Test-Tools }
        { $_ -in 'deps', 'bootstrap' } { Install-Dependencies }
        'icons' { New-WindowsResources }
        'frontend' { Build-Frontend }
        'frontend-dev' { Invoke-Tool $Npm @('run', 'dev', '--prefix', 'frontend', '--', '--port', "$Port") }
        'dev' { Set-BuildEnvironment; Build-Frontend; $env:FRONTEND_DEVSERVER_URL = "http://127.0.0.1:$Port"; Invoke-Tool $Go @('run', '.') }
        'test' { Set-BuildEnvironment; Invoke-GoTests './internal/...' }
        'test-install' { & "$PSScriptRoot/windows.test.ps1" }
        'check' {
            Test-Tools; Set-BuildEnvironment; Build-Frontend
            Invoke-Tool $Npm @('test', '--prefix', 'frontend')
            & "$PSScriptRoot/windows.test.ps1"
            Invoke-Tool $Node @('--test', 'scripts/release.test.mjs', 'scripts/updates.test.mjs')
            Invoke-Tool $Go @('vet', './...')
            Invoke-GoTests './...'
        }
        { $_ -in 'build', 'package' } { Build-App }
        'dist' {
            Build-App
            $stage = Join-Path $root ('bin/windows/package-' + [guid]::NewGuid())
            New-Item -ItemType Directory -Path $stage | Out-Null
            try {
                Copy-Item bin/racomi.exe, build/windows/INSTALL.txt, frontend/public/THIRD_PARTY_NOTICES.txt -Destination $stage
                New-Item -ItemType Directory -Force bin/releases | Out-Null
                $zip = Join-Path $root "bin/releases/racomi-$Version-x86_64-windows.zip"
                Compress-Archive -Path "$stage/*" -DestinationPath $zip -Force
                $hash = Get-SHA256 $zip
                [System.IO.File]::WriteAllText("$zip.sha256", "$hash  $(Split-Path $zip -Leaf)`n")
                Write-Host "Release: $zip"
            } finally {
                # Delete only the exact files staged above, never recurse over a computed path.
                foreach ($file in @('racomi.exe', 'INSTALL.txt', 'THIRD_PARTY_NOTICES.txt')) {
                    $path = Join-Path $stage $file
                    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path }
                }
                Remove-Item -LiteralPath $stage
            }
        }
        'install' { Install-Dependencies; Build-App; Install-App }
        'uninstall' { Uninstall-App }
        'run' { Build-App; Start-Process -FilePath (Join-Path $root 'bin/racomi.exe') }
    }
} catch {
    Write-Error $_
    exit 1
}
