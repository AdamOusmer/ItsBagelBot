# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

param(
    [Parameter(Mandatory=$true)][string]$Root,
    [Parameter(Mandatory=$true)][string]$BinDir,
    [Parameter(Mandatory=$true)][string]$AppName,
    [Parameter(Mandatory=$true)][ValidateSet('amd64','arm64')][string]$Arch,
    [string]$Certificate = '',
    [string]$Publisher = '',
    [switch]$UseMsixTool
)
$ErrorActionPreference = 'Stop'

function Find-SdkTool([string]$Name) {
    $tool = Get-Command $Name -ErrorAction SilentlyContinue
    if ($tool) { return $tool.Source }
    $sdk = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $hostArch = $env:PROCESSOR_ARCHITECTURE.ToLowerInvariant().Replace('amd64','x64')
    $versions = @(Get-ChildItem $sdk -Directory -Filter '10.*' | Sort-Object Name -Descending)
    foreach ($version in $versions) {
        $candidate = Join-Path $version.FullName "$hostArch\$Name"
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate }
    }
    throw "$Name requires the Windows SDK (or an explicit PATH entry)."
}

function Invoke-Checked([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

$binaryDirectory = Join-Path $Root $BinDir
$worker = Join-Path $binaryDirectory 'BagelNetworkWorker.exe'
if (!(Test-Path -LiteralPath $worker -PathType Leaf)) { throw 'Missing isolated network worker.' }
$makeAppx = Find-SdkTool 'MakeAppx.exe'
$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ('bagel-msix-' + [Guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $temporary
try {
    $basePackage = Join-Path $temporary 'base.msix'
    $arguments = @('tool','msix','--config',(Join-Path $Root 'build/config.yml'),
        '--name',"$AppName.exe",'--executable',(Join-Path $binaryDirectory "$AppName.exe"),
        '--arch',$Arch,'--out',$basePackage)
    if ($Publisher) { $arguments += @('--publisher',$Publisher) }
    if ($UseMsixTool) { $arguments += '--use-msix-tool' } else { $arguments += '--use-makeappx' }
    # Preserve Wails metadata, icons and manifest, then add the companion before
    # final signing. Its legacy MSIX builder packages only the main executable.
    Invoke-Checked 'wails3' $arguments
    $expanded = Join-Path $temporary 'expanded'
    Invoke-Checked $makeAppx @('unpack','/p',$basePackage,'/d',$expanded,'/o')
    Copy-Item -LiteralPath $worker -Destination (Join-Path $expanded 'BagelNetworkWorker.exe')
    foreach ($generated in @('AppxBlockMap.xml','AppxSignature.p7x','[Content_Types].xml')) {
        $file = Join-Path $expanded $generated
        if (Test-Path -LiteralPath $file) { Remove-Item -LiteralPath $file }
    }
    $package = Join-Path $temporary 'complete.msix'
    Invoke-Checked $makeAppx @('pack','/d',$expanded,'/p',$package,'/o')
    if ($Certificate) {
        $signTool = Find-SdkTool 'signtool.exe'
        Invoke-Checked $signTool @('sign','/fd','SHA256','/a','/f',$Certificate,$package)
    }
    Move-Item -LiteralPath $package -Destination (Join-Path $binaryDirectory "$AppName-$Arch.msix") -Force
} finally {
    Remove-Item -LiteralPath $temporary -Recurse -Force
}
