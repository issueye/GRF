[CmdletBinding()]
param(
    [string]$Version = "0.1.0",
    [string]$OutputDirectory = "bin",
    [switch]$SkipTests,
    [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$frontendRoot = Join-Path $repoRoot "desktop\frontend"
$outputRoot = Join-Path $repoRoot $OutputDirectory
$binaryPath = Join-Path $outputRoot "grf-desktop.exe"

foreach ($command in @("go", "node", "npm")) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "$command was not found in PATH. Install it and reopen PowerShell."
    }
}

$wails3 = Get-Command wails3 -ErrorAction SilentlyContinue
if ($wails3) {
    $wails3Path = $wails3.Source
}
else {
    $wails3Path = Join-Path (& go env GOPATH) "bin\wails3.exe"
}
if (-not (Test-Path $wails3Path)) {
    throw "wails3 was not found. Run: go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117"
}

Push-Location $repoRoot
try {
    if (-not $SkipTests) {
        Write-Host "==> Go tests"
        & go test ./...
        if ($LASTEXITCODE -ne 0) { throw "go test failed with exit code $LASTEXITCODE" }
    }

    Write-Host "==> Wails bindings"
    & $wails3Path generate bindings -names -ts -d desktop/frontend/bindings ./desktop
    if ($LASTEXITCODE -ne 0) { throw "Wails binding generation failed with exit code $LASTEXITCODE" }

    Write-Host "==> Frontend"
    Push-Location $frontendRoot
    try {
        if (-not $SkipInstall) {
            & npm ci
            if ($LASTEXITCODE -ne 0) { throw "npm ci failed with exit code $LASTEXITCODE" }
        }
        & npm run build
        if ($LASTEXITCODE -ne 0) { throw "frontend build failed with exit code $LASTEXITCODE" }
    }
    finally {
        Pop-Location
    }

    Write-Host "==> Windows desktop executable"
    New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
    & go build -tags production -trimpath -buildvcs=false -ldflags "-s -w -H windowsgui -X main.version=$Version" -o $binaryPath ./desktop
    if ($LASTEXITCODE -ne 0) { throw "desktop build failed with exit code $LASTEXITCODE" }

    Write-Host "Built: $binaryPath"
    Write-Host "Data:  %USERPROFILE%\.gtr (override with GTR_HOME)"
}
finally {
    Pop-Location
}
