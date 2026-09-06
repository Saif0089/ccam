# Installs ccam for the current user only: no admin/elevation, no
# system directories, only a per-user install dir and a per-user PATH
# entry (HKCU, not HKLM). Safe to pipe straight into a normal
# (non-elevated) PowerShell prompt:
#
#   irm https://ccam-six.vercel.app/install.ps1 | iex
#
# Binaries are served from this same site (public/releases in the repo,
# published by .github/workflows/release.yml) rather than GitHub
# Releases, since the source repo is private.
$ErrorActionPreference = "Stop"

$BaseUrl = "https://ccam-six.vercel.app"

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture) {
  "Arm64"   { "arm64" }
  default   { "amd64" }
}

$asset = "ccam_windows_$arch.exe"
$url = "$BaseUrl/releases/$asset"

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
