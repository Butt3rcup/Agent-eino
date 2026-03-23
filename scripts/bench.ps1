#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://localhost:8080',
    [int]$Requests = 12,
    [int]$Concurrency = 3,
    [string]$Query = '最近有哪些网络热词？',
    [string]$UploadFile = ''
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

$env:GOPATH = Join-Path $repoRoot 'tmp/.gopath'
$env:GOMODCACHE = Join-Path $env:GOPATH 'pkg/mod'
$env:GOCACHE = Join-Path $repoRoot 'tmp/.gocache'
$reportPath = Join-Path $repoRoot 'tmp/bench-report.ndjson'

foreach ($dir in @($env:GOPATH, $env:GOMODCACHE, $env:GOCACHE, (Split-Path -Parent $reportPath))) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}
if (Test-Path $reportPath) {
    Remove-Item $reportPath -Force
}

function Invoke-Bench {
    param(
        [string[]]$Arguments,
        [string]$Title
    )

    Write-Host "`n=== $Title ===" -ForegroundColor Cyan
    & go run ./cmd/loadtest @Arguments '-save' $reportPath
    if ($LASTEXITCODE -ne 0) {
        throw "压测失败：$Title"
    }
}

Invoke-Bench -Title '检索接口 /api/search' -Arguments @(
    '-scenario', 'search',
    '-base-url', $BaseUrl,
    '-requests', $Requests,
    '-concurrency', $Concurrency,
    '-query', $Query
)

foreach ($mode in @('rag', 'react', 'rag_agent', 'multi-agent', 'graph_rag', 'graph_multi')) {
    Invoke-Bench -Title "问答接口 /api/query [$mode]" -Arguments @(
        '-scenario', 'query',
        '-base-url', $BaseUrl,
        '-mode', $mode,
        '-requests', $Requests,
        '-concurrency', $Concurrency,
        '-query', $Query
    )
}

if ([string]::IsNullOrWhiteSpace($UploadFile)) {
    $candidate = Get-ChildItem -Path $repoRoot -File -Recurse -Include *.md,*.markdown | Select-Object -First 1
    if ($candidate) {
        $UploadFile = $candidate.FullName
    }
}

if (-not [string]::IsNullOrWhiteSpace($UploadFile)) {
    Invoke-Bench -Title "上传接口 /api/upload [$UploadFile]" -Arguments @(
        '-scenario', 'upload',
        '-base-url', $BaseUrl,
        '-requests', [Math]::Min($Requests, 6),
        '-concurrency', [Math]::Min($Concurrency, 2),
        '-file', $UploadFile
    )
} else {
    Write-Host "`n跳过上传压测：没找到可上传的 Markdown 文件。" -ForegroundColor Yellow
}

Write-Host "`n=== 压测总榜 ===" -ForegroundColor Green
& go run ./cmd/benchreport -input $reportPath
if ($LASTEXITCODE -ne 0) {
    throw '生成压测总榜失败'
}

Write-Host "`n报告已保存：$reportPath" -ForegroundColor Green
