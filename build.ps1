# .\build.ps1 builds build\bin\modhound.exe with the version stamped and writes its sha256; -Resources also regenerates the icons

param([switch]$Resources)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$version = "0.1.1"
$exe = "build\bin\modhound.exe"

if ($Resources) {
  pwsh -NoProfile -File .\tools\genicons.ps1
}

wails build -clean -ldflags "-X main.Version=$version"
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
Write-Output ("built {0} {1}" -f $exe, $version)

$hash = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLower()
"$hash  modhound.exe" | Out-File -Encoding ascii "$exe.sha256"
Write-Output ("sha256 {0} -> {1}.sha256" -f $hash, $exe)
