#requires -Version 5.1
[CmdletBinding()]
param()

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

$env:GOPATH = Join-Path $repoRoot 'tmp/.gopath'
$env:GOMODCACHE = Join-Path $env:GOPATH 'pkg/mod'
$env:GOCACHE = Join-Path $repoRoot 'tmp/.gocache'

foreach ($dir in @($env:GOPATH, $env:GOMODCACHE, $env:GOCACHE)) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

Write-Host "GOPATH: $($env:GOPATH)" -ForegroundColor Cyan
Write-Host "GOMODCACHE: $($env:GOMODCACHE)" -ForegroundColor Cyan
Write-Host "GOCACHE: $($env:GOCACHE)" -ForegroundColor Cyan

go test ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

go vet ./...
exit $LASTEXITCODE
