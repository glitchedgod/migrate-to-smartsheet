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

$InstallDir = "$env:LOCALAPPDATA\Programs\migrate-to-smartsheet"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

Invoke-WebRequest -Uri $Url -OutFile $ZipPath
Expand-Archive -Path $ZipPath -DestinationPath $InstallDir -Force
Remove-Item $ZipPath

# Add to user PATH if not already present
$CurrentPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($CurrentPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$CurrentPath;$InstallDir", "User")
    Write-Host "Added $InstallDir to your PATH."
    Write-Host "Restart your terminal for PATH changes to take effect."
}

Write-Host ""
Write-Host "✅ Installed: $InstallDir\$Binary.exe"
Write-Host ""
Write-Host "Run it (after restarting terminal):"
Write-Host "  $Binary"
