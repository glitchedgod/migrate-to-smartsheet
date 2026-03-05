$ErrorActionPreference = "Stop"

$Repo = "glitchedgod/migrate-to-smartsheet"
$Binary = "migrate-to-smartsheet"

# Detect architecture
$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }

# Fetch latest release version
Write-Host "Fetching latest release..."
$Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
$Version = $Release.tag_name.TrimStart("v")

Write-Host "Installing $Binary v$Version (windows/$Arch)..."

$Filename = "${Binary}_${Version}_windows_${Arch}.zip"
$Url = "https://github.com/$Repo/releases/download/v$Version/$Filename"
$ZipPath = "$env:TEMP\migrate.zip"

Invoke-WebRequest -Uri $Url -OutFile $ZipPath
Expand-Archive -Path $ZipPath -DestinationPath . -Force
Remove-Item $ZipPath

Write-Host ""
Write-Host "✅ Installed: .\$Binary.exe"
Write-Host ""
Write-Host "Run it:"
Write-Host "  .\$Binary.exe"
