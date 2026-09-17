param([string]$OutputName = 'full-review-20260917')

$ErrorActionPreference = 'Stop'
$root = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
if ($OutputName -notmatch '^[a-z0-9-]+$') { throw '测试产物目录名称无效' }
$output = Join-Path $root "out\$OutputName"
New-Item -ItemType Directory -Path $output -Force | Out-Null
$previous = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    Push-Location (Join-Path $root 'backend')
    try {
        $packages = & go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...
        if ($LASTEXITCODE -ne 0) { throw '无法枚举测试包' }
        $manifest = @()
        foreach ($package in $packages) {
            if (-not $package.Trim()) { continue }
            $relative = $package.Substring('git.pcux.cn/xuans/Smail'.Length).TrimStart('/')
            $directory = if ($relative) { "backend/$relative" } else { 'backend' }
            $target = Join-Path $output $directory
            New-Item -ItemType Directory -Path $target -Force | Out-Null
            & go test -c -o (Join-Path $target 'review.test') $package
            if ($LASTEXITCODE -ne 0) { throw "测试编译失败：$package" }
            $manifest += @{ package = $package; directory = $directory }
            Write-Output "已编译：$package"
        }
        $json = ConvertTo-Json -InputObject $manifest -Depth 4
        [IO.File]::WriteAllText((Join-Path $output 'manifest.json'), $json, [Text.UTF8Encoding]::new($false))
    } finally { Pop-Location }
    Copy-Item -LiteralPath (Join-Path $root 'deploy') -Destination (Join-Path $output 'deploy') -Recurse -Force
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'run_review_tests.py') -Destination $output -Force
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'start_review_tests.py') -Destination $output -Force
    & tar -czf "$output.tar.gz" -C $output .
    if ($LASTEXITCODE -ne 0) { throw '测试产物归档失败' }
    Write-Output "仅完成交叉编译，未在本机运行测试：$output.tar.gz"
} finally {
    foreach ($name in $previous.Keys) {
        [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process')
    }
}
