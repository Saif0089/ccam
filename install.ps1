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

# Stop a previous install before overwriting its exe. Windows locks a
# running executable's file, so without this every upgrade fails with
# "the process cannot access the file because it is being used by
# another process".
if (Test-Path $dest) {
  try { & $dest stop | Out-Null } catch { }
  Start-Sleep -Milliseconds 500
}

$tmp = "$dest.download"
Invoke-WebRequest -Uri $url -OutFile $tmp

# Verify against the checksums published alongside the binary.
try {
  $sumsUrl = ($url -replace '/[^/]+$', '/checksums.txt')
  $sums = (Invoke-WebRequest -Uri $sumsUrl).Content
  $line = ($sums -split "`n" | Where-Object { $_ -match [regex]::Escape($asset) + '\s*$' } | Select-Object -First 1)
  if ($line) {
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA256 -Path $tmp).Hash.ToLower()
    if ($expected.ToLower() -ne $actual) {
      Remove-Item $tmp -Force
      throw "checksum mismatch for $asset (expected $expected, got $actual)"
    }
  }
} catch [System.Net.WebException] {
  # Release without checksums; continue.
}

Move-Item -Force -Path $tmp -Destination $dest

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
