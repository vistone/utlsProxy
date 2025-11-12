#!/bin/bash

# 版本号自动增加脚本
# 用法: ./scripts/bump_version.sh [patch|minor|major]
# 默认: patch (小版本号+1)

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 配置文件路径
CONFIG_FILE="config/config.toml"
VERSION_FILE="VERSION"

# 获取当前版本号
get_current_version() {
    if [ -f "$VERSION_FILE" ]; then
        cat "$VERSION_FILE"
    else
        # 从config.toml读取
        grep -E '^Version=' "$CONFIG_FILE" | sed 's/Version="\(.*\)"/\1/' | head -1
    fi
}

# 解析版本号
parse_version() {
    local version=$1
    # 移除v前缀
    version=${version#v}
    # 分割版本号
    IFS='.' read -r major minor patch <<< "$version"
    echo "$major $minor $patch"
}

# 增加版本号
bump_version() {
    local version=$1
    local bump_type=${2:-patch}
    
    local parsed=$(parse_version "$version")
    read -r major minor patch <<< "$parsed"
    
    case $bump_type in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
        *)
            echo -e "${RED}错误: 无效的版本类型 '$bump_type'${NC}"
            echo "用法: $0 [patch|minor|major]"
            exit 1
            ;;
    esac
    
    echo "v${major}.${minor}.${patch}"
}

# 更新配置文件中的版本号
update_config_version() {
    local new_version=$1
    
    # 更新config.toml
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        sed -i '' "s/^Version=.*/Version=\"${new_version}\"/" "$CONFIG_FILE"
    else
        # Linux
        sed -i "s/^Version=.*/Version=\"${new_version}\"/" "$CONFIG_FILE"
    fi
    
    # 更新VERSION文件
    echo "$new_version" > "$VERSION_FILE"
}

# 主函数
main() {
    local bump_type=${1:-patch}
    
    echo -e "${GREEN}开始更新版本号...${NC}"
    
    # 获取当前版本
    current_version=$(get_current_version)
    if [ -z "$current_version" ]; then
        echo -e "${RED}错误: 无法获取当前版本号${NC}"
        exit 1
    fi
    
    echo -e "当前版本: ${YELLOW}${current_version}${NC}"
    
    # 计算新版本
    new_version=$(bump_version "$current_version" "$bump_type")
    echo -e "新版本: ${GREEN}${new_version}${NC}"
    
    # 更新版本号
    update_config_version "$new_version"
    
    echo -e "${GREEN}版本号已更新为: ${new_version}${NC}"
    echo "$new_version"
}

main "$@"

