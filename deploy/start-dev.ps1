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
        # 拒绝沿用旧运行目录，避免迁移遗漏时静默创建另一份账号库。
        $legacyConfig = Join-Path $root 'backend\config'
        foreach ($name in @('config.json', 'config.dev.json', 'credential.key', 'smail.db', 'smail_temp.db')) {
            if (Test-Path -LiteralPath (Join-Path $legacyConfig $name)) {
                throw 'backend/config 中仍有运行文件，请先停止后端并完成根目录 config 迁移'
            }
        }
        # SMAIL_ROOT 是 config 的父目录，不能指向 config 本身或编译输出目录。
        $env:SMAIL_ROOT = $root
        $arguments = @('--dev-disable-admin-reset')
        # 插件从 out/plugins 加载；账号、配置和密钥仍通过 SMAIL_ROOT 定位到根目录 config。
        foreach ($name in @('spam_block.exe', 'pmail_telegram_push.exe', 'wechat_push.exe')) {
            if (-not (Test-Path -LiteralPath (Join-Path $root "out\plugins\$name") -PathType Leaf)) {
                throw '内置插件产物不完整，请先运行 deploy/build.ps1'
            }
        }
        $workingDirectory = Join-Path $root 'out'
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
