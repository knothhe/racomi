$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Import-Module Microsoft.PowerShell.Utility -Global
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('racomi-install-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $testRoot | Out-Null
try {
    . "$PSScriptRoot/windows.ps1" -AppDir (Join-Path $testRoot 'Apps with spaces/racomi') -ShortcutPath (Join-Path $testRoot 'Start Menu/racomi.lnk')
    if (!(Test-Path bin/racomi.exe)) { Build-App }
    $expected = Get-SHA256 (Join-Path $root 'bin/racomi.exe')
    Install-App
    $paths = Get-InstallPaths
    if ((Get-SHA256 $paths.Exe) -ne $expected) { throw 'Installed executable differs.' }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($ShortcutPath)
    if ($shortcut.TargetPath -ne $paths.Exe) { throw 'Wrong shortcut target.' }
    # Upgrades must replace atomically, preserving adjacent user files.
    $sentinel = Join-Path $paths.Directory 'keep.txt'
    Set-Content -LiteralPath $sentinel -Value 'keep'
    Install-App
    # Windows denies replacing an executable opened without delete sharing.
    $locked = [System.IO.File]::Open($paths.Exe, 'Open', 'Read', 'Read')
    try {
        $rejected = $false
        try { Install-App } catch { $rejected = $true }
        if (!$rejected) { throw 'Expected replacement of a locked file to fail.' }
    } finally { $locked.Dispose() }
    if ((Get-SHA256 $paths.Exe) -ne $expected) { throw 'Failed upgrade damaged the previous executable.' }
    Uninstall-App
    Uninstall-App # Idempotent removal.
    if ((Test-Path -LiteralPath $paths.Exe) -or (Test-Path -LiteralPath $ShortcutPath)) { throw 'Uninstall left app/shortcut behind.' }
    if (!(Test-Path -LiteralPath $sentinel)) { throw 'Uninstall removed unrelated files.' }
    # An unrelated file named racomi.exe must never be overwritten or deleted.
    Set-Content -LiteralPath $paths.Exe -Value 'unrelated'
    foreach ($operation in @('Install-App', 'Uninstall-App')) {
        $rejected = $false
        try { & $operation } catch { $rejected = $true }
        if (!$rejected) { throw "$operation accepted an unrelated executable." }
    }
    Write-Host 'Windows installer tests passed (install, upgrade, locked file, uninstall, unrelated files).'
} finally {
    $resolved = [System.IO.Path]::GetFullPath($testRoot)
    $temporary = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    if (!$resolved.StartsWith($temporary, [System.StringComparison]::OrdinalIgnoreCase) -or (Split-Path $resolved -Leaf) -notlike 'racomi-install-*') { throw "Unsafe test cleanup path: $resolved" }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
