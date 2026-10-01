[CmdletBinding()]
param(
    [string]$PackageRoot = $null,
    [string]$InstallRoot = (Join-Path $env:ProgramData "Smail"),
    [switch]$Fresh,
    [switch]$StartService
)

if ([string]::IsNullOrWhiteSpace($PackageRoot)) {
    $PackageRoot = Split-Path -Parent $PSScriptRoot
}

$ErrorActionPreference = "Stop"

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "必须使用管理员权限运行 Windows 安装脚本"
    }
}

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $false)][string[]]$Arguments = @()
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "命令执行失败：$FilePath，退出码 $LASTEXITCODE"
    }
}

function Resolve-PackageFile {
    param(
        [Parameter(Mandatory = $true)][string[]]$Candidates,
        [Parameter(Mandatory = $true)][string]$Description
    )

    foreach ($candidate in $Candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    throw "未找到 $Description，请先构建对应 Windows 发布文件"
}

function Set-RestrictedAcl {
    param([Parameter(Mandatory = $true)][string]$Root)

    $mainBinary = Join-Path $Root "smail.exe"
    $adminBinary = Join-Path $Root "smail-admin.exe"
    $configDirectory = Join-Path $Root "config"
    Invoke-Native -FilePath "icacls.exe" -Arguments @(
        $Root,
        "/inheritance:r",
        "/grant:r",
        "SYSTEM:(OI)(CI)(F)",
        "Administrators:(OI)(CI)(F)",
        "NT SERVICE\Smail:(OI)(CI)(M)",
        "/C"
    )
    Invoke-Native -FilePath "icacls.exe" -Arguments @(
        $mainBinary,
        "/grant:r",
        "NT SERVICE\Smail:(RX)",
        "/deny",
        "NT SERVICE\Smail:(WD,AD,DC,WA)",
        "/C"
    )
    Invoke-Native -FilePath "icacls.exe" -Arguments @(
        $adminBinary,
        "/grant:r",
        "SYSTEM:(F)",
        "Administrators:(F)",
        "/deny",
        "NT SERVICE\Smail:(F)",
        "/C"
    )
    if (Test-Path -LiteralPath $configDirectory -PathType Container) {
        Invoke-Native -FilePath "icacls.exe" -Arguments @(
            $configDirectory,
            "/grant",
            "NT SERVICE\Smail:(OI)(CI)(M)",
            "/C"
        )
    }
}

function Stop-SmailService {
    $service = Get-Service -Name "Smail" -ErrorAction SilentlyContinue
    if ($null -eq $service) {
        return
    }
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name "Smail" -Force
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
}

Assert-Administrator
if ($Fresh -and $StartService) {
    throw "全新部署必须先完成初始化配置，不能在安装时启动服务"
}
if ($Fresh) {
    $freshConfirmation = "确认全新部署 Smail"
    $confirmation = Read-Host "这是不可逆的 Smail 专属数据清理操作。请输入“$freshConfirmation”继续"
    if ($confirmation -cne $freshConfirmation) {
        throw "未获得有效确认，已取消全新部署"
    }
}
$mainSource = Resolve-PackageFile -Candidates @(
    (Join-Path $PackageRoot "out\backend\smail_windows_amd64.exe"),
    (Join-Path $PackageRoot "backend\smail_windows_amd64.exe"),
    (Join-Path $PackageRoot "backend\smail.exe"),
    (Join-Path $PackageRoot "smail_windows_amd64.exe"),
    (Join-Path $PackageRoot "smail.exe")
) -Description "smail.exe"
$adminSource = Resolve-PackageFile -Candidates @(
    (Join-Path $PackageRoot "out\backend\smail-admin_windows_amd64.exe"),
    (Join-Path $PackageRoot "backend\smail-admin_windows_amd64.exe"),
    (Join-Path $PackageRoot "backend\smail-admin.exe"),
    (Join-Path $PackageRoot "smail-admin_windows_amd64.exe"),
    (Join-Path $PackageRoot "smail-admin.exe")
) -Description "smail-admin.exe"

$resolvedInstallRoot = [IO.Path]::GetFullPath($InstallRoot)
if (Test-Path -LiteralPath $resolvedInstallRoot) {
    $rootInfo = Get-Item -LiteralPath $resolvedInstallRoot -Force
    if (-not $rootInfo.PSIsContainer -or $rootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw "安装目录不能是文件或链接"
    }
}
if ($Fresh -and (Test-Path -LiteralPath $resolvedInstallRoot)) {
    foreach ($item in Get-ChildItem -LiteralPath $resolvedInstallRoot -Force) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw "全新部署已停止：安装目录顶层存在重解析点，拒绝清理：$($item.FullName)"
        }
    }
}
New-Item -ItemType Directory -Path $resolvedInstallRoot -Force | Out-Null
if ($Fresh) {
    Stop-SmailService
    $backupRoot = Join-Path ([IO.Path]::GetDirectoryName($resolvedInstallRoot)) (".smail-fresh-backup-" + [Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $backupRoot -Force | Out-Null
    if (Test-Path -LiteralPath $resolvedInstallRoot) {
        foreach ($item in Get-ChildItem -LiteralPath $resolvedInstallRoot -Force) {
            Copy-Item -LiteralPath $item.FullName -Destination (Join-Path $backupRoot $item.Name) -Recurse -Force
        }
        foreach ($item in Get-ChildItem -LiteralPath $resolvedInstallRoot -Force) {
            Remove-Item -LiteralPath $item.FullName -Recurse -Force
        }
    }
    Write-Output "已完成 Smail 专属运行数据清理；完整备份保存在 $backupRoot。"
} else {
    $backupRoot = Join-Path $resolvedInstallRoot (".backup-" + [Guid]::NewGuid().ToString("N"))
}
New-Item -ItemType Directory -Path (Join-Path $resolvedInstallRoot "config") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $resolvedInstallRoot "data") -Force | Out-Null
$transactionRoot = Join-Path $resolvedInstallRoot (".upgrade-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $transactionRoot -Force | Out-Null
if (-not $Fresh) {
    $backupRoot = Join-Path $resolvedInstallRoot (".backup-" + [Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $backupRoot -Force | Out-Null
}

$mainTarget = Join-Path $resolvedInstallRoot "smail.exe"
$adminTarget = Join-Path $resolvedInstallRoot "smail-admin.exe"
$mainBackup = Join-Path $backupRoot "smail.exe"
$adminBackup = Join-Path $backupRoot "smail-admin.exe"
$serviceCreated = $false

try {
    $mainInstalled = $false
    $adminInstalled = $false
    Stop-SmailService
    Copy-Item -LiteralPath $mainSource -Destination (Join-Path $transactionRoot "smail.exe")
    Copy-Item -LiteralPath $adminSource -Destination (Join-Path $transactionRoot "smail-admin.exe")
    if (Test-Path -LiteralPath $mainTarget) { Move-Item -LiteralPath $mainTarget -Destination $mainBackup }
    if (Test-Path -LiteralPath $adminTarget) { Move-Item -LiteralPath $adminTarget -Destination $adminBackup }
    Move-Item -LiteralPath (Join-Path $transactionRoot "smail.exe") -Destination $mainTarget
    $mainInstalled = $true
    Move-Item -LiteralPath (Join-Path $transactionRoot "smail-admin.exe") -Destination $adminTarget
    $adminInstalled = $true
    $service = Get-Service -Name "Smail" -ErrorAction SilentlyContinue
    if ($null -eq $service) {
        $binaryPath = '"' + $mainTarget + '"'
        Invoke-Native -FilePath "sc.exe" -Arguments @(
            "create", "Smail", "binPath=", $binaryPath,
            "start=", "auto", "obj=", "NT SERVICE\Smail",
            "DisplayName=", "Smail 邮件服务"
        )
        $serviceCreated = $true
    } else {
        Invoke-Native -FilePath "sc.exe" -Arguments @(
            "config", "Smail", "binPath=", ('"' + $mainTarget + '"'),
            "start=", "auto", "obj=", "NT SERVICE\Smail"
        )
    }
    Invoke-Native -FilePath "sc.exe" -Arguments @(
        "sidtype", "Smail", "unrestricted"
    )
    Set-RestrictedAcl -Root $resolvedInstallRoot

    $failureActions = "restart/60000/restart/60000/"
    Invoke-Native -FilePath "sc.exe" -Arguments @(
        "failure", "Smail", "reset=", "86400", "actions=", $failureActions
    )
    Write-Output "Smail Windows 安装完成；超级管理员重置程序不会自动注册或启动服务。"
    if ($StartService) {
        Start-Service -Name "Smail"
        Write-Output "Smail 主服务已启动。"
    }
    if (-not $StartService) {
        Write-Output "主服务保持停止状态，请完成配置后再手动启动。"
    }
} catch {
    if ($mainInstalled -and (Test-Path -LiteralPath $mainTarget)) {
        Remove-Item -LiteralPath $mainTarget -Force
    }
    if ($adminInstalled -and (Test-Path -LiteralPath $adminTarget)) {
        Remove-Item -LiteralPath $adminTarget -Force
    }
    if (Test-Path -LiteralPath $mainBackup) {
        Move-Item -LiteralPath $mainBackup -Destination $mainTarget -Force
    }
    if (Test-Path -LiteralPath $adminBackup) {
        Move-Item -LiteralPath $adminBackup -Destination $adminTarget -Force
    }
    if ($serviceCreated) {
        $deleteArguments = @("delete", "Smail")
        Invoke-Native -FilePath "sc.exe" -Arguments $deleteArguments
    }
    # 失败时保留事务目录和备份，回滚再次失败时也不能丢失唯一恢复材料。
    throw
}

$temporaryRoots = @($transactionRoot)
if (-not $Fresh) {
    $temporaryRoots += $backupRoot
}
foreach ($temporaryRoot in $temporaryRoots) {
    if (Test-Path -LiteralPath $temporaryRoot) {
        $resolvedTemporaryRoot = (Resolve-Path -LiteralPath $temporaryRoot).Path
        if ([IO.Path]::GetDirectoryName($resolvedTemporaryRoot) -ne $resolvedInstallRoot.TrimEnd('\')) {
            throw "事务目录超出安装目录，拒绝清理：$resolvedTemporaryRoot"
        }
        if ((Get-Item -LiteralPath $resolvedTemporaryRoot).Attributes -band [IO.FileAttributes]::ReparsePoint) {
            throw "事务目录不能是重解析点，拒绝清理：$resolvedTemporaryRoot"
        }
        Remove-Item -LiteralPath $resolvedTemporaryRoot -Recurse -Force
    }
}
