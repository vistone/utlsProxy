#!/bin/bash

# utlsProxy 一键安装脚本
# 从 GitHub 下载并安装 utlsProxy
# 用法: curl -fsSL https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install.sh | bash
# 或: bash <(curl -fsSL https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install.sh)

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# GitHub 仓库信息
GITHUB_REPO="vistone/utlsProxy"
GITHUB_URL="https://github.com/${GITHUB_REPO}"
RELEASE_API_URL="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"

# 安装目录
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="utlsProxy"

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检测操作系统和架构
detect_platform() {
    local os=""
    local arch=""
    
    case "$(uname -s)" in
        Linux*)
            os="linux"
            ;;
        Darwin*)
            os="darwin"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            os="windows"
            ;;
        *)
            log_error "不支持的操作系统: $(uname -s)"
            exit 1
            ;;
    esac
    
    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        arm64|aarch64)
            arch="arm64"
            ;;
        i386|i686)
            arch="386"
            ;;
        *)
            log_error "不支持的架构: $(uname -m)"
            exit 1
            ;;
    esac
    
    echo "${os}_${arch}"
}

# 检查命令是否存在
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# 检查 Go 环境
check_go() {
    if ! command_exists go; then
        log_error "未找到 Go 环境，请先安装 Go 1.25 或更高版本"
        log_info "安装 Go: https://golang.org/dl/"
        exit 1
    fi
    
    local go_version=$(go version | awk '{print $3}' | sed 's/go//')
    log_info "检测到 Go 版本: ${go_version}"
}

# 从 GitHub Releases 下载预编译二进制
download_from_release() {
    local platform=$1
    local version=$2
    
    log_info "尝试从 GitHub Releases 下载预编译二进制..."
    
    # 构建下载 URL
    local asset_name="${BINARY_NAME}_${platform}"
    if [ "$(echo $platform | cut -d'_' -f1)" = "windows" ]; then
        asset_name="${asset_name}.exe"
    fi
    
    local download_url="${GITHUB_URL}/releases/download/${version}/${asset_name}"
    local temp_file="/tmp/${asset_name}"
    
    log_info "下载地址: ${download_url}"
    
    # 尝试下载
    if command_exists curl; then
        if curl -fsSL -o "${temp_file}" "${download_url}"; then
            echo "${temp_file}"
            return 0
        fi
    elif command_exists wget; then
        if wget -q -O "${temp_file}" "${download_url}"; then
            echo "${temp_file}"
            return 0
        fi
    fi
    
    return 1
}

# 获取最新版本号
get_latest_version() {
    if command_exists curl; then
        curl -fsSL "${RELEASE_API_URL}" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | head -1
    elif command_exists wget; then
        wget -q -O - "${RELEASE_API_URL}" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | head -1
    else
        log_warn "未找到 curl 或 wget，无法获取最新版本号"
        echo "latest"
    fi
}

# 从源码编译安装
build_from_source() {
    local version=$1
    
    log_info "从源码编译安装..." >&2
    
    # 创建临时目录
    local temp_dir=$(mktemp -d)
    trap "rm -rf ${temp_dir}" EXIT
    
    log_info "克隆仓库到临时目录..." >&2
    if command_exists git; then
        git clone --depth 1 --branch "${version}" "${GITHUB_URL}.git" "${temp_dir}" 2>/dev/null || \
        git clone --depth 1 "${GITHUB_URL}.git" "${temp_dir}"
    else
        log_error "未找到 git，无法从源码编译" >&2
        exit 1
    fi
    
    cd "${temp_dir}"
    
    log_info "开始编译..." >&2
    
    # 编译所有可执行文件
    local build_errors=0
    
    # 编译 DNS 监控程序
    if [ -d "cmd/DNS" ]; then
        log_info "编译 DNS 监控程序..." >&2
        if go build -o "${temp_dir}/dns-monitor" ./cmd/DNS; then
            log_success "DNS 监控程序编译成功" >&2
        else
            log_warn "DNS 监控程序编译失败" >&2
            build_errors=$((build_errors + 1))
        fi
    fi
    
    # 编译 Crawler
    if [ -d "cmd/Crawler" ]; then
        log_info "编译 Crawler..." >&2
        if go build -o "${temp_dir}/crawler" ./cmd/Crawler; then
            log_success "Crawler 编译成功" >&2
        else
            log_warn "Crawler 编译失败" >&2
            build_errors=$((build_errors + 1))
        fi
    fi
    
    # 编译 TaskClient
    if [ -d "cmd/TaskClient" ]; then
        log_info "编译 TaskClient..." >&2
        if go build -o "${temp_dir}/task-client" ./cmd/TaskClient; then
            log_success "TaskClient 编译成功" >&2
        else
            log_warn "TaskClient 编译失败" >&2
            build_errors=$((build_errors + 1))
        fi
    fi
    
    if [ $build_errors -gt 0 ]; then
        log_warn "部分程序编译失败，但会继续安装已编译的程序" >&2
    fi
    
    # 只输出目录路径到标准输出
    echo "${temp_dir}"
}

# 安装二进制文件
install_binary() {
    local source_path=$1
    local target_name=$2
    
    # 确保安装目录存在
    if [ ! -d "${INSTALL_DIR}" ]; then
        log_info "创建安装目录: ${INSTALL_DIR}"
        sudo mkdir -p "${INSTALL_DIR}"
    fi
    
    local target_path="${INSTALL_DIR}/${target_name}"
    
    # 复制文件
    log_info "安装到: ${target_path}"
    sudo cp "${source_path}" "${target_path}"
    sudo chmod +x "${target_path}"
    
    log_success "安装成功: ${target_name}"
}

# 主安装函数
main() {
    local version=${1:-latest}
    
    echo ""
    log_info "========================================="
    log_info "  utlsProxy 一键安装脚本"
    log_info "========================================="
    echo ""
    
    # 检测平台
    local platform=$(detect_platform)
    log_info "检测到平台: ${platform}"
    
    # 获取版本号
    if [ "$version" = "latest" ]; then
        log_info "获取最新版本号..."
        version=$(get_latest_version)
        if [ -z "$version" ] || [ "$version" = "latest" ]; then
            version="main"
            log_warn "无法获取最新版本号，将使用 main 分支"
        else
            log_info "最新版本: ${version}"
        fi
    fi
    
    # 尝试从 Releases 下载预编译二进制
    local downloaded_file=""
    if [ "$version" != "main" ] && [ "$version" != "latest" ]; then
        downloaded_file=$(download_from_release "$platform" "$version" 2>/dev/null || echo "")
    fi
    
    if [ -n "$downloaded_file" ] && [ -f "$downloaded_file" ]; then
        log_success "成功下载预编译二进制文件"
        
        # 安装主程序
        install_binary "$downloaded_file" "$BINARY_NAME"
        
        # 清理临时文件
        rm -f "$downloaded_file"
    else
        log_warn "未找到预编译二进制文件，将从源码编译安装"
        
        # 检查 Go 环境
        check_go
        
        # 从源码编译
        local build_dir=$(build_from_source "$version")
        
        # 安装编译好的程序
        cd "$build_dir"
        
        if [ -f "dns-monitor" ]; then
            install_binary "dns-monitor" "utlsProxy-dns"
        fi
        
        if [ -f "crawler" ]; then
            install_binary "crawler" "utlsProxy-crawler"
        fi
        
        if [ -f "task-client" ]; then
            install_binary "task-client" "utlsProxy-task-client"
        fi
        
        # 清理临时目录
        rm -rf "$build_dir"
    fi
    
    echo ""
    log_success "========================================="
    log_success "  安装完成！"
    log_success "========================================="
    echo ""
    
    # 显示安装的程序
    log_info "已安装的程序:"
    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        echo "  - ${INSTALL_DIR}/${BINARY_NAME}"
    fi
    if [ -f "${INSTALL_DIR}/utlsProxy-dns" ]; then
        echo "  - ${INSTALL_DIR}/utlsProxy-dns"
    fi
    if [ -f "${INSTALL_DIR}/utlsProxy-crawler" ]; then
        echo "  - ${INSTALL_DIR}/utlsProxy-crawler"
    fi
    if [ -f "${INSTALL_DIR}/utlsProxy-task-client" ]; then
        echo "  - ${INSTALL_DIR}/utlsProxy-task-client"
    fi
    
    echo ""
    log_info "使用方法:"
    if [ -f "${INSTALL_DIR}/utlsProxy-dns" ]; then
        echo "  utlsProxy-dns          # 运行 DNS 监控程序"
    fi
    if [ -f "${INSTALL_DIR}/utlsProxy-crawler" ]; then
        echo "  utlsProxy-crawler      # 运行 Crawler"
    fi
    if [ -f "${INSTALL_DIR}/utlsProxy-task-client" ]; then
        echo "  utlsProxy-task-client  # 运行 TaskClient"
    fi
    echo ""
}

# 运行主函数
main "$@"

