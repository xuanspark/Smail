param(
    [ValidateSet('backend', 'frontend')][string]$Service = 'backend',
    [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$logs = Join-Path $root 'test-log\runtime'
New-Item -ItemType Directory -Path $logs -Force | Out-Null
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$previousRoot = $env:SMAIL_ROOT
try {
    if ($Service -eq 'backend') {
        $binary = Join-Path $root 'out\backend\smail-dev.exe'
        if (Get-NetTCPConnection -State Listen -LocalPort 443 -ErrorAction SilentlyContinue) { throw '443 端口已占用，请先确认并停止旧后端' }
        $running = Get-CimInstance Win32_Process | Where-Object {
            $_.ExecutablePath -and $_.ExecutablePath -eq $binary
        }
        if ($running) { throw '开发后端仍在运行，请先核对并停止旧进程，避免编译或启动旧版本' }
        # 拒绝沿用旧运行目录，避免迁移遗漏时静默创建另一份账号库。
        $legacyConfig = Join-Path $root 'backend\config'
        foreach ($name in @('config.json', 'config.dev.json', 'credential.key', 'smail.db', 'smail_temp.db')) {
            if (Test-Path -LiteralPath (Join-Path $legacyConfig $name)) {
                throw 'backend/config 中仍有运行文件，请先停止后端并完成根目录 config 迁移'
            }
        }
        # 默认先编译当前工作区，不再把历史 smail-dev.exe 当成最新后端。
        # 不编译前端，保留 Vite 热更新；缺少首次嵌入资源时要求显式完整构建。
        if (-not $SkipBuild) {
            & (Join-Path $PSScriptRoot 'build.ps1') -SkipFrontend
        }
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw '请先运行 deploy/build.ps1 编译后端' }
        # SMAIL_ROOT 是 config 的父目录，不能指向 config 本身或编译输出目录。
        $env:SMAIL_ROOT = $root
        $arguments = @('--dev-disable-admin-reset')
        Write-Output '开发启动：沿用 config/config.json 和现有数据库；明确停用管理员恢复通道，不请求权限提升。'
        if ($SkipBuild) { Write-Warning '已跳过编译，请自行确认二进制与当前源码一致。' }
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
    $stdout = Join-Path $logs "$Service-$stamp.stdout.log"
    $stderr = Join-Path $logs "$Service-$stamp.stderr.log"
    $process = Start-Process -FilePath $binary -ArgumentList $arguments -WorkingDirectory $workingDirectory -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput $stdout -RedirectStandardError $stderr
    Start-Sleep -Seconds 3
    $process.Refresh()
    if ($process.HasExited) { throw "服务启动失败；标准输出：$stdout；错误日志：$stderr" }
    Write-Output "$Service 已启动，进程编号：$($process.Id)；日志目录：test-log/runtime"
    Write-Output "运行文件：$binary；标准输出：$stdout；错误日志：$stderr"
} finally {
    $env:SMAIL_ROOT = $previousRoot
}
