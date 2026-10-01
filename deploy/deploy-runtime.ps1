param()

$ErrorActionPreference = 'Stop'
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
$receipt = Get-Content -LiteralPath (Join-Path $root 'out\build-state.json') -Encoding UTF8 -Raw | ConvertFrom-Json
$backup = Join-Path $root ('out\deployment-history\' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
foreach ($item in $receipt.files) {
    if (-not $item.target) { continue }
    $target = Join-Path $runtime $item.target
    if (Test-Path -LiteralPath $target) {
        $info = Get-Item -LiteralPath $target -Force
        if ($info.Attributes -band [IO.FileAttributes]::ReparsePoint -or $info.PSIsContainer) { throw '部署程序不能是链接或目录' }
        New-Item -ItemType Directory -Path $backup -Force | Out-Null
        Copy-Item -LiteralPath $target -Destination (Join-Path $backup $item.target) -ErrorAction Stop
    }
    Copy-Item -LiteralPath (Join-Path $root $item.source) -Destination $target -ErrorAction Stop
}
& node (Join-Path $PSScriptRoot 'build-state.mjs') verify --deployed
if ($LASTEXITCODE -ne 0) { throw '部署校验失败，禁止启动；旧程序备份保留在 out/deployment-history' }
Write-Output '最新程序已部署到 yunx-test；正常升级不会覆盖配置、证书或数据库。全新部署必须使用独立的空运行目录。'
} finally {
    if ($lock) { $lock.Dispose() }
}
