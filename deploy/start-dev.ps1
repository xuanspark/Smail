param(
    [ValidateSet('backend', 'frontend', 'all')][string]$Service = 'all',
    [switch]$Restart
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$runtime = Join-Path $root 'yunx-test'
$logs = Join-Path $root 'test-log\service-launch'
# 控制台与热更新输出属于模拟部署测试记录，业务运行日志由程序按天管理。
New-Item -ItemType Directory -Path $logs -Force | Out-Null
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$previousRoot = $env:SMAIL_ROOT
$previousHTTPPortOverride = $env:SMAIL_HTTP_PORT_OVERRIDE
$previousHTTPTLSModeOverride = $env:SMAIL_HTTP_TLS_MODE_OVERRIDE
$previousSetupPort = $env:setup_port
try {
    & node (Join-Path $PSScriptRoot 'check-runtime.mjs')
    if ($LASTEXITCODE -ne 0) { throw '请先完成 yunx-test 运行数据迁移，不能自动创建另一份账号库' }
    if ($Restart) {
        # 只停止本部署的已确认进程，不能按进程名称或端口盲目结束其他项目。
        $expected = Join-Path $runtime 'smail.exe'
        $vitePath = Join-Path $root 'frontend\node_modules\vite\bin\vite.js'
        $processes = Get-CimInstance Win32_Process | Where-Object {
            ($Service -in @('backend', 'all') -and $_.ExecutablePath -eq $expected) -or
            ($Service -in @('frontend', 'all') -and $_.Name -eq 'node.exe' -and $_.CommandLine.Contains($vitePath))
        }
        foreach ($item in $processes) { Stop-Process -Id $item.ProcessId -ErrorAction Stop }
        Start-Sleep -Seconds 2
    }
    if ($Service -in @('backend', 'all')) {
        $binary = Join-Path $runtime 'smail.exe'
        if (Get-NetTCPConnection -State Listen -LocalPort 5201 -ErrorAction SilentlyContinue) { throw '5201 端口已占用，请先确认并停止旧后端' }
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
        & (Join-Path $PSScriptRoot 'deploy-runtime.ps1')
        & node (Join-Path $PSScriptRoot 'build-state.mjs') verify --deployed
        if ($LASTEXITCODE -ne 0) { throw '运行包不是最新编译结果，拒绝启动' }
        # SMAIL_ROOT 是 config 的父目录，不能指向 config 本身或编译输出目录。
        $env:SMAIL_ROOT = $runtime
        $env:SMAIL_HTTP_PORT_OVERRIDE = '5201'
        $env:SMAIL_HTTP_TLS_MODE_OVERRIDE = 'proxy'
        $env:setup_port = '5201'
        $arguments = @('--dev-disable-admin-reset')
        Write-Output '开发启动：后端监听 5201，运行根目录固定为 yunx-test；明确停用管理员恢复通道，不请求权限提升。'
        # 插件产物仍须核验；插件打包迁移未完成时不能省略这些构建输入。
        foreach ($name in @('spam_block.exe', 'pmail_telegram_push.exe', 'wechat_push.exe')) {
            if (-not (Test-Path -LiteralPath (Join-Path $root "out\plugins\$name") -PathType Leaf)) {
                throw '内置插件产物不完整，请先运行 deploy/build.ps1'
            }
        }
        $workingDirectory = $runtime
    } else {
        foreach ($port in @(80, 443)) {
            if (Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue) { throw "$port 端口已占用，请先确认并停止旧前端或旧后端" }
        }
        $binary = (Get-Command node -ErrorAction Stop).Source
        & node (Join-Path $PSScriptRoot 'build-state.mjs') verify --deployed
        if ($LASTEXITCODE -ne 0) { throw '请先使用 -Service all 或 backend 重新编译部署，前端不能搭配旧后端启动' }
        $env:SMAIL_ROOT = $runtime
        $vite = Join-Path $root 'frontend\node_modules\vite\bin\vite.js'
        if (-not (Test-Path -LiteralPath $vite)) { throw '请先安装 frontend 的依赖' }
        $arguments = @(('"{0}"' -f $vite), '--host', '127.0.0.1', '--port', '443', '--strictPort')
        $workingDirectory = Join-Path $root 'frontend'
    }
    $stdout = Join-Path $logs "$Service-$stamp.stdout.log"
    $stderr = Join-Path $logs "$Service-$stamp.stderr.log"
    $process = Start-Process -FilePath $binary -ArgumentList $arguments -WorkingDirectory $workingDirectory -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput $stdout -RedirectStandardError $stderr
    Start-Sleep -Seconds 3
    $process.Refresh()
    if ($process.HasExited) { throw "服务启动失败；标准输出：$stdout；错误日志：$stderr" }
    Write-Output "$Service 已启动，进程编号：$($process.Id)；运行日志：yunx-test/log；启动诊断：test-log/service-launch"
    Write-Output "运行文件：$binary；标准输出：$stdout；错误日志：$stderr"
} finally {
    $env:SMAIL_ROOT = $previousRoot
    $env:SMAIL_HTTP_PORT_OVERRIDE = $previousHTTPPortOverride
    $env:SMAIL_HTTP_TLS_MODE_OVERRIDE = $previousHTTPTLSModeOverride
    $env:setup_port = $previousSetupPort
}
if ($Service -eq 'all') {
    & $PSCommandPath -Service frontend
}
