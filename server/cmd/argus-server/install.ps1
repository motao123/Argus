<#
.SYNOPSIS
    Argus Agent 一键安装脚本（Windows / PowerShell），功能与 install.sh 对齐。

.DESCRIPTION
    下载 argus-agent 与 checksums.txt，强制 SHA-256 校验（缺失或不匹配即失败，
    绝不跳过），安装到指定目录，并注册为 NSSM 服务（存在 NSSM 时）或计划任务，
    幂等可重装。

.EXAMPLE
    # 1) 下载脚本：
    #    curl.exe -fsSL http://<server>/install.ps1 -o install.ps1
    # 2) 以管理员身份执行：
    powershell -ExecutionPolicy Bypass -File install.ps1 -ServerUrl ws://<server>/ws/agent -Secret <secret>

.PARAMETER ServerUrl
    Agent WebSocket 服务器地址（必填，如 ws://127.0.0.1:8080/ws/agent）。

.PARAMETER Secret
    注册密钥（服务器密钥或用户 Agent 密钥，必填）。

.PARAMETER BaseUrl
    二进制下载根 URL（默认 GitHub Releases latest）。

.PARAMETER Version
    Agent 版本（默认 latest）。

.PARAMETER InstallDir
    安装目录（默认 %ProgramFiles%\argus-agent）。
#>
[CmdletBinding()]
param(
    [string]$ServerUrl,
    [string]$Secret,
    [string]$BaseUrl = "https://github.com/motao123/Argus/releases/latest/download",
    [string]$Version = "latest",
    [string]$InstallDir = (Join-Path $env:ProgramFiles "argus-agent")
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($ServerUrl) -or [string]::IsNullOrWhiteSpace($Secret)) {
    Write-Host "用法: $PSCommandPath -ServerUrl <ws://server/ws/agent> -Secret <secret> [-BaseUrl <url>] [-Version <ver>] [-InstallDir <dir>]"
    exit 1
}

# 旧版 Windows PowerShell 默认不走 TLS 1.2，需显式启用。
[Net.ServicePointManager]::SecurityProtocol = `
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# ---- 架构识别（amd64 / arm64 / 386）----
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    "x86"   { "386" }
    default { throw "不支持的架构: $env:PROCESSOR_ARCHITECTURE（release 仅提供 amd64/arm64 产物）" }
}

# ---- 下载与强制校验 ----
$BaseUrl = $BaseUrl.TrimEnd('/')
if ($Version -ne "latest") {
    $BaseUrl = $BaseUrl -replace "/latest/download$", "/download/$Version"
}
$file = "argus-agent-windows-$arch.exe"
$tmpDir = Join-Path $env:TEMP ("argus-agent-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
$agentPath = Join-Path $tmpDir $file
$sumsPath = Join-Path $tmpDir "checksums.txt"

try {
    Write-Host "==> 下载 Argus Agent ($arch, version=$Version)"
    Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$file" -OutFile $agentPath

    Write-Host "==> 下载 checksums.txt"
    Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/checksums.txt" -OutFile $sumsPath

    # 供应链强化：checksums.txt 中必须存在当前文件条目，否则中止。
    $expected = $null
    foreach ($line in Get-Content -LiteralPath $sumsPath) {
        $parts = $line -split "\s+"
        if ($parts.Count -ge 2) {
            $name = $parts[1].TrimStart('*')
            if ($name -eq $file) {
                $expected = $parts[0].Trim()
                break
            }
        }
    }
    if ([string]::IsNullOrWhiteSpace($expected)) {
        throw "checksums.txt 中未找到 $file 条目（供应链强化：拒绝无校验安装）"
    }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $agentPath).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) {
        throw "SHA-256 校验失败（期望 $expected，实际 $actual），已中止"
    }
    Write-Host "==> SHA-256 校验通过"

    # ---- 安装 ----
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $target = Join-Path $InstallDir "argus-agent.exe"
    Copy-Item -LiteralPath $agentPath -Destination $target -Force
    Write-Host "==> 已安装到 $target"

    # 命令行参数（ServerUrl/Secret 可能含空格或引号，统一转义后加引号）
    function ConvertTo-ArgValue([string]$v) {
        return '"' + $v.Replace('\', '\\').Replace('"', '\"') + '"'
    }
    $argStr = "-s " + (ConvertTo-ArgValue $ServerUrl) +
              " -k " + (ConvertTo-ArgValue $Secret) +
              " -i 2s -c " + (ConvertTo-ArgValue $InstallDir)

    # ---- 服务 / 计划任务（幂等可重装）----
    $nssm = Get-Command nssm -ErrorAction SilentlyContinue
    if ($nssm) {
        Write-Host "==> 检测到 NSSM，注册为 Windows 服务 argus-agent"
        & $nssm.Source stop argus-agent 2>$null | Out-Null
        & $nssm.Source remove argus-agent confirm 2>$null | Out-Null
        & $nssm.Source install argus-agent $target
        & $nssm.Source set argus-agent AppDirectory $InstallDir
        & $nssm.Source set argus-agent AppParameters $argStr
        & $nssm.Source set argus-agent Start SERVICE_AUTO_START
        & $nssm.Source set argus-agent AppStdout (Join-Path $InstallDir "argus-agent.log")
        & $nssm.Source set argus-agent AppStderr (Join-Path $InstallDir "argus-agent.err.log")
        & $nssm.Source start argus-agent
        Write-Host "==> 服务已启动 (argus-agent)"
    } else {
        Write-Host "==> 未检测到 NSSM，注册为计划任务 argus-agent"
        $action = New-ScheduledTaskAction -Execute $target -Argument $argStr -WorkingDirectory $InstallDir
        $trigger = New-ScheduledTaskTrigger -AtStartup
        $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
            -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -StartWhenAvailable `
            -ExecutionTimeLimit ([TimeSpan]::Zero)
        $principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
        $existing = Get-ScheduledTask -TaskName "argus-agent" -ErrorAction SilentlyContinue
        if ($existing) {
            Unregister-ScheduledTask -TaskName "argus-agent" -Confirm:$false
        }
        Register-ScheduledTask -TaskName "argus-agent" -Action $action -Trigger $trigger `
            -Settings $settings -Principal $principal -Force | Out-Null
        Start-ScheduledTask -TaskName "argus-agent"
        Write-Host "==> 计划任务已启动 (argus-agent)"
    }

    Write-Host ""
    Write-Host "==> 安装完成。可在 Argus 后台查看服务器上线状态。"
    Write-Host "    二进制: $target"
    Write-Host "    配置目录: $InstallDir"
    Write-Host "    服务/任务: argus-agent"
}
finally {
    Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
