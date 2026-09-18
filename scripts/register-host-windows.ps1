# Register SheepGet Native Messaging Host for Chrome and Edge on Windows
param (
    [string]$HostExePath = ""
)

$ErrorActionPreference = "Stop"

if (-not $HostExePath) {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
    $defaultCandidate = Join-Path $scriptDir "..\build\bin\sheepget-host.exe"
    if (Test-Path $defaultCandidate) {
        $HostExePath = (Resolve-Path $defaultCandidate).Path
    } else {
        Write-Error "sheepget-host.exe not found. Please build it first: go build -o build/bin/sheepget-host.exe ./cmd/sheepget-host"
    }
}

$HostExePath = (Resolve-Path $HostExePath).Path
$hostDir = Split-Path -Parent $HostExePath
$manifestPath = Join-Path $hostDir "com.sheepget.host.json"

# Pinned 32-character extension ID
$extensionId = "oediboaeofmnlkgcjhnpfnngphkjooam"

$manifest = @{
    name = "com.sheepget.host"
    description = "SheepGet Native Messaging Host"
    path = $HostExePath
    type = "stdio"
    allowed_origins = @(
        "chrome-extension://$extensionId/"
    )
}

$manifestJson = $manifest | ConvertTo-Json -Depth 4
[System.IO.File]::WriteAllText($manifestPath, $manifestJson, [System.Text.Encoding]::UTF8)
Write-Host "Created manifest: $manifestPath"

$registryKeys = @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.sheepget.host",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\com.sheepget.host"
)

foreach ($key in $registryKeys) {
    if (-not (Test-Path $key)) {
        New-Item -Path $key -Force | Out-Null
    }
    Set-ItemProperty -Path $key -Name "(Default)" -Value $manifestPath
    Write-Host "Registered in registry: $key"
}

Write-Host "Native Messaging Host successfully registered for SheepGet!"
