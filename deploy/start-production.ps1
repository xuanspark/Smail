param([switch]$Restart)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$runtime = Join-Path $root 'yunx-test'
$binary = Join-Path $runtime 'smail.exe'
& node (Join-Path $PSScriptRoot 'check-runtime.mjs')
if ($LASTEXITCODE -ne 0) { throw '模拟部署目录未准备完成' }
if ($Restart) {
    Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $binary } | ForEach-Object {
        Stop-Process -Id $_.ProcessId -ErrorAction Stop
    }
    Start-Sleep -Seconds 2
}
& (Join-Path $PSScriptRoot 'deploy-runtime.ps1')
& node (Join-Path $PSScriptRoot 'build-state.mjs') verify --deployed
if ($LASTEXITCODE -ne 0) { throw '运行包不是最新源码的构建结果' }
$previous = @{}
foreach ($name in @('SMAIL_ROOT', 'SMAIL_HTTP_PORT_OVERRIDE', 'SMAIL_HTTP_TLS_MODE_OVERRIDE', 'setup_port', 'SMAIL_DEV_DISABLE_ADMIN_RESET')) {
    $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    [Environment]::SetEnvironmentVariable($name, $null, 'Process')
}
try {
    $env:SMAIL_ROOT = $runtime
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
    $logs = Join-Path $root 'test-log\service-launch'
    New-Item -ItemType Directory -Path $logs -Force | Out-Null
    $process = Start-Process -FilePath $binary -WorkingDirectory $runtime -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $logs "production-$stamp.stdout.log") `
        -RedirectStandardError (Join-Path $logs "production-$stamp.stderr.log")
    Start-Sleep -Seconds 3
    $process.Refresh()
    if ($process.HasExited) { throw '生产模拟启动失败，请检查 test-log/service-launch；不会自动豁免管理员恢复通道保护' }
    Write-Output "生产模拟已启动：$($process.Id)，使用嵌入前端和 yunx-test 配置，管理员恢复通道保持保护。"
} finally {
    foreach ($name in $previous.Keys) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
}
