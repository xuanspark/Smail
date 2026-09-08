param([Parameter(Mandatory = $true)][string]$InvocationFile)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
. (Join-Path $root 'deploy\workspace-lock.ps1')
$workspaceLock = Enter-SmailWorkspaceLock -Root $root
try {
    # 参数从测试记录读取为字符串数组，不拼接或执行 Shell 命令。
    $invocation = Get-Content -LiteralPath $InvocationFile -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($invocation.arguments -isnot [Array] -or $invocation.arguments.Count -eq 0) {
        throw 'Go 测试参数必须是非空数组'
    }
    foreach ($argument in $invocation.arguments) {
        if ($argument -isnot [string]) { throw 'Go 测试参数必须是字符串' }
    }
    $arguments = @($invocation.arguments)
    Push-Location (Join-Path $root 'backend')
    try {
        & go @arguments
        $code = $LASTEXITCODE
    } finally {
        Pop-Location
    }
} finally {
    $workspaceLock.ReleaseMutex()
    $workspaceLock.Dispose()
}
exit $code
