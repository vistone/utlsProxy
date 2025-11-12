# 脚本目录

本目录包含版本管理、发布和安装相关的脚本。

## 脚本说明

### 1. install.sh / install.ps1

一键安装脚本，从 GitHub 下载并安装 utlsProxy。

**Linux/macOS 用法**:
```bash
# 方式1: 直接下载并执行
curl -fsSL https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install.sh | bash

# 方式2: 下载后执行
wget https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install.sh
chmod +x install.sh
./install.sh

# 方式3: 指定版本安装
./install.sh v1.0.4
```

**Windows 用法**:
```powershell
# 方式1: 直接下载并执行
irm https://raw.githubusercontent.com/vistone/utlsProxy/main/scripts/install.ps1 | iex

# 方式2: 下载后执行
powershell -ExecutionPolicy Bypass -File install.ps1

# 方式3: 指定版本安装
powershell -ExecutionPolicy Bypass -File install.ps1 v1.0.4
```

**功能**:
- 自动检测操作系统和架构（Linux/macOS/Windows, amd64/arm64）
- 优先从 GitHub Releases 下载预编译二进制文件
- 如果没有预编译文件，则从源码编译安装
- 自动安装到系统路径（Linux/macOS: `/usr/local/bin`, Windows: `C:\Program Files\utlsProxy`）
- 支持安装所有可执行程序（DNS监控、Crawler、TaskClient）

**环境要求**:
- 如果使用预编译二进制：无需额外依赖
- 如果从源码编译：需要 Go 1.25+ 和 Git

**自定义安装目录**:
```bash
# Linux/macOS
INSTALL_DIR=/opt/utlsProxy ./install.sh

# Windows PowerShell
$env:INSTALL_DIR="C:\MyTools\utlsProxy"; .\install.ps1
```

### 2. bump_version.sh

自动增加版本号脚本。

**用法**:
```bash
./scripts/bump_version.sh [patch|minor|major]
```

**参数**:
- `patch` (默认): 小版本号+1 (v1.0.0 → v1.0.1)
- `minor`: 中版本号+1 (v1.0.0 → v1.1.0)
- `major`: 大版本号+1 (v1.0.0 → v2.0.0)

**功能**:
- 从 `VERSION` 文件或 `config/config.toml` 读取当前版本
- 根据类型增加版本号
- 更新 `VERSION` 文件和 `config/config.toml`

**示例**:
```bash
# 增加小版本号
./scripts/bump_version.sh patch

# 增加中版本号
./scripts/bump_version.sh minor

# 增加大版本号
./scripts/bump_version.sh major
```

### 3. create_release.sh

创建GitHub Release脚本。

**用法**:
```bash
./scripts/create_release.sh [版本号] [提交信息]
```

**功能**:
- 创建Git标签
- 推送标签到GitHub
- 使用GitHub CLI创建Release（如果已安装）

**示例**:
```bash
# 使用VERSION文件中的版本号
./scripts/create_release.sh

# 指定版本号和提交信息
./scripts/create_release.sh v1.0.1 "修复热连接池问题"
```

### 4. commit_and_release.sh

一键提交并创建Release脚本（推荐使用）。

**用法**:
```bash
./scripts/commit_and_release.sh [提交信息] [版本类型]
```

**功能**:
- 自动增加版本号
- 提交所有更改
- 推送到GitHub
- 创建Git标签和Release

**示例**:
```bash
# 使用默认提交信息和patch版本
./scripts/commit_and_release.sh

# 指定提交信息
./scripts/commit_and_release.sh "修复热连接池白名单问题"

# 指定版本类型
./scripts/commit_and_release.sh "重大更新" minor
```

## 自动化流程

### GitHub Actions自动发布

项目配置了GitHub Actions workflow (`.github/workflows/release.yml`)，当推送到 `main` 分支时会自动：

1. 检测版本号（从VERSION文件或config.toml）
2. 自动增加小版本号（patch）
3. 更新VERSION文件和config.toml
4. 创建Git标签
5. 创建GitHub Release

**跳过自动发布**:
在提交信息中添加 `[skip release]` 即可跳过：
```bash
git commit -m "更新文档 [skip release]"
```

### 手动发布流程

如果需要手动控制版本发布：

```bash
# 1. 增加版本号
./scripts/bump_version.sh patch

# 2. 提交更改
git add VERSION config/config.toml
git commit -m "Bump version to $(cat VERSION)"

# 3. 创建Release
./scripts/create_release.sh

# 4. 推送
git push origin main
git push origin --tags
```

## 版本文件

- `VERSION`: 存储当前版本号（如 `v1.0.0`）
- `config/config.toml`: 配置文件中的版本号（自动同步）

## 注意事项

1. **版本格式**: 版本号格式为 `v主版本.次版本.修订版本`（如 `v1.0.0`）
2. **Git标签**: 每次发布都会创建对应的Git标签
3. **GitHub CLI**: 如果安装了 `gh` CLI，脚本会自动创建GitHub Release
4. **权限**: 创建Release需要GitHub token权限

