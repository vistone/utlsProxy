# utlsProxy Windows 一键安装脚本 (PowerShell)
# 用法: powershell -ExecutionPolicy Bypass -File install.ps1
# 或: irm https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"

# GitHub 仓库信息
$GITHUB_REPO = "vistone/utlsProxy"
$GITHUB_URL = "https://github.com/$GITHUB_REPO"
$RELEASE_API_URL = "https://api.github.com/repos/$GITHUB_REPO/releases/latest"

# 安装目录
$INSTALL_DIR = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:ProgramFiles\utlsProxy" }
$BINARY_NAME = "utlsProxy"

# 颜色输出函数
function Write-Info {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "[SUCCESS] $Message" -ForegroundColor Green
}

function Write-Warn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor Yellow
}

function Write-Error {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

# 检测平台
function Get-Platform {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -eq "AMD64") {
        return "windows_amd64"
    } elseif ($arch -eq "ARM64") {
        return "windows_arm64"
    } else {
        Write-Error "不支持的架构: $arch"
        exit 1
    }
}

# 检查命令是否存在
function Test-Command {
    param([string]$Command)
    $null = Get-Command $Command -ErrorAction SilentlyContinue
    return $?
}

# 检查 Go 环境
function Test-Go {
    if (-not (Test-Command "go")) {
        Write-Error "未找到 Go 环境，请先安装 Go 1.25 或更高版本"
        Write-Info "安装 Go: https://golang.org/dl/"
        exit 1
    }
    
    $goVersion = (go version).Split(' ')[2]
    Write-Info "检测到 Go 版本: $goVersion"
}

# 获取最新版本号
function Get-LatestVersion {
    try {
        $response = Invoke-RestMethod -Uri $RELEASE_API_URL
        return $response.tag_name
    } catch {
        Write-Warn "无法获取最新版本号，将使用 main 分支"
        return "main"
    }
}

# 从 GitHub Releases 下载预编译二进制
function Get-ReleaseBinary {
    param(
        [string]$Platform,
        [string]$Version
    )
    
    Write-Info "尝试从 GitHub Releases 下载预编译二进制..."
    
    $assetName = "${BINARY_NAME}_${Platform}.exe"
    $downloadUrl = "$GITHUB_URL/releases/download/$Version/$assetName"
    $tempFile = "$env:TEMP\$assetName"
    
    Write-Info "下载地址: $downloadUrl"
    
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempFile -UseBasicParsing
        if (Test-Path $tempFile) {
            return $tempFile
        }
    } catch {
        Write-Warn "下载预编译二进制失败: $_"
    }
    
    return $null
}

# 从源码编译安装
function Build-FromSource {
    param([string]$Version)
    
    Write-Info "从源码编译安装..."
    
    # 创建临时目录
    $tempDir = Join-Path $env:TEMP "utlsProxy-build-$(Get-Random)"
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
    
    Write-Info "克隆仓库到临时目录..."
    
    if (Test-Command "git") {
        if ($Version -eq "main" -or $Version -eq "latest") {
            git clone --depth 1 "$GITHUB_URL.git" $tempDir
        } else {
            git clone --depth 1 --branch $Version "$GITHUB_URL.git" $tempDir
        }
    } else {
        Write-Error "未找到 git，无法从源码编译"
        exit 1
    }
    
    Push-Location $tempDir
    
    Write-Info "开始编译..."
    
    $buildErrors = 0
    
    # 编译 DNS 监控程序
    if (Test-Path "cmd\DNS") {
        Write-Info "编译 DNS 监控程序..."
        Push-Location "cmd\DNS"
        if (go build -o "$tempDir\dns-monitor.exe") {
            Write-Success "DNS 监控程序编译成功"
        } else {
            Write-Warn "DNS 监控程序编译失败"
            $buildErrors++
        }
        Pop-Location
    }
    
    # 编译 Crawler
    if (Test-Path "cmd\Crawler") {
        Write-Info "编译 Crawler..."
        Push-Location "cmd\Crawler"
        if (go build -o "$tempDir\crawler.exe") {
            Write-Success "Crawler 编译成功"
        } else {
            Write-Warn "Crawler 编译失败"
            $buildErrors++
        }
        Pop-Location
    }
    
    # 编译 TaskClient
    if (Test-Path "cmd\TaskClient") {
        Write-Info "编译 TaskClient..."
        Push-Location "cmd\TaskClient"
        if (go build -o "$tempDir\task-client.exe") {
            Write-Success "TaskClient 编译成功"
        } else {
            Write-Warn "TaskClient 编译失败"
            $buildErrors++
        }
        Pop-Location
    }
    
    Pop-Location
    
    if ($buildErrors -gt 0) {
        Write-Warn "部分程序编译失败，但会继续安装已编译的程序"
    }
    
    return $tempDir
}

# 安装二进制文件
function Install-Binary {
    param(
        [string]$SourcePath,
        [string]$TargetName
    )
    
    # 确保安装目录存在
    if (-not (Test-Path $INSTALL_DIR)) {
        Write-Info "创建安装目录: $INSTALL_DIR"
        New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null
    }
    
    $targetPath = Join-Path $INSTALL_DIR $TargetName
    
    # 复制文件
    Write-Info "安装到: $targetPath"
    Copy-Item -Path $SourcePath -Destination $targetPath -Force
    
    Write-Success "安装成功: $TargetName"
}

# 主安装函数
function Main {
    param([string]$Version = "latest")
    
    Write-Host ""
    Write-Info "========================================="
    Write-Info "  utlsProxy 一键安装脚本"
    Write-Info "========================================="
    Write-Host ""
    
    # 检测平台
    $platform = Get-Platform
    Write-Info "检测到平台: $platform"
    
    # 获取版本号
    if ($Version -eq "latest") {
        Write-Info "获取最新版本号..."
        $Version = Get-LatestVersion
        Write-Info "版本: $Version"
    }
    
    # 尝试从 Releases 下载预编译二进制
    $downloadedFile = $null
    if ($Version -ne "main" -and $Version -ne "latest") {
        $downloadedFile = Get-ReleaseBinary $platform $Version
    }
    
    if ($downloadedFile -and (Test-Path $downloadedFile)) {
        Write-Success "成功下载预编译二进制文件"
        
        # 安装主程序
        Install-Binary $downloadedFile "$BINARY_NAME.exe"
        
        # 清理临时文件
        Remove-Item $downloadedFile -Force
    } else {
        Write-Warn "未找到预编译二进制文件，将从源码编译安装"
        
        # 检查 Go 环境
        Test-Go
        
        # 从源码编译
        $buildDir = Build-FromSource $Version
        
        # 安装编译好的程序
        if (Test-Path "$buildDir\dns-monitor.exe") {
            Install-Binary "$buildDir\dns-monitor.exe" "utlsProxy-dns.exe"
        }
        
        if (Test-Path "$buildDir\crawler.exe") {
            Install-Binary "$buildDir\crawler.exe" "utlsProxy-crawler.exe"
        }
        
        if (Test-Path "$buildDir\task-client.exe") {
            Install-Binary "$buildDir\task-client.exe" "utlsProxy-task-client.exe"
        }
        
        # 清理临时目录
        Remove-Item $buildDir -Recurse -Force
    }
    
    Write-Host ""
    Write-Success "========================================="
    Write-Success "  安装完成！"
    Write-Success "========================================="
    Write-Host ""
    
    # 显示安装的程序
    Write-Info "已安装的程序:"
    if (Test-Path "$INSTALL_DIR\$BINARY_NAME.exe") {
        Write-Host "  - $INSTALL_DIR\$BINARY_NAME.exe"
    }
    if (Test-Path "$INSTALL_DIR\utlsProxy-dns.exe") {
        Write-Host "  - $INSTALL_DIR\utlsProxy-dns.exe"
    }
    if (Test-Path "$INSTALL_DIR\utlsProxy-crawler.exe") {
        Write-Host "  - $INSTALL_DIR\utlsProxy-crawler.exe"
    }
    if (Test-Path "$INSTALL_DIR\utlsProxy-task-client.exe") {
        Write-Host "  - $INSTALL_DIR\utlsProxy-task-client.exe"
    }
    
    Write-Host ""
    Write-Info "使用方法:"
    if (Test-Path "$INSTALL_DIR\utlsProxy-dns.exe") {
        Write-Host "  utlsProxy-dns.exe          # 运行 DNS 监控程序"
    }
    if (Test-Path "$INSTALL_DIR\utlsProxy-crawler.exe") {
        Write-Host "  utlsProxy-crawler.exe      # 运行 Crawler"
    }
    if (Test-Path "$INSTALL_DIR\utlsProxy-task-client.exe") {
        Write-Host "  utlsProxy-task-client.exe  # 运行 TaskClient"
    }
    Write-Host ""
    
    # 添加到 PATH（可选）
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -notlike "*$INSTALL_DIR*") {
        Write-Info "是否要将 $INSTALL_DIR 添加到 PATH 环境变量？(Y/N)"
        $response = Read-Host
        if ($response -eq "Y" -or $response -eq "y") {
            [Environment]::SetEnvironmentVariable("Path", "$currentPath;$INSTALL_DIR", "User")
            Write-Success "已添加到 PATH，请重新打开终端窗口"
        }
    }
}

# 运行主函数
Main $args[0]

