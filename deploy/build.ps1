param([switch]$SkipFrontend)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$output = Join-Path $root 'out\backend'
New-Item -ItemType Directory -Path $output -Force | Out-Null
if (-not $SkipFrontend) {
    Push-Location (Join-Path $root 'frontend')
    try {
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
    } finally { Pop-Location }
}
Push-Location (Join-Path $root 'backend')
try {
    & go build -o (Join-Path $output 'smail-dev.exe') .
    if ($LASTEXITCODE -ne 0) { throw '开发后端构建失败' }
    & go build -o (Join-Path $output 'smail-admin.exe') ./cmd/smail-admin
    if ($LASTEXITCODE -ne 0) { throw '管理工具构建失败' }
    & go build -o (Join-Path $output 'smail-config.exe') ./cmd/smail-config
    if ($LASTEXITCODE -ne 0) { throw '配置工具构建失败' }
} finally { Pop-Location }
Write-Output '构建完成：out/frontend 和 out/backend。正式发行请使用 Makefile 的跨平台构建目标。'
