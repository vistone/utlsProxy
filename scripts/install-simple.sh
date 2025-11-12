#!/bin/bash

# utlsProxy 简化安装脚本（备用方案）
# 如果主安装脚本无法访问，可以使用此脚本
# 用法: curl -fsSL https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install-simple.sh | bash

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# GitHub 仓库信息
GITHUB_REPO="vistone/utlsProxy"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}"

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

# 从 GitHub API 下载文件内容
download_file_from_api() {
    local file_path=$1
    local output_file=$2
    
    local api_url="${GITHUB_API}/contents/${file_path}"
    
    if command_exists curl; then
        # 使用 GitHub API 下载文件
        curl -fsSL -H "Accept: application/vnd.github.v3.raw" "${api_url}" -o "${output_file}"
    elif command_exists wget; then
        wget -q --header="Accept: application/vnd.github.v3.raw" -O "${output_file}" "${api_url}"
    else
        log_error "未找到 curl 或 wget"
        return 1
    fi
}

# 从源码编译安装
install_from_source() {
    local version=${1:-main}
    
    log_info "从源码编译安装..."
    
    # 创建临时目录
    local temp_dir=$(mktemp -d)
    trap "rm -rf ${temp_dir}" EXIT
    
    log_info "克隆仓库..."
    if command_exists git; then
        if [ "$version" = "main" ] || [ "$version" = "latest" ]; then
            git clone --depth 1 "https://github.com/${GITHUB_REPO}.git" "${temp_dir}"
        else
            git clone --depth 1 --branch "${version}" "https://github.com/${GITHUB_REPO}.git" "${temp_dir}"
        fi
    else
        log_error "未找到 git，无法从源码编译"
        exit 1
    fi
    
    cd "${temp_dir}"
    
    log_info "开始编译..."
    
    # 编译 DNS 监控程序
    if [ -d "cmd/DNS" ]; then
        log_info "编译 DNS 监控程序..."
        if go build -o "${temp_dir}/dns-monitor" ./cmd/DNS; then
            log_success "DNS 监控程序编译成功"
            sudo cp "${temp_dir}/dns-monitor" "/usr/local/bin/utlsProxy-dns"
            sudo chmod +x "/usr/local/bin/utlsProxy-dns"
        fi
    fi
    
    # 编译 Crawler
    if [ -d "cmd/Crawler" ]; then
        log_info "编译 Crawler..."
        if go build -o "${temp_dir}/crawler" ./cmd/Crawler; then
            log_success "Crawler 编译成功"
            sudo cp "${temp_dir}/crawler" "/usr/local/bin/utlsProxy-crawler"
            sudo chmod +x "/usr/local/bin/utlsProxy-crawler"
        fi
    fi
    
    # 编译 TaskClient
    if [ -d "cmd/TaskClient" ]; then
        log_info "编译 TaskClient..."
        if go build -o "${temp_dir}/task-client" ./cmd/TaskClient; then
            log_success "TaskClient 编译成功"
            sudo cp "${temp_dir}/task-client" "/usr/local/bin/utlsProxy-task-client"
            sudo chmod +x "/usr/local/bin/utlsProxy-task-client"
        fi
    fi
    
    # 清理临时目录
    rm -rf "${temp_dir}"
}

# 主函数
main() {
    echo ""
    log_info "========================================="
    log_info "  utlsProxy 简化安装脚本"
    log_info "========================================="
    echo ""
    
    # 检查 Go 环境
    check_go
    
    # 从源码编译安装
    install_from_source "main"
    
    echo ""
    log_success "========================================="
    log_success "  安装完成！"
    log_success "========================================="
    echo ""
    
    log_info "已安装的程序:"
    [ -f "/usr/local/bin/utlsProxy-dns" ] && echo "  - /usr/local/bin/utlsProxy-dns"
    [ -f "/usr/local/bin/utlsProxy-crawler" ] && echo "  - /usr/local/bin/utlsProxy-crawler"
    [ -f "/usr/local/bin/utlsProxy-task-client" ] && echo "  - /usr/local/bin/utlsProxy-task-client"
    echo ""
}

main "$@"


