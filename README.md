# utlsProxy - 基于uTLS的智能代理系统

utlsProxy 是一个基于 uTLS 库的智能代理系统，能够模拟各种浏览器的 TLS 指纹，实现防指纹识别的网络请求。该项目包含多个核心组件，用于管理 IP 地址池、域名监控、访问控制、TLS 指纹模拟和热连接池管理。

## 项目特性

- **TLS指纹伪装**：使用UTLS库模拟真实浏览器的TLS握手特征，有效规避TLS指纹检测
- **智能IP管理**：支持IPv4/IPv6双栈，自动检测环境并动态生成IPv6地址
- **域名IP监控**：自动监控域名IP变化，并发DNS查询，增量更新IP池
- **热连接池**：预热连接池，自动管理连接健康状态，支持连接复用
- **IP访问控制**：基于黑白名单的IP访问控制，支持动态管理
- **接口驱动设计**：所有组件都采用接口设计，易于扩展和测试

## 项目结构

```
utlsProxy/
├── cmd/
│   └── DNS/              # DNS 监控主程序
├── config/               # 配置管理
│   ├── config.go         # 配置结构定义
│   └── config.toml       # 配置文件示例
├── docs/                 # 详细文档目录
│   ├── Config.md                    # 配置说明文档
│   ├── LocalIPPool.md               # 本地IP池文档
│   ├── RemoteDomainIPPool.md        # 域名IP监控文档
│   ├── UTlsClient.md                # UTLS客户端文档
│   ├── UtlsClientHotConnPool.md     # 热连接池文档
│   └── WhiteBlackIPPool.md          # IP访问控制文档
├── domain_ips/           # 域名 IP 地址存储目录
├── src/                  # 核心源代码
│   ├── LocalIPPool.go              # 本地IP地址池
│   ├── RemoteDomainIPPool.go      # 远程域名IP监控
│   ├── UTlsClient.go              # UTLS客户端
│   ├── UtlsClientHotConnPool.go   # 热连接池
│   ├── UTlsFingerPrint.go         # TLS指纹库
│   ├── WhiteBlackIPPool.go        # IP黑白名单
│   └── DNSServerNames.json        # DNS服务器配置
├── test/                 # 单元测试代码
├── go.mod                # Go 模块定义
└── go.sum                # Go 模块校验和
```

## 核心组件

### 1. UTlsClient - UTLS客户端

**文件**: [UTlsClient.go](src/UTlsClient.go) | **文档**: [UTlsClient.md](docs/UTlsClient.md)

功能强大的HTTP/HTTPS客户端，支持TLS指纹伪装、IPv4/IPv6双栈、HTTP/2和HTTP/1.1协议自动协商。

**核心功能**：
- TLS指纹伪装：模拟真实浏览器的TLS握手特征
- 多协议支持：自动支持HTTP/2和HTTP/1.1协议
- IPv4/IPv6双栈：完整支持IPv4和IPv6地址
- 智能连接降级：IP直连失败时自动降级到域名连接
- 本地IP绑定：支持指定本地IP地址进行连接
- 自动请求头填充：自动填充User-Agent和Accept-Language

**主要方法**：
- `NewUTlsClient() *UTlsClient` - 创建客户端实例
- `Do(req *UTlsRequest) (*UTlsResponse, error)` - 执行HTTP请求

### 2. UtlsClientHotConnPool - 热连接池

**文件**: [UtlsClientHotConnPool.go](src/UtlsClientHotConnPool.go) | **文档**: [UtlsClientHotConnPool.md](docs/UtlsClientHotConnPool.md)

智能热连接池，支持连接复用、自动健康管理、IP黑白名单自动更新。

**核心功能**：
- **连接复用**：优先使用池中的连接，减少创建开销
- **预热机制**：系统启动时测试所有IP，填充黑白名单
- **健康管理**：根据HTTP状态码自动分类连接（200→健康池，403→黑名单）
- **IP追踪**：连接元数据包含目标IP，支持黑白名单自动更新
- **空闲清理**：自动清理超时连接，释放资源
- **IPv6优先**：优先使用IPv6连接，失败时自动降级

**设计初衷**：
- 系统启动时白名单和黑名单都是空的
- 预热阶段测试所有IP，根据结果填充黑白名单
- 200状态码的IP加入白名单，403的IP加入黑名单

**主要方法**：
- `NewDomainHotConnPool(config DomainConnPoolConfig) (HotConnPool, error)` - 创建连接池
- `Warmup() error` - 预热连接池
- `GetConn() (*utls.UConn, error)` - 获取连接
- `ReturnConn(conn *utls.UConn, statusCode int) error` - 归还连接
- `Close() error` - 关闭连接池

### 3. LocalIPPool - 本地IP地址池

**文件**: [LocalIPPool.go](src/LocalIPPool.go) | **文档**: [LocalIPPool.md](docs/LocalIPPool.md)

智能IP地址池，能够自动适应运行环境，支持IPv4和IPv6地址管理。

**核心功能**：
- **环境自适应**：自动检测IPv6子网是否可用，智能降级
- **动态IPv6生成**：在支持IPv6的环境中动态生成海量IPv6地址
- **接口驱动设计**：通过 `IPPool` 接口实现解耦
- **并发安全**：使用channel实现高效队列，支持并发访问

**主要方法**：
- `NewLocalIPPool(staticIPv4s []string, ipv6SubnetCIDR string) (IPPool, error)` - 创建IP池
- `GetIP() net.IP` - 获取IP地址
- `Close() error` - 关闭IP池

### 4. RemoteDomainIPPool - 域名IP监控

**文件**: [RemoteDomainIPPool.go](src/RemoteDomainIPPool.go) | **文档**: [RemoteDomainIPPool.md](docs/RemoteDomainIPPool.md)

域名IP地址监控服务，持续监控指定域名的IP地址变化，并将结果持久化存储。

**核心功能**：
- **并发DNS解析**：同时向多个DNS服务器发起请求，获取最全面的IP列表
- **增量更新**：只查询新发现的IP地址，节省API调用次数
- **只增不减**：IP池持续增长，已记录的IP不会删除
- **隔离存储**：每个域名生成独立的存储文件
- **多格式支持**：支持JSON、YAML、TOML格式

**主要方法**：
- `NewRemoteIPMonitor(config MonitorConfig) (DomainMonitor, error)` - 创建监控实例
- `Start()` - 启动监控服务
- `Stop()` - 停止监控服务
- `GetDomainPool(domain string) (map[string][]IPRecord, bool)` - 获取域名IP池

### 5. WhiteBlackIPPool - IP访问控制器

**文件**: [WhiteBlackIPPool.go](src/WhiteBlackIPPool.go) | **文档**: [WhiteBlackIPPool.md](docs/WhiteBlackIPPool.md)

基于内存的IP访问控制器，提供并发安全的黑白名单管理功能。

**核心功能**：
- **黑白名单管理**：支持动态添加和删除IP地址
- **安全策略**：黑名单优先、默认拒绝策略
- **并发安全**：使用读写锁保护，支持多读并发
- **接口驱动设计**：通过 `IPAccessController` 接口实现解耦

**主要方法**：
- `NewWhiteBlackIPPool() IPAccessController` - 创建访问控制器
- `AddIP(ip string, isWhite bool)` - 添加IP到指定名单
- `RemoveIP(ip string, isWhite bool)` - 从指定名单删除IP
- `IsIPAllowed(ip string) bool` - 检查IP是否允许访问
- `GetAllowedIPs() []string` - 获取白名单IP列表
- `GetBlockedIPs() []string` - 获取黑名单IP列表

### 6. UTlsFingerPrint - TLS指纹库

**文件**: [UTlsFingerPrint.go](src/UTlsFingerPrint.go)

浏览器TLS指纹模拟库，包含多种浏览器和平台的指纹配置。

**核心功能**：
- 支持多种浏览器（Chrome, Firefox, Safari, Edge）
- 支持不同平台（Windows, macOS, Linux, iOS）
- 提供随机指纹选择功能
- 支持根据浏览器类型或平台筛选指纹

**主要方法**：
- `GetRandomFingerprint() Profile` - 获取随机指纹
- `ProfileByName(name string) (*Profile, error)` - 根据名称查找指纹
- `ProfilesByBrowser(browser string) []Profile` - 根据浏览器筛选指纹

## 详细文档

每个组件都有详细的文档说明，包括功能点、工作流程、使用示例等：

- [配置管理文档](docs/Config.md) - 配置文件说明
- [本地IP池文档](docs/LocalIPPool.md) - 本地IP地址池详细说明
- [域名IP监控文档](docs/RemoteDomainIPPool.md) - 域名IP监控详细说明
- [UTLS客户端文档](docs/UTlsClient.md) - UTLS客户端详细说明
- [热连接池文档](docs/UtlsClientHotConnPool.md) - 热连接池详细说明
- [IP访问控制文档](docs/WhiteBlackIPPool.md) - IP黑白名单详细说明

## 快速开始

### 1. 安装依赖

```bash
go mod download
```

### 2. 配置设置

复制并编辑配置文件：

```bash
cp config/config.toml.example config/config.toml
# 编辑 config/config.toml，设置你的配置
```

主要配置项：
- `[DNSDomain]` - 域名监控配置
- `[HotConnPool]` - 热连接池配置
- `[UTlsClient]` - UTLS客户端配置
- `[IPInfo]` - IP信息查询配置（需要ipinfo.io token）

### 3. 运行DNS监控程序

```bash
cd cmd/DNS
go run main.go
```

程序将：
1. 读取配置文件中的DNS服务器列表
2. 对配置的域名进行监控（如 `kh.google.com`, `earth.google.com` 等）
3. 按配置的间隔（默认10分钟）更新数据
4. 将结果保存在 `domain_ips/` 目录中

### 4. 使用热连接池

```go
package main

import (
    "log"
    "utlsProxy/config"
    "utlsProxy/src"
)

func main() {
    // 加载配置
    cfg, err := config.LoadConfig("./config/config.toml")
    if err != nil {
        log.Fatalf("加载配置失败: %v", err)
    }

    // 创建域名监控器
    domainMonitor := src.NewRemoteIPMonitor(...)
    domainMonitor.Start()
    defer domainMonitor.Stop()

    // 创建本地IP池
    localIPv4Pool, _ := src.NewLocalIPPool(
        cfg.HotConnPool.LocalIPv4Addresses,
        "",
    )
    localIPv6Pool, _ := src.NewLocalIPPool(
        []string{},
        cfg.HotConnPool.LocalIPv6SubnetCIDR,
    )

    // 创建热连接池
    poolConfig := src.DomainConnPoolConfig{
        DomainMonitor:   domainMonitor,
        IPAccessControl: src.NewWhiteBlackIPPool(),
        LocalIPv4Pool:   localIPv4Pool,
        LocalIPv6Pool:   localIPv6Pool,
        Fingerprint:     src.GetRandomFingerprint(),
        Domain:          cfg.HotConnPool.Domain,
        Port:            cfg.HotConnPool.Port,
        MaxConns:        cfg.HotConnPool.MaxConns,
        // ... 其他配置
    }
    
    pool, err := src.NewDomainHotConnPool(poolConfig)
    if err != nil {
        log.Fatalf("创建连接池失败: %v", err)
    }
    defer pool.Close()

    // 预热连接池
    if err := pool.Warmup(); err != nil {
        log.Printf("预热失败: %v", err)
    }

    // 使用连接
    conn, err := pool.GetConn()
    if err != nil {
        log.Fatalf("获取连接失败: %v", err)
    }
    // ... 使用连接发送请求 ...
    
    // 归还连接
    pool.ReturnConn(conn, 200)
}
```

## 集成到其他项目

可以通过导入 `utlsProxy/src` 包来使用各个组件：

```go
import "utlsProxy/src"

// 创建本地IP池
ipPool, err := src.NewLocalIPPool(
    []string{"192.168.1.100"}, 
    "2607:8700:5500:2943::/64",
)

// 获取随机TLS指纹
fingerprint := src.GetRandomFingerprint()

// 创建访问控制器
accessControl := src.NewWhiteBlackIPPool()
accessControl.AddIP("192.168.1.100", true) // 添加到白名单

// 创建UTLS客户端
client := src.NewUTlsClient()
client.DialTimeout = 10 * time.Second
client.ReadTimeout = 30 * time.Second
```

## 测试

项目包含完整的单元测试，位于 `test/` 目录中：

- `localip_pool_test.go` - 本地IP池测试
- `remote_domain_ip_pool_test.go` - 远程域名IP池测试
- `utls_client_test.go` - UTLS客户端测试
- `utls_client_hot_conn_pool_test.go` - 热连接池测试
- `utls_fingerprint_test.go` - TLS指纹测试
- `whiteblack_ip_pool_test.go` - IP黑白名单测试
- `integration_test.go` - 集成测试

运行测试：

```bash
# 运行所有测试
go test ./test/... -v

# 运行特定测试
go test ./test/... -run TestLocalIPPool -v
```

## 依赖库

- [uTLS](https://github.com/refraction-networking/utls) - 用于TLS指纹伪装
- [miekg/dns](https://github.com/miekg/dns) - 用于DNS查询
- [BurntSushi/toml](https://github.com/BurntSushi/toml) - 用于TOML解析
- [yaml.v3](https://gopkg.in/yaml.v3) - 用于YAML解析
- [golang.org/x/net/http2](https://pkg.go.dev/golang.org/x/net/http2) - HTTP/2协议支持

## 架构设计

### 组件关系图

```
┌─────────────────────────────────────────────────────────┐
│                    utlsProxy 系统                        │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────┐      ┌──────────────┐                │
│  │ DomainMonitor│ ───→ │ HotConnPool  │                │
│  │ (IP监控)      │      │ (热连接池)    │                │
│  └──────────────┘      └──────┬───────┘                │
│                                │                         │
│                                ↓                         │
│                        ┌──────────────┐                 │
│                        │  UTlsClient  │                 │
│                        │ (HTTP客户端) │                 │
│                        └──────┬───────┘                 │
│                                │                         │
│        ┌───────────────────────┼───────────────────┐   │
│        │                       │                   │   │
│        ↓                       ↓                   ↓   │
│  ┌──────────┐         ┌──────────┐         ┌──────────┐│
│  │LocalIPPool│         │IPAccess  │         │Fingerprint││
│  │(本地IP池) │         │Controller│         │(指纹库)   ││
│  └──────────┘         └──────────┘         └──────────┘│
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### 工作流程

1. **域名监控**：`RemoteDomainIPPool` 定期监控域名IP变化
2. **IP收集**：收集到的IP存储在 `domain_ips/` 目录
3. **热连接池**：`UtlsClientHotConnPool` 从监控器获取IP，预热连接
4. **连接管理**：根据HTTP状态码自动更新黑白名单
5. **连接复用**：优先使用池中的连接，提高性能

## 许可证

本项目采用 MIT 许可证，详情请见 [LICENSE](LICENSE) 文件。

## 贡献

欢迎提交 Issue 和 Pull Request！

## 版本管理

项目使用自动版本管理系统，每次提交到 `main` 分支时会自动增加小版本号并创建GitHub Release。

### 自动发布

推送到 `main` 分支时，GitHub Actions会自动：
1. 检测当前版本号
2. 增加小版本号（patch: v1.0.0 → v1.0.1）
3. 更新VERSION文件和config.toml
4. 创建Git标签
5. 创建GitHub Release

**跳过自动发布**：在提交信息中添加 `[skip release]`

### 手动发布

```bash
# 一键提交并发布
./scripts/commit_and_release.sh "你的提交信息"

# 或分步执行
./scripts/bump_version.sh patch    # 增加版本号
git add VERSION config/config.toml
git commit -m "Bump version"
git push origin main
```

详细说明请查看 [版本管理文档](docs/版本管理.md)

## 相关链接

- [详细文档目录](docs/)
- [配置说明](docs/Config.md)
- [版本管理说明](docs/版本管理.md)
- [API文档](docs/)
