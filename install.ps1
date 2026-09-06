# Installs ccam for the current user only: no admin/elevation, no
# system directories, only a per-user install dir and a per-user PATH
# entry (HKCU, not HKLM). Safe to pipe straight into a normal
# (non-elevated) PowerShell prompt:
#
#   irm https://raw.githubusercontent.com/Saif0089/ccam/main/install.ps1 | iex
$ErrorActionPreference = "Stop"

$Repo = "Saif0089/ccam"

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture) {
  "Arm64"   { "arm64" }
  default   { "amd64" }
}

$version = if ($env:CCAM_VERSION) { $env:CCAM_VERSION } else { "latest" }
$asset = "ccam_windows_$arch.exe"
if ($version -eq "latest") {
  $url = "https://github.com/$Repo/releases/latest/download/$asset"
} else {
  $url = "https://github.com/$Repo/releases/download/$version/$asset"
}

$installDir = if ($env:CCAM_INSTALL_DIR) { $env:CCAM_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "ccam\bin" }
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$dest = Join-Path $installDir "ccam.exe"

Write-Host "Downloading ccam (windows/$arch)..."
Invoke-WebRequest -Uri $url -OutFile $dest

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not $userPath) { $userPath = "" }
if ($userPath.Split(";") -notcontains $installDir) {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
  $env:Path = "$env:Path;$installDir"
  Write-Host "Added $installDir to your user PATH (open a new terminal for it to show up everywhere)."
}

Write-Host "Installed $dest"
Write-Host ""
& $dest install
