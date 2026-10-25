param(
    [ValidateSet('dev', 'stable', 'preview')][string]$Channel = 'dev',
    [ValidateSet('local', 'ci')][string]$Executor = 'local',
    [string]$BuildNumber = ''
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$expectedNumber = $BuildNumber
. "$PSScriptRoot/windows.ps1" -Channel $Channel
Add-Type @'
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class RacomiPEResources {
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern IntPtr LoadLibraryEx(string path, IntPtr file, uint flags);
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern IntPtr FindResource(IntPtr module, IntPtr name, IntPtr type);
    [DllImport("kernel32.dll")] static extern uint SizeofResource(IntPtr module, IntPtr resource);
    [DllImport("kernel32.dll")] static extern IntPtr LoadResource(IntPtr module, IntPtr resource);
    [DllImport("kernel32.dll")] static extern IntPtr LockResource(IntPtr resource);
    [DllImport("kernel32.dll")] static extern bool FreeLibrary(IntPtr module);
    public static string Manifest(string path) {
        IntPtr module=LoadLibraryEx(path,IntPtr.Zero,2); // Data only: never execute the DLL entrypoint.
        if(module==IntPtr.Zero) throw new Exception("Cannot open PE resources");
        try {
            IntPtr resource=FindResource(module,new IntPtr(1),new IntPtr(24));
            if(resource==IntPtr.Zero) throw new Exception("Missing application manifest");
            byte[] bytes=new byte[SizeofResource(module,resource)];
            Marshal.Copy(LockResource(LoadResource(module,resource)),bytes,0,bytes.Length);
            if(FindResource(module,new IntPtr(1),new IntPtr(14))==IntPtr.Zero) throw new Exception("Missing application icon");
            return Encoding.UTF8.GetString(bytes).TrimEnd('\0');
        } finally { FreeLibrary(module); }
    }
}
'@
$version = (Get-Content frontend/package.json -Raw | ConvertFrom-Json).version
$zip = Join-Path $root "bin/releases/racomi-$version-x86_64-windows.zip"
$digest = (Get-Content -LiteralPath "$zip.sha256" -Raw).Split(' ')[0]
if ((Get-SHA256 $zip) -cne $digest) { throw 'Archive checksum mismatch' }
$stage = Join-Path $root ('bin/windows/artifact-' + [guid]::NewGuid())
try {
    Expand-Archive -LiteralPath $zip -DestinationPath $stage
    $files = @((Get-ChildItem -LiteralPath $stage).Name | Sort-Object)
    if (($files -join ',') -cne 'INSTALL.txt,racomi.exe,THIRD_PARTY_NOTICES.txt') { throw 'Unexpected ZIP contents' }
    $exe = Join-Path $stage 'racomi.exe'
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo.FileName = $exe
    $process.StartInfo.Arguments = '--build-info'
    $process.StartInfo.UseShellExecute = $false
    $process.StartInfo.RedirectStandardOutput = $true
    $process.StartInfo.CreateNoWindow = $true
    if (!$process.Start()) { throw 'Unable to start packaged executable' }
    $outputTask = $process.StandardOutput.ReadToEndAsync()
    if (!$process.WaitForExit(15000)) { $process.Kill(); throw 'Packaged executable did not exit' }
    if ($process.ExitCode -ne 0) { throw 'Packaged executable failed' }
    $output = $outputTask.GetAwaiter().GetResult()
    $process.Dispose()
    $info = $output | ConvertFrom-Json
    if ($info.version -cne $version -or $info.channel -cne $Channel -or $info.executor -cne $Executor -or $info.os -cne 'windows' -or $info.arch -cne 'amd64' -or $info.commit -cne (& git rev-parse HEAD)) { throw 'Packaged runtime identity mismatch' }
    if ($Executor -eq 'ci' -and $info.dirty) { throw 'Dirty CI artifact' }
    if ($expectedNumber -and $info.number -ne $expectedNumber) { throw 'Build number mismatch' }
    $expectedName = if ($Channel -eq 'dev') { 'racomi Dev' } else { 'racomi' }
    $expectedID = if ($Channel -eq 'dev') { 'com.knothhe.racomi.dev' } else { 'com.knothhe.racomi' }
    if ($info.name -cne $expectedName -or $info.id -cne $expectedID) { throw 'Channel identity mismatch' }
    $resource = (Get-Item -LiteralPath $exe).VersionInfo
    if ($resource.ProductVersion -cne $version -or $resource.ProductName -cne $expectedName) { throw 'PE version resource mismatch' }
    [xml]$manifest = [RacomiPEResources]::Manifest($exe)
    if ($manifest.SelectSingleNode("//*[local-name()='longPathAware']").InnerText -ne 'true' -or $manifest.SelectSingleNode("//*[local-name()='dpiAwareness']").InnerText -notmatch 'PerMonitorV2' -or $manifest.SelectSingleNode("//*[local-name()='requestedExecutionLevel']").GetAttribute('level') -ne 'asInvoker') { throw 'Embedded manifest lost long-path, DPI or normal-user settings' }
    $imports = & objdump -p $exe
    if ($LASTEXITCODE -ne 0) { throw 'Unable to inspect PE imports' }
    if (($imports -join "`n") -match '(?i)DLL Name:.*(libgcc|libstdc\+\+|libwinpthread|msys-)') { throw 'Executable requires a MinGW/MSYS DLL' }
    Write-Host "Windows artifact verified: $version / $Channel / $Executor / $($info.number), checksum, identity, PE resources and static runtime."
} finally {
    $resolved = [System.IO.Path]::GetFullPath($stage)
    $parent = [System.IO.Path]::GetFullPath((Join-Path $root 'bin/windows')).TrimEnd('\') + '\'
    if (!$resolved.StartsWith($parent, [System.StringComparison]::OrdinalIgnoreCase) -or (Split-Path $resolved -Leaf) -notlike 'artifact-*') { throw "Unsafe cleanup: $resolved" }
    if (Test-Path -LiteralPath $resolved) { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
