#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://localhost:8080',
    [string]$Dataset = 'testdata/agent_eval_cases.json',
    [string]$Modes = 'rag,rag_agent,multi-agent,graph_multi',
    [int]$TimeoutSec = 90
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptDir
Set-Location $repoRoot

$env:GOTELEMETRY = 'off'
$env:GOPATH = Join-Path $repoRoot 'tmp/.gopath'
$env:GOMODCACHE = Join-Path $env:GOPATH 'pkg/mod'
$env:GOCACHE = Join-Path $repoRoot 'tmp/.gocache'
$reportPath = Join-Path $repoRoot 'tmp/agent-eval-report.json'
$timeoutArg = '{0}s' -f $TimeoutSec

foreach ($dir in @($env:GOPATH, $env:GOMODCACHE, $env:GOCACHE, (Split-Path -Parent $reportPath))) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

Write-Host "=== 执行 agent 评测 ===" -ForegroundColor Cyan
& go run ./cmd/agenteval -base-url $BaseUrl -dataset $Dataset -modes $Modes -timeout $timeoutArg -save $reportPath
if ($LASTEXITCODE -ne 0) {
    throw 'agent 评测执行失败'
}

Write-Host "`n=== 输出评测总榜 ===" -ForegroundColor Green
& go run ./cmd/agentevalreport -input $reportPath
if ($LASTEXITCODE -ne 0) {
    throw 'agent 评测总榜生成失败'
}

Write-Host "`n报告已保存：$reportPath" -ForegroundColor Green