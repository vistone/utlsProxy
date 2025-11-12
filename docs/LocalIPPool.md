# 智能IP地址池 (IPPool)

本文档详细介绍了 `IPPool` 接口及其默认实现 `LocalIPPool` 的设计理念、核心功能和使用方法。这是一个为高性能网络应用设计的、能够自动适应运行环境的智能IP地址池。

## 设计理念

在现代网络应用中，尤其是在云服务和VPS环境中，我们经常面临两种情况：
1.  服务商提供了一个或多个固定的IPv4地址。
2.  服务商额外提供了一个完整的IPv6子网（例如 `/64`），允许我们使用其中海量的地址。

`LocalIPPool` 的核心设计目标就是为了优雅地处理这种混合环境，并最大化地利用可用的IP资源，同时通过 `IPPool` 接口保持代码的简洁、可测试和可扩展性。

## 核心功能

- **接口驱动设计**：模块提供了 `IPPool` 接口，将IP池的行为（“能做什么”）与具体实现（“如何做”）分离。业务逻辑应始终依赖于 `IPPool` 接口，这使得未来可以轻松替换IP池的实现（例如，从本地生成切换为从远程服务获取），而无需修改任何业务代码。

- **环境自适应**：这是`LocalIPPool`最关键的特性。在初始化时，它会**自动检测**当前系统的网络配置，判断您提供的IPv6子网是否真实可用。
    - 如果可用，它将进入**“IPv4+动态IPv6”**模式，在后台持续生成新的IPv6地址。
    - 如果不可用（例如，代码部署在只有IPv4的服务器或您的本地开发机上），它将自动、静默地**降级为“仅IPv4”**模式。

- **动态IPv6生成**：在支持IPv6的环境下，它能从您提供的子网（无论是公网还是私有网络）中，为每一次IP获取请求提供一个**全新的、随机的IPv6地址**。这对于需要高隐蔽性或大量出站连接的应用场景（如爬虫、API请求）极为有用。

- **统一的调用接口**：无论底层工作在何种模式，使用者都只需要调用 `IPPool` 接口中定义的 `GetIP()` 方法来获取地址。

- **并发安全与高性能**：
    - 内部使用`chan`作为生产者-消费者队列来传递生成的IPv6地址，这是Go语言中最高效、最原生的并发模型。
    - 后台goroutine会预先生成一批IP地址放入缓冲区，确保消费者能无延迟地快速获取。
    - 实现了标准的 `io.Closer` 接口，可以通过 `defer pool.Close()` 来优雅地停止后台任务，防止goroutine泄漏。

## 使用方法

### 1. 导入

```go
import "path/to/your/src"
```

### 2. 初始化IP池

**推荐方式：使用统一配置文件**

项目现在支持通过 `config/config.toml` 统一管理本地IP池配置：

```go
import (
    "log"
    "utlsProxy/config"
    "utlsProxy/src"
)

// 加载配置
cfg, err := config.LoadConfig("./config/config.toml")
if err != nil {
    log.Fatalf("加载配置失败: %v", err)
}

// 创建IP池（使用配置文件中的值）
var ipPool src.IPPool // 关键：依赖于接口，而非具体实现
ipPool, err = src.NewLocalIPPool(
    cfg.HotConnPool.LocalIPv4Addresses,  // IPv4地址列表
    cfg.HotConnPool.LocalIPv6SubnetCIDR, // IPv6子网CIDR
)
if err != nil {
    log.Fatalf("无法初始化IP池: %v", err)
}

// IPv6队列大小由配置决定（默认100）
// 如果需要自定义，可以在创建LocalIPPool后调整
```

**传统方式：手动指定参数**

```go
var ipPool src.IPPool // 关键：依赖于接口，而非具体实现
var err error

// 场景A：在拥有公网IPv6子网的VPS上
ipv4s := []string{"172.93.47.57"}
ipv6Subnet := "2607:8700:5500:2943::/64"
ipPool, err = src.NewLocalIPPool(ipv4s, ipv6Subnet)
if err != nil {
    log.Fatalf("无法初始化IP池: %v", err)
}
// 程序将输出: "检测到可用的IPv6子网，已启用IPv6动态生成模式。"

// 场景B：在只有IPv4的服务器或本地开发机上运行同样的代码
// ipPool, err = src.NewLocalIPPool(ipv4s, ipv6Subnet)
// 程序将输出: "未在当前网络环境中检测到指定的IPv6子网，已降级为仅IPv4模式。"
```

**配置说明**

在 `config/config.toml` 中的 `[HotConnPool]` 段包含本地IP池配置：

```toml
[HotConnPool]
# 本地IP池配置
LocalIPv4Addresses = ["192.168.1.100", "192.168.1.101"]  # IPv4地址列表（备用）
LocalIPv6SubnetCIDR = "2607:8700:5500:2943::/64"         # IPv6子网CIDR（优先）
IPv6QueueSize = 100                                       # IPv6地址队列缓冲区大小
```

详细配置说明请参考 [配置管理文档](./Config.md)。

### 3. 获取IP地址

使用 `GetIP()` 方法从池中获取一个IP地址。

```go
// 在支持IPv6的模式下:
// 每次调用都会返回一个全新的、随机的IPv6地址。
// 例如: 2607:8700:5500:2943:abcd:1234:efff:5678
ip := ipPool.GetIP()
fmt.Printf("获取到的IP: %s\n", ip.String())

// 在仅IPv4的模式下:
// 每次调用都会从您提供的列表中随机返回一个IPv4地址。
// 例如: 172.93.47.57
ip := ipPool.GetIP()
fmt.Printf("获取到的IP: %s\n", ip.String())
```

### 4. 关闭IP池

在您的应用程序准备退出时，调用 `Close()` 方法来确保后台的goroutine被干净地关闭。这通常在 `main` 函数的末尾通过 `defer` 来完成。

```go
func main() {
    // ... 初始化 ipPool ...
    defer ipPool.Close()

    // ... 您的应用主逻辑 ...
}
```

## 核心功能点

### 1. 接口驱动设计
- **IPPool 接口**：定义 `GetIP()` 和 `Close()` 方法
- **实现解耦**：业务逻辑依赖于接口，而非具体实现
- **易于扩展**：可以轻松替换实现，无需修改业务代码

### 2. 环境自适应
- **自动检测**：初始化时自动检测IPv6子网是否可用
- **智能降级**：IPv6不可用时自动降级为仅IPv4模式
- **零配置**：无需手动配置，自动适应运行环境

### 3. 动态IPv6生成
- **随机生成**：每次获取都是全新的随机IPv6地址
- **子网约束**：生成的IP地址在指定子网范围内
- **加密安全**：使用 `crypto/rand` 生成随机数

### 4. 高性能并发
- **生产者-消费者模式**：使用channel实现高效队列
- **预生成缓冲**：后台goroutine预先生成IP地址
- **无锁获取**：IPv6模式下从channel获取，性能极高

## 工作流程

### 1. 初始化流程

```
调用 NewLocalIPPool(staticIPv4s, ipv6SubnetCIDR)
    ↓
初始化基础结构
    ├─ rand = 随机数生成器
    ├─ stopChan = 停止信号通道
    └─ staticIPv4s = []  // 解析IPv4地址列表
    ↓
解析IPv4地址
    └─ 验证并添加到 staticIPv4s
    ↓
检查IPv6子网CIDR
    ├─ 为空 → 跳过IPv6初始化
    └─ 不为空 → 继续
    ↓
解析IPv6子网CIDR
    ├─ 解析失败 → 返回错误
    └─ 解析成功 → 继续
    ↓
检查子网是否已配置 (isSubnetConfigured)
    ├─ 已配置 → 启用IPv6模式
    │   ├─ hasIPv6Support = true
    │   ├─ ipv6Subnet = 子网信息
    │   ├─ ipv6Queue = make(chan net.IP, 100)
    │   └─ 启动后台生产者 goroutine
    │
    └─ 未配置 → 降级为仅IPv4模式
        └─ hasIPv6Support = false
    ↓
验证至少有一个可用IP
    ├─ 无IPv6且无IPv4 → 返回错误
    └─ 有可用IP → 返回IPPool接口实例
```

### 2. IPv6子网检测流程 (isSubnetConfigured)

```
遍历系统网络接口
    ↓
过滤接口
    ├─ 状态为Down → 跳过
    ├─ 回环接口 → 跳过
    └─ 活动且非回环 → 继续
    ↓
获取接口地址列表
    ↓
遍历地址
    ├─ 非IPv6地址 → 跳过
    └─ IPv6地址 → 检查
    ↓
检查地址是否在目标子网内
    ├─ 在子网内 → 返回 true (已配置)
    └─ 不在子网内 → 继续
    ↓
所有接口检查完毕
    └─ 返回 false (未配置)
```

### 3. 获取IP地址流程 (GetIP)

#### IPv6模式

```
调用 GetIP()
    ↓
检查 hasIPv6Support
    ├─ false → 跳转到IPv4模式
    └─ true → 继续
    ↓
从 ipv6Queue 获取IP
    ├─ 队列有IP → 立即返回
    └─ 队列为空 → 阻塞等待生产者生成
    ↓
返回IPv6地址
```

#### IPv4模式

```
调用 GetIP()
    ↓
检查 hasIPv6Support
    ├─ true → 跳转到IPv6模式
    └─ false → 继续
    ↓
检查 staticIPv4s 是否为空
    ├─ 为空 → 返回 nil
    └─ 不为空 → 继续
    ↓
随机选择索引
    └─ idx = rand.Intn(len(staticIPv4s))
    ↓
返回 staticIPv4s[idx]
```

### 4. IPv6地址生成流程 (generateRandomIPInSubnet)

```
复制子网前缀
    └─ prefix = copy(ipv6Subnet.IP)
    ↓
计算主机位数
    ├─ ones, total = subnet.Mask.Size()
    └─ hostBits = total - ones
    ↓
生成随机数
    ├─ 使用 crypto/rand 生成随机大整数
    ├─ 失败 → 回退到 math/rand
    └─ randInt = 随机数 (0 到 2^hostBits-1)
    ↓
转换为字节数组
    └─ randBytes = randInt.Bytes()
    ↓
填充到IP地址主机部分
    └─ 从后向前填充字节
    ↓
返回生成的IPv6地址
```

### 5. 后台生产者流程 (producer)

```
启动后台goroutine
    ↓
无限循环
    ↓
检查停止信号
    ├─ 收到停止信号 → 退出
    └─ 未收到 → 继续
    ↓
生成随机IPv6地址
    └─ ip = generateRandomIPInSubnet()
    ↓
放入队列
    ├─ 队列未满 → 立即放入
    └─ 队列已满 → 阻塞等待
    ↓
继续循环
```

### 6. 关闭流程 (Close)

```
调用 Close()
    ↓
检查 hasIPv6Support
    ├─ false → 直接返回 (无需清理)
    └─ true → 继续
    ↓
检查 stopChan 是否已关闭
    ├─ 已关闭 → 直接返回
    └─ 未关闭 → 继续
    ↓
关闭 stopChan
    └─ close(stopChan)
    ↓
后台生产者收到停止信号
    └─ producer() 退出
    ↓
返回 nil (成功)
```

## 设计细节：如何实现环境自适应？

`NewLocalIPPool` 在内部调用了一个名为 `isSubnetConfigured` 的辅助函数。该函数会：
1. 遍历当前操作系统上所有**处于活动状态 (UP)** 且**非回环 (non-loopback)** 的网络接口。
2. 获取每个接口上配置的所有IP地址。
3. 检查是否有任何一个已配置的IP地址，位于您在初始化时提供的IPv6大子网（例如`/64`）之内。

只要找到一个匹配项，就证明当前环境确实配置了该IPv6子网，可以安全地启用动态生成功能。这种设计确保了`LocalIPPool`不会在错误的环境中尝试生成和使用无法路由的IP地址。

## 状态转换图

```
IP池状态：
    ┌─────────────┐
    │   初始化     │
    └──────┬──────┘
           │
           │ NewLocalIPPool()
           ↓
    ┌─────────────┐
    │  检测环境    │
    └──────┬──────┘
           │
           ├─ IPv6可用 ────→ ┌──────────────┐
           │                 │ IPv6+IPv4模式 │
           │                 │ (动态生成IPv6)│
           │                 └──────┬───────┘
           │                        │
           │                        │ GetIP()
           │                        ↓
           │                 ┌──────────────┐
           │                 │ 返回随机IPv6  │
           │                 └──────────────┘
           │
           └─ IPv6不可用 ────→ ┌──────────────┐
                              │  仅IPv4模式   │
                              │ (随机选择IPv4)│
                              └──────┬───────┘
                                     │
                                     │ GetIP()
                                     ↓
                              ┌──────────────┐
                              │ 返回随机IPv4 │
                              └──────────────┘
```

## 性能特点

1. **O(1) 获取**：IPv6模式下从channel获取，时间复杂度O(1)
2. **预生成缓冲**：后台预先生成100个IP地址，减少等待时间
3. **无锁设计**：IPv6获取使用channel，无需加锁
4. **并发安全**：所有操作都是线程安全的

## 注意事项

1. **接口依赖**：业务代码应依赖于 `IPPool` 接口，而非 `LocalIPPool` 具体实现
2. **资源清理**：使用完毕后应调用 `Close()` 关闭IP池，防止goroutine泄漏
3. **IPv6队列大小**：默认100，可根据需求调整
4. **环境检测**：IPv6子网检测基于系统网络配置，确保子网已正确配置
5. **随机性**：IPv6地址使用加密安全的随机数生成，保证随机性
