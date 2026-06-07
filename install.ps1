#!/usr/bin/env pwsh
#Requires -Version 5.1
# ═══════════════════════════════════════════════════════════════════
# XiaoTianQuant Windows One-Line Installer
# Usage:
#   irm https://raw.githubusercontent.com/xiaotian-quant/xiaotian_quant/main/install.ps1 | iex
#   irm ... | iex -Version v3.0.0 -Dir "C:\XiaoTianQuant"
# ═══════════════════════════════════════════════════════════════════

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# ── Colors ──────────────────────────────────────────────────────
function Write-Info    { param($msg) Write-Host "[INFO]  $msg" -ForegroundColor Green }
function Write-Warn    { param($msg) Write-Host "[WARN]  $msg" -ForegroundColor Yellow }
function Write-Error   { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red }
function Write-Step    { param($msg) Write-Host "[STEP]  $msg" -ForegroundColor Cyan }

# ── Banner ──────────────────────────────────────────────────────
function Show-Banner {
    Write-Host @"
`n╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant Windows 一键安装器                    ║
║                                                              ║
║   支持: Windows 10/11 | PowerShell 5.1+ | Docker Desktop     ║
║   架构: AMD64 | ARM64                                        ║
╚══════════════════════════════════════════════════════════════╝
"@ -ForegroundColor Cyan
}

# ── Configuration ───────────────────────────────────────────────
$Repo = "xiaotian-quant/xiaotian_quant"
$Version = if ($env:XTQ_VERSION) { $env:XTQ_VERSION } else { "v3.0.0" }
$InstallDir = if ($env:XTQ_DIR) { $env:XTQ_DIR } else { "$env:USERPROFILE\.xiaotianquant" }
$ForceSource = if ($env:XTQ_SOURCE) { $true } else { $false }

# Detect if running via irm | iex
$IsRemote = $MyInvocation.InvocationName -eq 'Invoke-Expression' -or $MyInvocation.Line -match '^irm\s+'

# Platform
$OS = "windows"
$Arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "arm64" }
$PlatformName = "${OS}-${Arch}"

# ── Download Helper ─────────────────────────────────────────────
function Download-File($url, $out) {
    Write-Info "下载: $url"
    try {
        Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing
    } catch {
        throw "下载失败: $url"
    }
}

# ── Install from Pre-built Release ────────────────────────────
function Install-Binary {
    Write-Step "下载预编译版本 ${Version} (${PlatformName})..."

    $baseUrl = "https://github.com/${Repo}/releases/download/${Version}"
    $archive = "xiaotianquant-${Version}-${PlatformName}.zip"
    $url = "${baseUrl}/${archive}"
    $tmpDir = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    try {
        Download-File $url "$tmpDir\$archive"
    } catch {
        Write-Warn "预编译版本下载失败，将尝试从源码构建..."
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
        return $false
    }

    Write-Info "解压到 ${InstallDir}..."
    if (Test-Path $InstallDir) { Remove-Item -Recurse -Force $InstallDir }
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
    Expand-Archive -Path "$tmpDir\$archive" -DestinationPath $InstallDir -Force

    Remove-Item -Recurse -Force $tmpDir
    Write-Info "预编译版本安装完成"
    return $true
}

# ── Install from Source ───────────────────────────────────────
function Install-Source {
    Write-Step "从源码构建..."

    if ($IsRemote) {
        if (Test-Path "$InstallDir\.git") {
            Write-Info "更新现有仓库..."
            Set-Location $InstallDir
            git pull origin main
        } else {
            Write-Info "克隆仓库到 ${InstallDir}..."
            if (Test-Path $InstallDir) { Remove-Item -Recurse -Force $InstallDir }
            git clone --depth 1 "https://github.com/${Repo}.git" $InstallDir
        }
    }

    Set-Location $InstallDir

    # Install dependencies
    Install-Go
    Install-Node
    Install-Python

    # Build frontend
    Write-Step "构建前端..."
    Set-Location "$InstallDir\web"
    if (-not (Test-Path "node_modules")) {
        & npm ci
    }
    & npm run build

    # Build Go backend
    Write-Step "构建 Go 后端..."
    Set-Location "$InstallDir\gateway"
    if (Test-Path "spa") { Remove-Item "spa\*" -Recurse -Force -ErrorAction SilentlyContinue }
    if (-not (Test-Path "spa")) { New-Item -ItemType Directory -Path "spa" | Out-Null }
    Copy-Item "$InstallDir\web\dist\*" "spa\" -Recurse -Force -ErrorAction SilentlyContinue

    $env:CGO_ENABLED = "0"
    $buildTime = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
    & go build `
        -ldflags="-s -w -X main.version=$Version -X main.buildTime=$buildTime" `
        -trimpath `
        -o gateway-server.exe `
        .\cmd\server

    # Install Python deps
    Write-Step "安装 Python 依赖..."
    Set-Location "$InstallDir\sandbox\ml_server"
    & $script:PythonExe -m pip install -r requirements.txt
    & $script:PythonExe -m pip install stable-baselines3 gymnasium tensorboard scikit-learn

    Write-Info "源码构建完成"
}

# ── Dependency Installers ─────────────────────────────────────
function Install-Go {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Write-Info "Go 已安装: $(go version)"
        return
    }
    Write-Step "安装 Go..."
    $goVer = "1.25.0"
    $goUrl = "https://go.dev/dl/go${goVer}.windows-amd64.msi"
    $goInstaller = "$env:TEMP\go-installer.msi"
    Download-File $goUrl $goInstaller
    Start-Process msiexec.exe -ArgumentList "/i", $goInstaller, "/quiet", "/norestart" -Wait
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    Write-Info "Go 安装完成"
}

function Install-Node {
    if (Get-Command node -ErrorAction SilentlyContinue) {
        Write-Info "Node.js 已安装: $(node --version)"
        return
    }
    Write-Step "安装 Node.js..."
    $nodeUrl = "https://nodejs.org/dist/v22.11.0/node-v22.11.0-x64.msi"
    $nodeInstaller = "$env:TEMP\node-installer.msi"
    Download-File $nodeUrl $nodeInstaller
    Start-Process msiexec.exe -ArgumentList "/i", $nodeInstaller, "/quiet", "/norestart" -Wait
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    Write-Info "Node.js 安装完成"
}

function Install-Python {
    $pythonPaths = @(
        "$env:LOCALAPPDATA\Programs\Python\Python312\python.exe",
        "C:\Python312\python.exe",
        "C:\Program Files\Python312\python.exe"
    )
    $script:PythonExe = $null
    foreach ($path in $pythonPaths) {
        if (Test-Path $path) { $script:PythonExe = $path; break }
    }
    if ($script:PythonExe) {
        Write-Info "Python 已安装: $(& $script:PythonExe --version)"
        return
    }
    Write-Step "安装 Python 3.12..."
    $pyUrl = "https://www.python.org/ftp/python/3.12.9/python-3.12.9-amd64.exe"
    $pyInstaller = "$env:TEMP\python-installer.exe"
    Download-File $pyUrl $pyInstaller
    Start-Process $pyInstaller -ArgumentList "/quiet", "InstallAllUsers=0", "PrependPath=1", "Include_pip=1" -Wait
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
    foreach ($path in $pythonPaths) {
        if (Test-Path $path) { $script:PythonExe = $path; break }
    }
    Write-Info "Python 安装完成"
}

# ── Create Launch Scripts ─────────────────────────────────────
function Create-StartScript {
    Write-Step "创建启动脚本..."

    # start-all.bat
    $startBat = @"
@echo off
title XiaoTianQuant Multi-System Launcher
color 0A

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║     XiaoTianQuant 多系统一键启动器                        ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.

:: Start ML Server
start "ML Server :8001" cmd /c "cd /d $InstallDir\sandbox\ml_server && $script:PythonExe -m uvicorn main:app --host 0.0.0.0 --port 8001"
timeout /t 3 /nobreak >nul

:: Start Go Gateway
start "Gateway :8080" cmd /c "cd /d $InstallDir\gateway && gateway-server.exe"
timeout /t 3 /nobreak >nul

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║  All services started!                                      ║
echo  ║  Frontend: http://localhost:8080                         ║
echo  ║  Gateway:  http://localhost:8080/api                   ║
echo  ║  ML:       http://localhost:8001                         ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.
pause
"@
    $startBat | Out-File -FilePath "$InstallDir\start-all.bat" -Encoding UTF8

    # start.ps1 (PowerShell version)
    $startPs1 = @"
# XiaoTianQuant PowerShell 启动脚本
`$Dir = "$InstallDir"

Write-Host @"
`n╔══════════════════════════════════════════════════════════════╗
║           XiaoTianQuant 多系统启动器                        ║
╚══════════════════════════════════════════════════════════════╝
"@ -ForegroundColor Cyan

# Start ML Server
Write-Info "启动 ML Server :8001"
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd `$Dir\sandbox\ml_server; $script:PythonExe -m uvicorn main:app --host 0.0.0.0 --port 8001" -WindowStyle Normal

Start-Sleep -Seconds 3

# Start Gateway
Write-Info "启动 Gateway :8080"
Start-Process powershell -ArgumentList "-NoExit", "-Command", "cd `$Dir\gateway; .\gateway-server.exe" -WindowStyle Normal

Write-Host @"
`n╔══════════════════════════════════════════════════════════╗
║  All services started!                                    ║
║  Frontend: http://localhost:8080                        ║
║  Gateway:  http://localhost:8080/api                  ║
║  ML:       http://localhost:8001                        ║
╚══════════════════════════════════════════════════════════╝
"@ -ForegroundColor Green
"@
    $startPs1 | Out-File -FilePath "$InstallDir\start.ps1" -Encoding UTF8

    Write-Info "启动脚本已创建:"
    Write-Info "  $InstallDir\start-all.bat  (CMD)"
    Write-Info "  $InstallDir\start.ps1     (PowerShell)"
}

# ── Main ────────────────────────────────────────────────────────
function Main {
    Show-Banner
    Write-Info "平台: Windows ${Arch} → ${PlatformName}"
    Write-Info "安装目录: ${InstallDir}"
    Write-Info "版本: ${Version}"
    if ($IsRemote) { Write-Info "模式: 远程安装 (irm | iex)" }
    if ($ForceSource) { Write-Info "模式: 强制源码构建" }

    # Create install directory
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

    # Try binary install first
    $binarySuccess = $false
    if (-not $ForceSource) {
        $binarySuccess = Install-Binary
    }
    if (-not $binarySuccess) {
        Write-Warn "预编译版本不可用，切换到源码构建..."
        Install-Source
    }

    # Create scripts
    Create-StartScript

    # Done
    Write-Host @"
`n╔══════════════════════════════════════════════════════════════╗
║              ✅ 安装完成！                                    ║
║                                                              ║
║  安装目录: ${InstallDir}                                      ║
║                                                              ║
║  启动方式:                                                   ║
║    cd ${InstallDir}                                         ║
║    .\start-all.bat           # CMD 一键启动                  ║
║    .\start.ps1               # PowerShell 一键启动           ║
║                                                              ║
║  访问地址:                                                   ║
║    http://localhost:8080    # 前端 + API 网关               ║
║    http://localhost:8001    # ML 服务                       ║
║                                                              ║
║  多系统组成:                                                 ║
║    • Go Gateway    :8080  API网关 + 策略引擎                 ║
║    • ML Server     :8001  机器学习服务                       ║
║    • Redis         :6379  缓存/队列 (可选)                   ║
║                                                              ║
║  默认账号: admin / admin123                                  ║
╚══════════════════════════════════════════════════════════════╝
"@ -ForegroundColor Green
}

# Run
Main
