#!/bin/bash

# 提交并自动创建Release脚本
# 用法: ./scripts/commit_and_release.sh [提交信息] [版本类型:patch|minor|major]

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 脚本目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

# 参数
COMMIT_MSG=${1:-"更新代码"}
BUMP_TYPE=${2:-patch}

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}自动提交和发布流程${NC}"
echo -e "${BLUE}========================================${NC}"

# 1. 检查是否有未提交的更改
if [ -z "$(git status --porcelain)" ]; then
    echo -e "${YELLOW}没有未提交的更改${NC}"
    exit 0
fi

# 2. 增加版本号
echo -e "${GREEN}步骤1: 增加版本号 (${BUMP_TYPE})${NC}"
NEW_VERSION=$("$SCRIPT_DIR/bump_version.sh" "$BUMP_TYPE")
echo -e "${GREEN}新版本: ${NEW_VERSION}${NC}"

# 3. 添加所有更改
echo -e "${GREEN}步骤2: 添加更改到暂存区${NC}"
git add -A

# 4. 提交更改
echo -e "${GREEN}步骤3: 提交更改${NC}"
git commit -m "$COMMIT_MSG

版本: ${NEW_VERSION}"

# 5. 推送提交
echo -e "${GREEN}步骤4: 推送到GitHub${NC}"
git push origin main

# 6. 创建Release
echo -e "${GREEN}步骤5: 创建Release${NC}"
"$SCRIPT_DIR/create_release.sh" "$NEW_VERSION" "$COMMIT_MSG"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}完成！${NC}"
echo -e "${GREEN}版本: ${NEW_VERSION}${NC}"
echo -e "${GREEN}========================================${NC}"

