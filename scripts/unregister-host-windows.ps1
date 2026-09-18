# Unregister SheepGet Native Messaging Host on Windows

$ErrorActionPreference = "SilentlyContinue"

$registryKeys = @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.sheepget.host",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\com.sheepget.host"
)

foreach ($key in $registryKeys) {
    if (Test-Path $key) {
        Remove-Item -Path $key -Recurse -Force
        Write-Host "Removed registry key: $key"
    }
}

Write-Host "Native Messaging Host unregistered successfully."
