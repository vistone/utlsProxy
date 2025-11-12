#!/bin/bash

# 清理系统上已配置的IPv6地址脚本
# 用法: sudo bash scripts/cleanup_ipv6.sh

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# 检查是否为root用户
if [ "$EUID" -ne 0 ]; then 
    echo -e "${RED}错误: 请使用 sudo 运行此脚本${NC}"
    exit 1
fi

# IPv6子网前缀
SUBNET_PREFIX="2607:8700:5500:2943"
INTERFACE="ipv6net"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}清理IPv6地址脚本${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 检查接口是否存在
if ! ip link show "$INTERFACE" &>/dev/null; then
    echo -e "${YELLOW}警告: 接口 $INTERFACE 不存在${NC}"
    exit 1
fi

echo -e "${GREEN}开始清理 $INTERFACE 接口上的IPv6地址...${NC}"
echo ""

# 获取接口上所有的IPv6地址
addresses=$(ip -6 addr show "$INTERFACE" | grep "inet6 ${SUBNET_PREFIX}" | awk '{print $2}' | cut -d'/' -f1)

if [ -z "$addresses" ]; then
    echo -e "${YELLOW}未找到需要清理的IPv6地址${NC}"
    exit 0
fi

# 统计数量
count=$(echo "$addresses" | wc -l)
echo -e "${BLUE}找到 $count 个IPv6地址需要清理${NC}"
echo ""

# 清理地址
cleaned=0
failed=0

while IFS= read -r addr; do
    if [ -n "$addr" ]; then
        echo -n "清理地址: $addr ... "
        if ip -6 addr del "$addr/128" dev "$INTERFACE" 2>/dev/null; then
            echo -e "${GREEN}成功${NC}"
            cleaned=$((cleaned + 1))
        else
            echo -e "${RED}失败${NC}"
            failed=$((failed + 1))
        fi
    fi
done <<< "$addresses"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}清理完成！${NC}"
echo -e "${GREEN}成功清理: $cleaned 个地址${NC}"
if [ $failed -gt 0 ]; then
    echo -e "${RED}失败: $failed 个地址${NC}"
fi
echo -e "${GREEN}========================================${NC}"

