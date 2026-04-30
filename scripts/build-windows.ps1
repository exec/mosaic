# Build a signed mosaic.exe + NSIS installer.
#
# Required env:
#   VERSION  e.g. v0.8.0 (defaults to "dev")
#
# Optional (all required for signing):
#   AZURE_KEY_VAULT_URI
#   AZURE_KEY_VAULT_CERT_NAME
#   AZURE_TENANT_ID
#   AZURE_CLIENT_ID
#   AZURE_CLIENT_SECRET

$ErrorActionPreference = "Stop"
$Version = $env:VERSION
if (-not $Version) { $Version = "dev" }

$Root = Resolve-Path "$PSScriptRoot/.."
Set-Location $Root
$BinDir = "$Root/build/bin"

Write-Host "==> build frontend first so main.go's go:embed has its target"
Push-Location frontend
npm run build
$rc = $LASTEXITCODE
Pop-Location
if ($rc -ne 0) { throw "frontend build failed" }

Write-Host "==> prime module cache"
go mod download
if ($LASTEXITCODE -ne 0) { throw "go mod download failed" }
go build ./...
if ($LASTEXITCODE -ne 0) { throw "go build prime failed" }

Write-Host "==> wails build windows/amd64"
wails build `
    -platform windows/amd64 `
    -ldflags "-X main.version=$Version" `
    -nsis `
    -clean `
    -skipbindings `
    -skipembedcreate
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }

$Exe = "$BinDir/mosaic.exe"
$Installer = "$BinDir/Mosaic-${Version}-windows-amd64-installer.exe"
$Portable  = "$BinDir/Mosaic-${Version}-windows-amd64-portable.exe"

# wails -nsis emits the installer at $BinDir/mosaic-amd64-installer.exe
Move-Item "$BinDir/mosaic-amd64-installer.exe" $Installer -Force
Copy-Item $Exe $Portable -Force

if ($env:AZURE_KEY_VAULT_URI -and $env:AZURE_KEY_VAULT_CERT_NAME) {
    Write-Host "==> AzureSignTool: sign exe + installer"
    $Args = @(
        "sign",
        "-kvu", $env:AZURE_KEY_VAULT_URI,
        "-kvc", $env:AZURE_KEY_VAULT_CERT_NAME,
        "-kvt", $env:AZURE_TENANT_ID,
        "-kvi", $env:AZURE_CLIENT_ID,
        "-kvs", $env:AZURE_CLIENT_SECRET,
        "-tr",  "http://timestamp.digicert.com",
        "-td",  "sha256",
        "-fd",  "sha256"
    )
    AzureSignTool @Args $Portable
    AzureSignTool @Args $Installer
} else {
    Write-Host "==> Windows signing skipped (AZURE_* not all set) — UNSIGNED dev build"
}

Write-Host "==> done: $Installer"
Write-Host "==> done: $Portable"
