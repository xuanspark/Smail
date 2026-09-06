param([string]$WorkspaceRoot = (Join-Path $PSScriptRoot '..\..'))

$ErrorActionPreference = 'Stop'
$workspace = [IO.Path]::GetFullPath($WorkspaceRoot)
$scriptPath = Join-Path $workspace 'deploy\install-windows.ps1'
$tokens = $null
$parseErrors = $null
$scriptText = [IO.File]::ReadAllText($scriptPath, [Text.Encoding]::UTF8)
$ast = [Management.Automation.Language.Parser]::ParseInput($scriptText, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -ne 0) {
    throw '安装脚本存在语法错误，停止隔离验证'
}
$tries = @($ast.EndBlock.Statements | Where-Object { $_ -is [Management.Automation.Language.TryStatementAst] })
if ($tries.Count -ne 1) {
    throw '安装脚本事务结构已改变，需要重新审核测试入口'
}
$allowedCommands = @('Stop-SmailService', 'Copy-Item', 'Test-Path', 'Move-Item', 'Get-Service', 'Invoke-Native', 'Set-RestrictedAcl', 'Write-Output', 'Start-Service', 'Remove-Item', 'Join-Path')
$commands = $tries[0].FindAll({ param($node) $node -is [Management.Automation.Language.CommandAst] }, $true)
foreach ($command in $commands) {
    if ($command.GetCommandName() -notin $allowedCommands) {
        throw "发现未隔离的命令，拒绝执行：$($command.GetCommandName())"
    }
}

# 只执行真实事务语句，所有文件和服务操作均为内存函数桩，不执行安装脚本主体。
$transaction = [scriptblock]::Create($tries[0].Extent.Text)
$results = @()
foreach ($stage in @('stop', 'copy-main', 'copy-admin', 'backup-main', 'backup-admin', 'install-main', 'install-admin', 'service')) {
    $results += & {
        param($failureStage, $body)
        $virtualRoot = [IO.Path]::GetFullPath((Join-Path $workspace 'test-log\virtual-install-audit'))
        $resolvedInstallRoot = Join-Path $virtualRoot 'install'
        $transactionRoot = Join-Path $resolvedInstallRoot '.upgrade-audit'
        $backupRoot = Join-Path $resolvedInstallRoot '.backup-audit'
        $mainSource = Join-Path $virtualRoot 'package\smail.exe'
        $adminSource = Join-Path $virtualRoot 'package\smail-admin.exe'
        $mainTarget = Join-Path $resolvedInstallRoot 'smail.exe'
        $adminTarget = Join-Path $resolvedInstallRoot 'smail-admin.exe'
        $mainBackup = Join-Path $backupRoot 'smail.exe'
        $adminBackup = Join-Path $backupRoot 'smail-admin.exe'
        $serviceCreated = $false
        $StartService = $false
        $state = @{
            Files = @{}
            Operations = [Collections.Generic.List[string]]::new()
            Injected = $false
        }
        $state.Files[$mainSource] = 'new-main'
        $state.Files[$adminSource] = 'new-admin'
        $state.Files[$mainTarget] = 'old-main'
        $state.Files[$adminTarget] = 'old-admin'

        function Assert-AuditPath {
            param([string]$Value)
            $absolute = [IO.Path]::GetFullPath($Value)
            if (-not $absolute.StartsWith($virtualRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
                throw '模拟路径超出隔离范围'
            }
        }
        function Invoke-Failure {
            param([string]$Stage)
            if ($failureStage -eq $Stage -and -not $state.Injected) {
                $state.Injected = $true
                throw "audit-injected:$Stage"
            }
        }
        function Stop-SmailService {
            Invoke-Failure 'stop'
        }
        function Test-Path {
            param([string]$LiteralPath)
            Assert-AuditPath $LiteralPath
            return $state.Files.ContainsKey($LiteralPath)
        }
        function Copy-Item {
            param([string]$LiteralPath, [string]$Destination)
            Assert-AuditPath $LiteralPath
            Assert-AuditPath $Destination
            if ($LiteralPath -eq $mainSource) { Invoke-Failure 'copy-main' }
            if ($LiteralPath -eq $adminSource) { Invoke-Failure 'copy-admin' }
            $state.Files[$Destination] = $state.Files[$LiteralPath]
            $state.Operations.Add("copy:$LiteralPath")
        }
        function Move-Item {
            param([string]$LiteralPath, [string]$Destination, [switch]$Force)
            Assert-AuditPath $LiteralPath
            Assert-AuditPath $Destination
            if ($LiteralPath -eq $mainTarget) { Invoke-Failure 'backup-main' }
            if ($LiteralPath -eq $adminTarget) { Invoke-Failure 'backup-admin' }
            if ($Destination -eq $mainTarget -and $LiteralPath -ne $mainBackup) { Invoke-Failure 'install-main' }
            if ($Destination -eq $adminTarget -and $LiteralPath -ne $adminBackup) { Invoke-Failure 'install-admin' }
            $state.Files[$Destination] = $state.Files[$LiteralPath]
            $state.Files.Remove($LiteralPath)
            $state.Operations.Add("move:$LiteralPath")
        }
        function Remove-Item {
            param([string]$LiteralPath, [switch]$Recurse, [switch]$Force)
            Assert-AuditPath $LiteralPath
            $state.Files.Remove($LiteralPath)
            $state.Operations.Add("remove:$LiteralPath")
        }
        function Get-Service { Invoke-Failure 'service'; throw '不得调用真实系统服务' }
        function Invoke-Native { throw '不得调用原生系统工具' }
        function Set-RestrictedAcl { throw '不得修改真实权限' }
        function Start-Service { throw '不得启动真实服务' }

        $caught = ''
        try {
            & $body
        } catch {
            $caught = $_.Exception.Message
        }
        if ($caught -ne "audit-injected:$failureStage") {
            throw "隔离失败注入未按预期命中：$caught"
        }
        [pscustomobject]@{
            Stage = $failureStage
            OriginalMainPreserved = $state.Files[$mainTarget] -eq 'old-main'
            OriginalAdminPreserved = $state.Files[$adminTarget] -eq 'old-admin'
            Operations = @($state.Operations.ToArray())
        }
    } $stage $transaction
}
$results | ConvertTo-Json -Depth 5
if (@($results | Where-Object { -not $_.OriginalMainPreserved -or -not $_.OriginalAdminPreserved }).Count -gt 0) {
    throw '安装事务在备份完成前失败时，回滚会删除未被替换的旧程序；以上操作全部为内存模拟'
}
