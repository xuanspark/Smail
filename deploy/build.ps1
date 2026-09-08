param([switch]$SkipFrontend)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'workspace-lock.ps1')
$workspaceLock = Enter-SmailWorkspaceLock -Root $root
try {
$fingerprint = & node (Join-Path $PSScriptRoot 'build-state.mjs') fingerprint
if ($LASTEXITCODE -ne 0) { throw '无法计算源码指纹' }
$output = Join-Path $root 'out\backend'
New-Item -ItemType Directory -Path $output -Force | Out-Null
if (-not $SkipFrontend) {
    # 只归档确定的编译目录，不删除文件；防止历史散列资源持续进入 Go 嵌入包。
    $archive = Join-Path $root ('out\build-history\' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
    $generated = @{
        'frontend' = 'out\frontend'
        'embedded-frontend' = 'backend\listen\http_server\dist'
        'telegram-ui' = 'out\plugins\telegram-ui'
        'embedded-telegram' = 'plugins\pmail_telegram_push\hook\dist'
        'embedded-plugins' = 'backend\cmd\smail\bundled'
    }
    foreach ($name in $generated.Keys) {
        $source = [IO.Path]::GetFullPath((Join-Path $root $generated[$name]))
        $target = [IO.Path]::GetFullPath((Join-Path $archive $name))
        $boundary = [IO.Path]::GetFullPath($root).TrimEnd('\') + '\'
        if (-not $source.StartsWith($boundary, [StringComparison]::OrdinalIgnoreCase) -or -not $target.StartsWith($boundary, [StringComparison]::OrdinalIgnoreCase)) {
            throw '编译归档路径越过项目目录，已停止'
        }
        if (Test-Path -LiteralPath $source) {
            $info = Get-Item -LiteralPath $source -Force
            if (-not $info.PSIsContainer -or $info.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '编译目录无效或为链接，禁止归档' }
            New-Item -ItemType Directory -Path $archive -Force | Out-Null
            Move-Item -LiteralPath $source -Destination $target -ErrorAction Stop
        }
    }
    Push-Location (Join-Path $root 'frontend')
    try {
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
    } finally { Pop-Location }
}
& node (Join-Path $PSScriptRoot 'build-plugins.mjs') --dev
if ($LASTEXITCODE -ne 0) { throw '插件构建失败' }
$bundle = Join-Path $root 'backend\cmd\smail\bundled'
New-Item -ItemType Directory -Path $bundle -Force | Out-Null
foreach ($name in @('spam_block.exe', 'pmail_telegram_push.exe', 'wechat_push.exe')) {
    Copy-Item -LiteralPath (Join-Path $root "out\plugins\$name") -Destination (Join-Path $bundle $name) -ErrorAction Stop
}
Push-Location (Join-Path $root 'backend')
try {
    $gitHash = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw '无法读取构建提交信息' }
    $buildTime = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
    $goVersion = (& go version).Trim()
    $flags = "-s -w -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.Version=dev-$($fingerprint.Substring(0, 12))' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GitHash=$gitHash' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.BuildTime=$buildTime' -X 'git.pcux.cn/xuans/Smail/internal/buildinfo.GoVersion=$goVersion'"
    & go build -tags bundled -trimpath -ldflags $flags -o (Join-Path $output 'smail.exe') ./cmd/smail
    if ($LASTEXITCODE -ne 0) { throw '开发后端构建失败' }
    & go build -trimpath -ldflags $flags -o (Join-Path $output 'smail-admin.exe') ./cmd/smail-admin
    if ($LASTEXITCODE -ne 0) { throw '管理工具构建失败' }
    & go build -o (Join-Path $output 'smail-config.exe') ./cmd/smail-config
    if ($LASTEXITCODE -ne 0) { throw '配置工具构建失败' }
} finally { Pop-Location }
if (-not $SkipFrontend) {
    & node (Join-Path $PSScriptRoot 'build-state.mjs') seal $fingerprint
    if ($LASTEXITCODE -ne 0) { throw '编译产物封存失败，禁止部署或启动旧包' }
}
Write-Output '构建完成：out/frontend、out/backend 和 out/plugins，包含三个内置插件。正式发行请使用 Makefile 的跨平台构建目标。'
} finally {
    $workspaceLock.ReleaseMutex()
    $workspaceLock.Dispose()
}
