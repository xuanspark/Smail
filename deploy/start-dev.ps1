param([ValidateSet('backend', 'frontend')][string]$Service = 'backend')

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$logs = Join-Path $root 'test-log\runtime'
New-Item -ItemType Directory -Path $logs -Force | Out-Null
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$previousRoot = $env:SMAIL_ROOT
try {
    if ($Service -eq 'backend') {
        $binary = Join-Path $root 'out\backend\smail-dev.exe'
        if (-not (Test-Path -LiteralPath $binary)) { throw '请先运行 deploy/build.ps1 编译后端' }
        if (Get-NetTCPConnection -State Listen -LocalPort 443 -ErrorAction SilentlyContinue) { throw '443 端口已占用，请先确认并停止旧后端' }
        # 二进制与配置分离后必须显式指定根目录，不能在 out 下初始化新数据库。
        $env:SMAIL_ROOT = Join-Path $root 'backend'
        $arguments = @('--dev-disable-admin-reset')
        $workingDirectory = $env:SMAIL_ROOT
    } else {
        if (Get-NetTCPConnection -State Listen -LocalPort 15173 -ErrorAction SilentlyContinue) { throw '15173 端口已占用，请先确认并停止旧前端' }
        $binary = (Get-Command node -ErrorAction Stop).Source
        $vite = Join-Path $root 'frontend\node_modules\vite\bin\vite.js'
        if (-not (Test-Path -LiteralPath $vite)) { throw '请先安装 frontend 的依赖' }
        $arguments = @(('"{0}"' -f $vite), '--host', '127.0.0.1', '--port', '15173', '--strictPort')
        $workingDirectory = Join-Path $root 'frontend'
    }
    $process = Start-Process -FilePath $binary -ArgumentList $arguments -WorkingDirectory $workingDirectory -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $logs "$Service-$stamp.stdout.log") `
        -RedirectStandardError (Join-Path $logs "$Service-$stamp.stderr.log")
    Start-Sleep -Seconds 3
    $process.Refresh()
    if ($process.HasExited) { throw "服务启动失败，请检查 test-log/runtime 中的 $Service 日志" }
    Write-Output "$Service 已启动，进程编号：$($process.Id)；日志目录：test-log/runtime"
} finally {
    $env:SMAIL_ROOT = $previousRoot
}
