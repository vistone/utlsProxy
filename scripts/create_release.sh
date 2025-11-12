#!/bin/bash

# 创建GitHub Release脚本
# 用法: ./scripts/create_release.sh [版本号] [提交信息]

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
REPO_OWNER="vistone"
REPO_NAME="utlsProxy"
VERSION_FILE="VERSION"

# 获取版本号
get_version() {
    if [ -n "$1" ]; then
        # 清理版本号，移除可能的颜色代码和额外文本
        echo "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1
    elif [ -f "$VERSION_FILE" ]; then
        cat "$VERSION_FILE" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
    else
        echo -e "${RED}错误: 未指定版本号且VERSION文件不存在${NC}"
        exit 1
    fi
}

# 获取最新提交信息
get_latest_commit_message() {
    if [ -n "$2" ]; then
        echo "$2"
    else
        git log -1 --pretty=%B
    fi
}

# 生成Release说明
generate_release_notes() {
    local version=$1
    local commit_msg=$2
    
    cat <<EOF
## 版本 ${version}

### 变更内容

${commit_msg}

### 详细变更

\`\`\`
$(git log --oneline -10)
\`\`\`

### 文档

详细文档请查看 [docs/](docs/) 目录。

### 下载

- 源代码: [v${version}](https://github.com/${REPO_OWNER}/${REPO_NAME}/archive/refs/tags/v${version}.zip)
- 查看所有版本: [Releases](https://github.com/${REPO_OWNER}/${REPO_NAME}/releases)
EOF
}

# 创建Git标签
create_tag() {
    local version=$1
    local message=$2
    
    echo -e "${BLUE}创建Git标签: ${version}${NC}"
    
    # 检查标签是否已存在
    if git rev-parse "$version" >/dev/null 2>&1; then
        echo -e "${YELLOW}警告: 标签 ${version} 已存在，跳过创建${NC}"
        return 0
    fi
    
    git tag -a "$version" -m "$message"
    echo -e "${GREEN}标签创建成功${NC}"
}

# 推送标签到GitHub
push_tag() {
    local version=$1
    
    echo -e "${BLUE}推送标签到GitHub...${NC}"
    git push origin "$version"
    echo -e "${GREEN}标签推送成功${NC}"
}

# 创建GitHub Release（使用GitHub CLI）
create_github_release() {
    local version=$1
    local release_notes=$2
    
    echo -e "${BLUE}创建GitHub Release...${NC}"
    
    # 检查是否安装了gh CLI
    if ! command -v gh &> /dev/null; then
        echo -e "${YELLOW}警告: GitHub CLI (gh) 未安装，跳过自动创建Release${NC}"
        echo -e "${YELLOW}请手动创建Release: https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/new${NC}"
        return 1
    fi
    
    # 检查是否已登录
    if ! gh auth status &> /dev/null; then
        echo -e "${YELLOW}警告: 未登录GitHub CLI，跳过自动创建Release${NC}"
        echo -e "${YELLOW}请运行: gh auth login${NC}"
        return 1
    fi
    
    # 创建Release
    echo "$release_notes" | gh release create "$version" \
        --title "Release ${version}" \
        --notes-file - \
        --repo "${REPO_OWNER}/${REPO_NAME}"
    
    echo -e "${GREEN}GitHub Release创建成功${NC}"
    echo -e "${GREEN}Release URL: https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/tag/${version}${NC}"
}

# 主函数
main() {
    local version=$(get_version "$1")
    local commit_msg=$(get_latest_commit_message "$1" "$2")
    
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}创建 Release: ${version}${NC}"
    echo -e "${GREEN}========================================${NC}"
    
    # 生成Release说明
    release_notes=$(generate_release_notes "$version" "$commit_msg")
    
    # 创建标签
    create_tag "$version" "Release ${version}"
    
    # 推送标签
    push_tag "$version"
    
    # 创建GitHub Release
    create_github_release "$version" "$release_notes"
    
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Release创建完成！${NC}"
    echo -e "${GREEN}========================================${NC}"
}

main "$@"

