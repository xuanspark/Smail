[CmdletBinding()]
param(
    [string]$InstallRoot = (Join-Path $env:ProgramData "Smail")
)

$ErrorActionPreference = "Stop"

function Assert-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "必须使用管理员权限运行 Windows 卸载脚本"
    }
}

Assert-Administrator
$resolvedInstallRoot = [IO.Path]::GetFullPath($InstallRoot)
$service = Get-Service -Name "Smail" -ErrorAction SilentlyContinue
if ($null -ne $service) {
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name "Smail" -Force
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
    & sc.exe delete Smail
    if ($LASTEXITCODE -ne 0) {
        throw "删除 Smail Windows 服务失败"
    }
}

foreach ($fileName in @("smail.exe", "smail-admin.exe")) {
    $filePath = Join-Path $resolvedInstallRoot $fileName
    if (Test-Path -LiteralPath $filePath -PathType Leaf) {
        Remove-Item -LiteralPath $filePath -Force
    }
}
Write-Output "Smail Windows 服务和程序已卸载；配置、数据库、证书和用户数据未删除。"
