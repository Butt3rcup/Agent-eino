#requires -Version 5.1
[CmdletBinding()]
param(
    [int]$Port,
    [switch]$Release,
    [string]$Gopath,
    [string]$Gocache
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

foreach ($var in @(
    'HTTP_PROXY',
    'HTTPS_PROXY',
    'ALL_PROXY',
    'GIT_HTTP_PROXY',
    'GIT_HTTPS_PROXY'
)) {
    if (Test-Path "Env:$var") {
        Remove-Item "Env:$var" -ErrorAction SilentlyContinue
    }
}

$resolvedGopath = if ($Gopath) { $Gopath } else { Join-Path $repoRoot 'tmp/.gopath' }
$resolvedGocache = if ($Gocache) { $Gocache } else { Join-Path $repoRoot 'tmp/.gocache' }

if ([string]::IsNullOrWhiteSpace($resolvedGopath)) {
    throw 'GOPATH is required.'
}
if ([string]::IsNullOrWhiteSpace($resolvedGocache)) {
    throw 'GOCACHE is required.'
}

$resolvedGomodcache = Join-Path $resolvedGopath 'pkg/mod'
foreach ($dir in @($resolvedGopath, $resolvedGocache, $resolvedGomodcache)) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

$env:GOPATH = $resolvedGopath
$env:GOMODCACHE = $resolvedGomodcache
$env:GOCACHE = $resolvedGocache

if ($Port) {
    $env:SERVER_PORT = $Port
}
if ($Release) {
    $env:GIN_MODE = 'release'
}

Write-Host 'Proxy variables cleared' -ForegroundColor Yellow
Write-Host "GOPATH: $($env:GOPATH)" -ForegroundColor Cyan
Write-Host "GOMODCACHE: $($env:GOMODCACHE)" -ForegroundColor Cyan
Write-Host "GOCACHE: $($env:GOCACHE)" -ForegroundColor Cyan

& go run ./cmd/server
