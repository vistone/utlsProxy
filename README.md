# utlsProxy - 基于uTLS的智能代理系统

utlsProxy 是一个基于 uTLS 库的智能代理系统，能够模拟各种浏览器的 TLS 指纹，实现防指纹识别的网络请求。该项目包含多个核心组件，用于管理 IP 地址池、域名监控、访问控制和 TLS 指纹模拟。

## 项目结构

```
utlsProxy/
├── cmd/
│   └── DNS/           # DNS 监控主程序
├── docs/              # 文档目录
├── domain_ips/        # 域名 IP 地址存储目录
├── src/               # 核心源代码
├── test/              # 单元测试代码
├── go.mod             # Go 模块定义
└── go.sum             # Go 模块校验和
```

## 核心组件

### 1. LocalIPPool - 本地 IP 地址池

[LocalIPPool.go](src/LocalIPPool.go) 实现了一个智能 IP 地址池，支持 IPv4 和 IPv6 地址管理：

- 支持静态 IPv4 地址列表配置
- 自动检测系统网络环境，支持 IPv6 子网动态生成
- 在支持 IPv6 的环境中，可以动态生成海量 IPv6 地址
- 线程安全的设计，支持并发访问
- 实现了 [IPPool](src/LocalIPPool.go#L17-L21) 接口

主要方法：
- `NewLocalIPPool(staticIPv4s []string, ipv6SubnetCIDR string) (IPPool, error)` - 创建 IP 池实例
- `GetIP() net.IP` - 从池中获取一个可用的 IP 地址
- `Close() error` - 优雅关闭 IP 池

### 2. RemoteDomainIPPool - 远程域名 IP 监控池

[RemoteDomainIPPool.go](src/RemoteDomainIPPool.go) 实现了域名 IP 地址监控功能：

- 定期监控指定域名的 IP 地址变化
- 支持多个 DNS 服务器并发查询，获取最全面的 IP 列表
- 使用 ipinfo.io API 获取 IP 地址详细信息
- 支持多种数据存储格式（JSON, YAML, TOML）
- 线程安全的设计，支持并发访问
- 实现了 [DomainMonitor](src/RemoteDomainIPPool.go#L17-L27) 接口

主要方法：
- `NewRemoteIPMonitor(config MonitorConfig) (DomainMonitor, error)` - 创建监控实例
- `Start()` - 启动监控
- `Stop()` - 停止监控
- `GetDomainPool(domain string) (map[string][]IPRecord, bool)` - 获取域名的 IP 池数据

### 3. UTlsFingerPrint - TLS 指纹库

[UTlsFingerPrint.go](src/UTlsFingerPrint.go) 实现了浏览器 TLS 指纹模拟功能：

- 包含多种浏览器（Chrome, Firefox, Safari, Edge）的 TLS 指纹配置
- 支持不同平台（Windows, macOS, Linux, iOS）的指纹模拟
- 提供随机指纹选择功能，增强防指纹识别能力
- 支持根据浏览器类型或平台筛选指纹配置
- 实现了基于 [uTLS](https://github.com/refraction-networking/utls) 库的 TLS 指纹伪装

主要方法：
- `GetRandomFingerprint() Profile` - 获取随机指纹配置
- `NewLibrary() *Library` - 创建指纹库实例
- `RandomProfile() Profile` - 随机返回一个配置文件
- `ProfileByName(name string) (*Profile, error)` - 根据名称查找配置文件
- `ProfilesByBrowser(browser string) []Profile` - 根据浏览器类型筛选配置文件

### 4. WhiteBlackIPPool - IP 黑白名单访问控制器

[WhiteBlackIPPool.go](src/WhiteBlackIPPool.go) 实现了基于内存的 IP 访问控制：

- 支持 IP 地址的黑白名单管理
- 实现"黑名单优先"和"默认拒绝"的安全策略
- 线程安全的设计，支持并发访问
- 实现了 [IPAccessController](src/WhiteBlackIPPool.go#L7-L17) 接口

主要方法：
- `NewWhiteBlackIPPool() IPAccessController` - 创建访问控制器实例
- `AddIP(ip string, isWhite bool)` - 将 IP 添加到指定名单
- `RemoveIP(ip string, isWhite bool)` - 从指定名单删除 IP
- `IsIPAllowed(ip string) bool` - 检查 IP 是否被允许访问
- `GetAllowedIPs() []string` - 获取白名单中的所有 IP
- `GetBlockedIPs() []string` - 获取黑名单中的所有 IP

## 主程序

### DNS 监控程序

[cmd/DNS/main.go](cmd/DNS/main.go) 是项目的主程序，实现了域名 IP 监控功能：

1. 从配置文件加载 DNS 服务器列表
2. 对指定域名进行定期监控
3. 将获取的 IP 信息保存到文件中
4. 支持优雅关闭

## 使用方法

### 运行 DNS 监控程序

```bash
cd cmd/DNS
go run main.go
```

程序将：
1. 读取 [src/DNSServerNames.json](src/DNSServerNames.json) 文件中的 DNS 服务器配置
2. 对 `kh.google.com`, `earth.google.com`, `khmdb.google.com` 等域名进行监控
3. 每 10 分钟更新一次数据
4. 将结果保存在 [domain_ips](domain_ips/) 目录中

### 集成到其他项目

可以通过导入 `utlsProxy/src` 包来使用各个组件：

```go
import "utlsProxy/src"

// 创建本地 IP 池
ipPool, err := src.NewLocalIPPool([]string{"1.1.1.1", "8.8.8.8"}, "2607:8700:5500:2943::/64")

// 获取随机 TLS 指纹
fingerprint := src.GetRandomFingerprint()

// 创建访问控制器
accessControl := src.NewWhiteBlackIPPool()
accessControl.AddIP("192.168.1.100", true) // 添加到白名单
```

## 测试

项目包含完整的单元测试，位于 [test](test/) 目录中：

- [localip_pool_test.go](test/localip_pool_test.go) - 本地 IP 池测试
- [remote_domain_ip_pool_test.go](test/remote_domain_ip_pool_test.go) - 远程域名 IP 池测试
- [utls_fingerprint_test.go](test/utls_fingerprint_test.go) - TLS 指纹测试
- [whiteblack_ip_pool_test.go](test/whiteblack_ip_pool_test.go) - IP 黑白名单测试
- [main_test.go](test/main_test.go) - 主程序相关测试
- [integration_test.go](test/integration_test.go) - 集成测试

运行测试：
```bash
go test ./test/... -v
```

## 依赖库

- [uTLS](https://github.com/refraction-networking/utls) - 用于 TLS 指纹伪装
- [miekg/dns](https://github.com/miekg/dns) - 用于 DNS 查询
- [BurntSushi/toml](https://github.com/BurntSushi/toml) - 用于 TOML 解析
- [yaml.v3](https://gopkg.in/yaml.v3) - 用于 YAML 解析

## 许可证

本项目采用 MIT 许可证，详情请见 [LICENSE](LICENSE) 文件。