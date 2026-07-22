[CmdletBinding()]
param(
    [string]$Version = "0.1.0",
    [string]$OutputDirectory = "bin",
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$outputPath = Join-Path $repoRoot $OutputDirectory
$binaryPath = Join-Path $outputPath "grf.exe"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go was not found in PATH. Install Go and reopen PowerShell."
}

Push-Location $repoRoot
try {
    if (-not $SkipTests) {
        & go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw "go test failed with exit code $LASTEXITCODE"
        }
    }

    New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
    & go build -ldflags "-s -w -X main.version=$Version" -o $binaryPath ./cmd/grok
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }

    Write-Host "Built: $binaryPath"
    Write-Host "Run:   $binaryPath help"
}
finally {
    Pop-Location
}
