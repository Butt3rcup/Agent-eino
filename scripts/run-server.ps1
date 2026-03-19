#requires -Version 5.1
[CmdletBinding()]
param(
    [int]
    $Port,

    [switch]
    $Release,

    [string]
    $Gopath,

    [string]
    $Gocache
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

$proxyVars = @(
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "GIT_HTTP_PROXY",
    "GIT_HTTPS_PROXY"
)

foreach ($var in $proxyVars) {
    if (Test-Path "Env:$var") {
        Remove-Item "Env:$var" -ErrorAction SilentlyContinue
    }
}

$defaultGoEnv = & go env -json | ConvertFrom-Json
if (-not $defaultGoEnv) {
    throw "无法读取 go env 输出，确认 Go 已安装可用。"
}

$resolvedGopath = if ($Gopath) { $Gopath } else { $defaultGoEnv.GOPATH }
$resolvedGocache = if ($Gocache) { $Gocache } else { $defaultGoEnv.GOCACHE }

foreach ($dir in @($resolvedGopath, $resolvedGocache)) {
    if ([string]::IsNullOrWhiteSpace($dir)) {
        continue
    }
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

if (-not $resolvedGopath) {
    throw "GOPATH 未设置，无法继续。"
}
if (-not $resolvedGocache) {
    throw "GOCACHE 未设置，无法继续。"
}

$env:GOPATH = $resolvedGopath
$env:GOMODCACHE = Join-Path $env:GOPATH 'pkg/mod'
$env:GOCACHE = $resolvedGocache

if ($Port) {
    $env:SERVER_PORT = $Port
}

if ($Release) {
    $env:GIN_MODE = 'release'
}

Write-Host "Proxy variables cleared" -ForegroundColor Yellow
Write-Host "GOPATH: $($env:GOPATH)" -ForegroundColor Cyan
Write-Host "GOMODCACHE: $($env:GOMODCACHE)" -ForegroundColor Cyan
Write-Host "GOCACHE: $($env:GOCACHE)" -ForegroundColor Cyan

& go run ./cmd/server
