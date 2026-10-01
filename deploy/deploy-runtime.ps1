param()

$ErrorActionPreference = 'Stop'
$freshConfirmation = '确认全新部署 Smail'
$confirmation = Read-Host "这是不可逆的 Smail 专属数据清理操作。请输入“$freshConfirmation”继续"
if ($confirmation -cne $freshConfirmation) { throw '未获得有效确认，已取消全新部署' }
$root = Split-Path -Parent $PSScriptRoot
$runtime = Join-Path $root 'yunx-test'
$boundary = [IO.Path]::GetFullPath($root).TrimEnd('\') + '\'
foreach ($directory in @($runtime, (Join-Path $root 'out'))) {
    if (-not [IO.Path]::GetFullPath($directory).StartsWith($boundary, [StringComparison]::OrdinalIgnoreCase)) { throw '部署目录越过项目边界' }
    if (Test-Path -LiteralPath $directory) {
        $info = Get-Item -LiteralPath $directory -Force
        if (-not $info.PSIsContainer -or $info.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '部署目录不能是链接或文件' }
    }
}
$binary = Join-Path $runtime 'smail.exe'
$running = Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq $binary }
if ($running) { throw 'yunx-test 后端仍在运行，请先停止后端再部署，禁止覆盖正在运行的程序' }
New-Item -ItemType Directory -Path (Join-Path $root 'out') -Force | Out-Null
$lock = $null
try {
    $lock = [IO.File]::Open((Join-Path $root 'out\deployment.lock'), [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)

# 完整构建成功后才允许更新运行包，失败时不回退使用历史产物。
& (Join-Path $PSScriptRoot 'build.ps1')
& node (Join-Path $PSScriptRoot 'build-state.mjs') verify
if ($LASTEXITCODE -ne 0) { throw '编译产物校验失败，未部署' }

New-Item -ItemType Directory -Path $runtime -Force | Out-Null
foreach ($item in Get-ChildItem -LiteralPath $runtime -Force -ErrorAction SilentlyContinue) {
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '运行目录顶层存在重解析点，拒绝清理' }
    Remove-Item -LiteralPath $item.FullName -Recurse -Force
}
$receipt = Get-Content -LiteralPath (Join-Path $root 'out\build-state.json') -Encoding UTF8 -Raw | ConvertFrom-Json
foreach ($item in $receipt.files) {
    if (-not $item.target) { continue }
    $target = Join-Path $runtime $item.target
    Copy-Item -LiteralPath (Join-Path $root $item.source) -Destination $target -ErrorAction Stop
}
& node (Join-Path $PSScriptRoot 'build-state.mjs') verify --deployed
if ($LASTEXITCODE -ne 0) { throw '部署校验失败，禁止启动；本次全新部署未保留旧程序' }
Write-Output '最新程序已完成全新部署到 yunx-test；配置、数据库、证书、日志、插件和绑定信息均未继承，请重新完成初始化配置。'
} finally {
    if ($lock) { $lock.Dispose() }
}
