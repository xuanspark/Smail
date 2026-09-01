[CmdletBinding()]
param(
    [string]$PackageRoot = $null,
    [string]$InstallRoot = (Join-Path $env:ProgramData "Smail"),
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
$mainSource = Resolve-PackageFile -Candidates @(
    (Join-Path $PackageRoot "server\smail_windows_amd64.exe"),
    (Join-Path $PackageRoot "server\smail.exe"),
    (Join-Path $PackageRoot "smail_windows_amd64.exe"),
    (Join-Path $PackageRoot "smail.exe")
) -Description "smail.exe"
$adminSource = Resolve-PackageFile -Candidates @(
    (Join-Path $PackageRoot "server\smail-admin_windows_amd64.exe"),
    (Join-Path $PackageRoot "server\smail-admin.exe"),
    (Join-Path $PackageRoot "smail-admin_windows_amd64.exe"),
    (Join-Path $PackageRoot "smail-admin.exe")
) -Description "smail-admin.exe"

$resolvedInstallRoot = [IO.Path]::GetFullPath($InstallRoot)
New-Item -ItemType Directory -Path $resolvedInstallRoot -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $resolvedInstallRoot "config") -Force | Out-Null
$transactionRoot = Join-Path $resolvedInstallRoot (".upgrade-" + [Guid]::NewGuid().ToString("N"))
$backupRoot = Join-Path $resolvedInstallRoot (".backup-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $transactionRoot -Force | Out-Null
New-Item -ItemType Directory -Path $backupRoot -Force | Out-Null

$mainTarget = Join-Path $resolvedInstallRoot "smail.exe"
$adminTarget = Join-Path $resolvedInstallRoot "smail-admin.exe"
$mainBackup = Join-Path $backupRoot "smail.exe"
$adminBackup = Join-Path $backupRoot "smail-admin.exe"
$serviceCreated = $false

try {
    Stop-SmailService
    Copy-Item -LiteralPath $mainSource -Destination (Join-Path $transactionRoot "smail.exe")
    Copy-Item -LiteralPath $adminSource -Destination (Join-Path $transactionRoot "smail-admin.exe")
    if (Test-Path -LiteralPath $mainTarget) { Move-Item -LiteralPath $mainTarget -Destination $mainBackup }
    if (Test-Path -LiteralPath $adminTarget) { Move-Item -LiteralPath $adminTarget -Destination $adminBackup }
    Move-Item -LiteralPath (Join-Path $transactionRoot "smail.exe") -Destination $mainTarget
    Move-Item -LiteralPath (Join-Path $transactionRoot "smail-admin.exe") -Destination $adminTarget
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
    if (Test-Path -LiteralPath $mainTarget) {
        Remove-Item -LiteralPath $mainTarget -Force
    }
    if (Test-Path -LiteralPath $adminTarget) {
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
    if (Test-Path -LiteralPath $transactionRoot) {
        Remove-Item -LiteralPath $transactionRoot -Recurse -Force
    }
    if (Test-Path -LiteralPath $backupRoot) {
        Remove-Item -LiteralPath $backupRoot -Recurse -Force
    }
    throw
}

if (Test-Path -LiteralPath $transactionRoot) {
    Remove-Item -LiteralPath $transactionRoot -Recurse -Force
}
if (Test-Path -LiteralPath $backupRoot) {
    Remove-Item -LiteralPath $backupRoot -Recurse -Force
}
