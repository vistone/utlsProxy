# 配置管理 (Config)

本文档详细介绍了项目的统一配置管理系统。所有配置项都集中在 `config/config.toml` 文件中，通过 `config` 包进行统一加载和管理。

## 配置文件结构

项目使用 TOML 格式的配置文件，位于 `config/config.toml`。配置文件包含以下主要配置段：

```
[ServerConfig]          # 服务器配置
[DNSDomain]             # DNS域名配置
[PoolConfig]            # 连接池配置
[IPInfo]                # IP信息配置
[UTlsClient]            # UTLS客户端配置
[HotConnPool]           # 热连接池配置
[RockTreeDataConfig]    # RockTree数据配置
[EarthImageryDataConfig] # Earth影像数据配置
```

## 配置加载

### 基本用法

```go
package main

import (
    "log"
    "utlsProxy/config"
)

func main() {
    // 加载配置文件
    cfg, err := config.LoadConfig("./config/config.toml")
    if err != nil {
        log.Fatalf("加载配置失败: %v", err)
    }
    
    // 使用配置
    serverPort := cfg.ServerConfig.ServerPort
    domains := cfg.DNSDomain.HostName
    
    // ...
}
```

### 配置验证和默认值

`LoadConfig` 函数会自动：
- 验证配置文件是否存在
- 解析 TOML 格式
- 设置合理的默认值
- 验证配置项的有效性

## 配置段详解

### ServerConfig - 服务器配置

```toml
[ServerConfig]
Version="v1.0.0"
ServerPort=9091
```

**字段说明**：
- `Version`: 服务器版本号
- `ServerPort`: 服务器监听端口

### DNSDomain - DNS域名配置

```toml
[DNSDomain]
HostName=["kh.google.com","earth.google.com","khmdb.google.com"]
StorageDir="./domain_ips"
StorageFormat="json"
UpdateIntervalMinutes=10
DNSServerFilePath="./src/DNSServerNames.json"
DefaultDNSServers=["8.8.8.8", "1.1.1.1"]
DNSQueryTimeoutSeconds=5
DNSMaxWorkers=50
HTTPClientTimeoutSeconds=10
HTTPMaxIdleConns=100
HTTPMaxIdleConnsPerHost=10
HTTPIdleConnTimeoutSeconds=90
```

**字段说明**：
- `HostName`: 要监控的域名列表
- `StorageDir`: IP数据存储目录
- `StorageFormat`: 存储格式（json/yaml/toml）
- `UpdateIntervalMinutes`: 域名IP更新间隔（分钟）
- `DNSServerFilePath`: DNS服务器配置文件路径
- `DefaultDNSServers`: 默认DNS服务器列表（当配置文件不存在时使用）
- `DNSQueryTimeoutSeconds`: DNS查询超时时间（秒）
- `DNSMaxWorkers`: DNS并发查询工作线程数
- `HTTPClientTimeoutSeconds`: HTTP客户端超时时间（秒）
- `HTTPMaxIdleConns`: HTTP最大空闲连接数
- `HTTPMaxIdleConnsPerHost`: 每个主机最大空闲连接数
- `HTTPIdleConnTimeoutSeconds`: HTTP空闲连接超时时间（秒）

**辅助方法**：
```go
updateInterval := cfg.DNSDomain.GetUpdateInterval()        // time.Duration
dnsTimeout := cfg.DNSDomain.GetDNSQueryTimeout()          // time.Duration
httpTimeout := cfg.DNSDomain.GetHTTPClientTimeout()      // time.Duration
idleTimeout := cfg.DNSDomain.GetHTTPIdleConnTimeout()    // time.Duration
```

### PoolConfig - 连接池配置

```toml
[PoolConfig]
ProxyAddress = ""
Concurrency = 50
RehabilitationIntervalMinutes = 20
IdleTimeoutMinutes = 10
```

**字段说明**：
- `ProxyAddress`: 代理地址（可选）
- `Concurrency`: 并发数
- `RehabilitationIntervalMinutes`: 恢复间隔（分钟）
- `IdleTimeoutMinutes`: 空闲超时（分钟）

**辅助方法**：
```go
rehabInterval := cfg.PoolConfig.GetRehabilitationInterval() // time.Duration
idleTimeout := cfg.PoolConfig.GetIdleTimeout()             // time.Duration
```

### IPInfo - IP信息配置

```toml
[IPInfo]
Token="f6babc99a5ec26"
```

**字段说明**：
- `Token`: ipinfo.io API Token，用于查询IP地址的详细信息

### UTlsClient - UTLS客户端配置

```toml
[UTlsClient]
ReadTimeoutSeconds=30
DialTimeoutSeconds=10
MaxRetries=0
```

**字段说明**：
- `ReadTimeoutSeconds`: 读取超时时间（秒），默认30秒
- `DialTimeoutSeconds`: 连接超时时间（秒），默认10秒
- `MaxRetries`: 最大重试次数，0表示不重试

**辅助方法**：
```go
readTimeout := cfg.UTlsClient.GetReadTimeout()  // time.Duration
dialTimeout := cfg.UTlsClient.GetDialTimeout()  // time.Duration
```

### HotConnPool - 热连接池配置

```toml
[HotConnPool]
# 本地IP池配置（智能自动检测模式）
# 如果留空，系统会自动检测并使用可用的网络接口IP地址
# 只有在需要指定特定IP时才需要配置这些选项
LocalIPv4Addresses = [] # 本地IPv4地址列表（留空则自动检测公网IPv4）
LocalIPv6SubnetCIDR = "" # 本地IPv6子网CIDR（留空则自动检测，优先使用/64子网，支持隧道模式）
IPv6QueueSize = 100

# 连接池基础配置
Domain = "kh.google.com"
Port = "443"
MaxConns = 100
IdleTimeoutMinutes = 5

# 预热配置
WarmupMethod = "GET"
WarmupConcurrency = 10

# 定时任务配置
BlacklistTestIntervalMinutes = 5
IPRefreshIntervalMinutes = 10

# TLS指纹配置
FingerprintName = ""
```

**字段说明**：
- `LocalIPv4Addresses`: 本地IPv4地址列表。如果为空数组 `[]`，系统会自动检测所有可用的**公网IPv4地址**（自动过滤私有地址RFC 1918）
- `LocalIPv6SubnetCIDR`: 本地IPv6子网CIDR。如果为空字符串 `""`，系统会自动检测可用的**公网IPv6子网**（优先使用/64子网）。如果未检测到公网IPv6子网但系统支持IPv6路由（如通过隧道），会自动启用**IPv6隧道模式**（不绑定本地IP）
- `IPv6QueueSize`: IPv6地址队列缓冲区大小，默认100
- `Domain`: 目标域名
- `Port`: 目标端口，默认443
- `MaxConns`: 最大连接数，默认100
- `IdleTimeoutMinutes`: 连接空闲超时时间（分钟），默认5分钟
- `WarmupMethod`: 预热请求方法，默认GET
- `WarmupConcurrency`: 预热并发数，默认10
- `BlacklistTestIntervalMinutes`: 黑名单IP测试间隔（分钟），默认5分钟
- `IPRefreshIntervalMinutes`: IP列表刷新间隔（分钟），默认10分钟
- `FingerprintName`: TLS指纹名称，留空则随机选择

**自动检测模式**：
- 当 `LocalIPv4Addresses = []` 时，系统会自动扫描所有网络接口，检测并过滤出公网IPv4地址（排除10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16等私有地址）
- 当 `LocalIPv6SubnetCIDR = ""` 时，系统会自动检测公网IPv6子网（优先/64子网，排除ULA和Link-local地址）
- 如果未检测到公网IPv6子网但系统支持IPv6路由（如通过隧道），会自动启用IPv6隧道模式，此时不绑定本地IP，由系统自动选择路由

**手动指定模式**：
- 如果需要使用特定的IPv4地址，可以在 `LocalIPv4Addresses` 中指定，例如：`["192.168.1.100", "192.168.1.101"]`
- 如果需要使用特定的IPv6子网，可以在 `LocalIPv6SubnetCIDR` 中指定，例如：`"2607:8700:5500:2943::/64"`

**特殊说明**：
- `WarmupPath` 和 `WarmupHeaders` 会自动使用 `RockTreeDataConfig` 中的配置
- 如果 `HotConnPool.WarmupPath` 为空，则使用 `RockTreeDataConfig.CheckStatusPath`
- 预热请求头使用 `RockTreeDataConfig.RocktreeRquestHeader`

**辅助方法**：
```go
idleTimeout := cfg.HotConnPool.GetIdleTimeout()                    // time.Duration
blacklistInterval := cfg.HotConnPool.GetBlacklistTestInterval()  // time.Duration
ipRefreshInterval := cfg.HotConnPool.GetIPRefreshInterval()      // time.Duration
warmupPath := cfg.GetWarmupPath()                                // string
warmupHeaders := cfg.GetWarmupHeaders()                          // map[string]string
```

### RockTreeDataConfig - RockTree数据配置

```toml
[RockTreeDataConfig]
HostName="kh.google.com"
CheckStatusPath="/rt/earth/PlanetoidMetadata"
BulkMetadataPath="/rt/earth/BulkMetadata/pb=!1m2!1s%s!2u%d"
NodeDataPath="/rt/earth/NodeData/pb=!1m2!1s%s!2u%d!2e6!4b0"
ImageryDataPath="/rt/earth/NodeData/pb=!1m2!1s%s!2u%d!2e1!3u%d!4b0"
RocktreeRquestHeader=[
    "Accept-Encoding: gzip, deflate, br, zstd",
    "Accept: */*",
    "Host:kh.google.com",
    "Origin:https://earth.google.com",
    "Referer:https://earth.google.com/",
    "Sec-Fetch-Dest:empty",
    "Sec-Fetch-Mode:cors",
    "Sec-Fetch-Site:same-site",
    "TE:trailers"
]
```

**字段说明**：
- `HostName`: 主机名
- `CheckStatusPath`: 检查状态路径（也是热连接池的预热路径）
- `BulkMetadataPath`: 批量元数据路径模板
- `NodeDataPath`: 节点数据路径模板
- `ImageryDataPath`: 影像数据路径模板
- `RocktreeRquestHeader`: 请求头列表（格式："Key: Value"）

### EarthImageryDataConfig - Earth影像数据配置

```toml
[EarthImageryDataConfig]
HostName="kh.google.com"
CheckStatusPath="/geauth"
dbrootPath="/dbRoot.v5"
q2path="/flatfile?q2-%s-q.%d"
imageryPath="/flatfile?f1-%s-i.%d"
requestHeader=[
    "Accept-Encoding: gzip, deflate, br, zstd",
    "Accept: */*",
    "Host:kh.google.com",
    "Origin:https://kh.google.com",
    "Referer:https://kh.google.com/",
    "Sec-Fetch-Dest:empty",
    "Sec-Fetch-Mode:cors",
    "Sec-Fetch-Site:same-site",
    "TE:trailers"
]
```

**字段说明**：
- `HostName`: 主机名
- `CheckStatusPath`: 检查状态路径（认证入口）
- `dbrootPath`: dbroot路径
- `q2path`: q2路径模板
- `imageryPath`: 影像路径模板
- `requestHeader`: 请求头列表

## 完整使用示例

### 示例1：使用配置初始化DomainMonitor

```go
package main

import (
    "encoding/json"
    "log"
    "os"
    "utlsProxy/config"
    "utlsProxy/src"
)

func main() {
    // 1. 加载配置
    cfg, err := config.LoadConfig("./config/config.toml")
    if err != nil {
        log.Fatalf("加载配置失败: %v", err)
    }
    
    // 2. 从配置文件加载DNS服务器列表
    dnsData, err := os.ReadFile(cfg.DNSDomain.DNSServerFilePath)
    if err != nil {
        log.Printf("无法读取DNS服务器文件，使用默认DNS服务器")
        // 使用默认DNS服务器
    }
    
    var dnsDB struct {
        Servers map[string]string `json:"servers"`
    }
    if err := json.Unmarshal(dnsData, &dnsDB); err == nil {
        // 提取并去重DNS服务器IP
        uniqueServers := make(map[string]bool)
        var dnsServers []string
        for _, ip := range dnsDB.Servers {
            if !uniqueServers[ip] {
                uniqueServers[ip] = true
                dnsServers = append(dnsServers, ip)
            }
        }
        cfg.DNSDomain.DefaultDNSServers = dnsServers
    }
    
    // 3. 创建MonitorConfig
    monitorConfig := src.MonitorConfig{
        Domains:        cfg.DNSDomain.HostName,
        DNSServers:     cfg.DNSDomain.DefaultDNSServers,
        IPInfoToken:    cfg.IPInfo.Token,
        UpdateInterval: cfg.DNSDomain.GetUpdateInterval(),
        StorageDir:     cfg.DNSDomain.StorageDir,
        StorageFormat:  cfg.DNSDomain.StorageFormat,
    }
    
    // 4. 初始化监视器
    monitor, err := src.NewRemoteIPMonitor(monitorConfig)
    if err != nil {
        log.Fatalf("无法创建监视器: %v", err)
    }
    
    monitor.Start()
    defer monitor.Stop()
    
    // ...
}
```

### 示例2：使用配置初始化UTlsClient

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
    
    // 创建UTlsClient并应用配置
    client := src.NewUTlsClient()
    client.ReadTimeout = cfg.UTlsClient.GetReadTimeout()
    client.DialTimeout = cfg.UTlsClient.GetDialTimeout()
    client.MaxRetries = cfg.UTlsClient.MaxRetries
    
    // 使用客户端
    // ...
}
```

### 示例3：使用配置初始化热连接池

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
    
    // 创建本地IP池
    localIPv4Pool, _ := src.NewLocalIPPool(
        cfg.HotConnPool.LocalIPv4Addresses,
        cfg.HotConnPool.LocalIPv6SubnetCIDR,
    )
    
    // 获取预热路径和请求头
    warmupPath := cfg.GetWarmupPath()
    warmupHeaders := cfg.GetWarmupHeaders()
    
    // 创建热连接池配置
    poolConfig := src.DomainConnPoolConfig{
        DomainMonitor:   monitor,
        IPAccessControl: src.NewWhiteBlackIPPool(),
        LocalIPv4Pool:   localIPv4Pool,
        Domain:         cfg.HotConnPool.Domain,
        Port:           cfg.HotConnPool.Port,
        MaxConns:       cfg.HotConnPool.MaxConns,
        IdleTimeout:    cfg.HotConnPool.GetIdleTimeout(),
        WarmupPath:     warmupPath,
        WarmupMethod:   cfg.HotConnPool.WarmupMethod,
        // ...
    }
    
    // ...
}
```

## 配置最佳实践

1. **统一管理**：所有配置项都应在 `config/config.toml` 中定义，避免硬编码
2. **使用辅助方法**：优先使用配置结构体的辅助方法获取 `time.Duration` 类型
3. **默认值**：配置系统会自动设置合理的默认值，但建议显式配置重要参数
4. **环境区分**：可以通过不同的配置文件来区分开发、测试、生产环境
5. **配置验证**：在程序启动时验证关键配置项的有效性

## 配置项优先级

某些配置项有优先级关系：

1. **预热路径**：
   - `HotConnPool.WarmupPath`（如果配置）
   - `RockTreeDataConfig.CheckStatusPath`（优先）
   - `EarthImageryDataConfig.CheckStatusPath`（备用）

2. **预热请求头**：
   - `RockTreeDataConfig.RocktreeRquestHeader`（优先）
   - `EarthImageryDataConfig.RequestHeader`（备用）

3. **DNS服务器**：
   - 从 `DNSServerFilePath` 文件加载（优先）
   - `DefaultDNSServers`（备用）

## 注意事项

1. 配置文件路径应使用相对路径或绝对路径
2. 时间配置统一使用分钟或秒，通过辅助方法转换为 `time.Duration`
3. 数组配置项（如 `HostName`、`LocalIPv4Addresses`）使用 TOML 数组语法
4. 请求头配置使用字符串数组，格式为 `"Key: Value"`


