# 域名IP监视器 (DomainMonitor)

本文档详细介绍了 `DomainMonitor` 组件的设计理念、核心功能和使用方法。这是一个高度可配置的、健壮的后台服务，用于持续监控指定域名的IP地址，并将结果持久化存储。

## 核心功能点

### 1. 接口驱动设计
- **DomainMonitor 接口**：定义 `Start()`、`Stop()` 和 `GetDomainPool()` 方法
- **实现解耦**：业务逻辑依赖于接口，而非具体实现
- **易于扩展**：可以轻松替换实现，无需修改业务代码

### 2. 并发DNS解析
- **多DNS服务器**：同时向所有配置的DNS服务器发起请求
- **并发工作池**：使用工作池模式，最多50个并发查询
- **去重收集**：收集所有独特的IPv4和IPv6地址

### 3. 增量更新（累加模式）
- **历史加载**：每次更新时加载域名的历史IP数据
- **新IP识别**：只查询新发现的IP地址的详细信息
- **只增不减**：新IP追加到历史数据库，IP池持续增长
- **节省API调用**：避免重复查询已知IP，节省ipinfo.io API调用次数

### 4. 隔离存储
- **按域名隔离**：每个域名生成独立的存储文件
- **多格式支持**：支持JSON、YAML、TOML格式
- **自动创建目录**：自动创建不存在的存储目录

### 5. 健壮的后台服务
- **生命周期管理**：提供 `Start()` 和 `Stop()` 方法
- **周期性更新**：按配置的间隔自动执行更新任务
- **内存缓存**：通过 `GetDomainPool()` 安全读取最新数据
- **HTTP客户端优化**：内置可复用的HTTP客户端，优化连接池

## 工作流程

### 1. 初始化流程

```
调用 NewRemoteIPMonitor(config)
    ↓
创建 remoteIPMonitor 实例
    ├─ config = 配置信息
    ├─ latestData = make(map[string]map[string][]IPRecord)
    ├─ mu = sync.Mutex{}
    └─ httpClient = 创建HTTP客户端（连接池优化）
    ↓
创建存储目录（如果不存在）
    └─ os.MkdirAll(config.StorageDir, 0755)
    ↓
返回 DomainMonitor 接口实例
```

### 2. 启动流程 (Start)

```
调用 Start()
    ↓
检查是否已启动
    ├─ 已启动 → 直接返回
    └─ 未启动 → 继续
    ↓
启动后台goroutine
    ↓
立即执行一次更新
    └─ updateAllDomains()
    ↓
启动定时器
    └─ time.NewTicker(config.UpdateInterval)
    ↓
循环执行
    ├─ 定时触发 → updateAllDomains()
    └─ 收到停止信号 → 退出
```

### 3. 更新所有域名流程 (updateAllDomains)

```
调用 updateAllDomains()
    ↓
记录开始时间
    ↓
遍历所有域名
    ↓
为每个域名启动goroutine
    └─ processSingleDomain(domain)
    ↓
等待所有域名处理完成 (WaitGroup)
    ↓
记录完成时间
    ↓
完成
```

### 4. 处理单个域名流程 (processSingleDomain)

```
调用 processSingleDomain(domain)
    ↓
构建文件路径
    └─ fileName = domain.replace(".", "_") + ".json"
    └─ filePath = StorageDir + fileName
    ↓
加载历史数据
    ├─ 文件存在 → 解析JSON/YAML/TOML
    └─ 文件不存在 → 返回空数据
    ↓
构建已知IP映射
    ├─ 遍历历史IPv4记录
    └─ 遍历历史IPv6记录
    ↓
并发DNS解析 (resolveDomainConcurrently)
    ├─ 查询A记录（IPv4）
    └─ 查询AAAA记录（IPv6）
    ↓
识别新IP
    └─ 过滤出不在已知IP映射中的IP
    ↓
查询新IP信息（如果有新IP）
    ├─ 并发查询每个新IP的详细信息
    ├─ 调用 ipinfo.io API
    └─ 添加到domainPool
    ↓
更新内存缓存
    └─ latestData[domain] = domainPool
    ↓
保存到文件
    └─ saveDomainData(filePath, domainPool)
    ↓
完成
```

### 5. 并发DNS解析流程 (resolveDomainConcurrently)

```
调用 resolveDomainConcurrently(domain)
    ↓
创建同步Map
    ├─ ipv4Map = sync.Map{}
    └─ ipv6Map = sync.Map{}
    ↓
创建DNS服务器通道
    └─ serverChan = make(chan string)
    ↓
启动工作池（最多50个goroutine）
    ↓
每个goroutine循环处理
    ├─ 从serverChan读取DNS服务器
    ├─ 创建DNS客户端
    ├─ 查询A记录（IPv4）
    │   └─ 成功 → 存储到ipv4Map
    └─ 查询AAAA记录（IPv6）
        └─ 成功 → 存储到ipv6Map
    ↓
等待所有goroutine完成
    ↓
收集结果
    ├─ 遍历ipv4Map → ipv4s列表
    └─ 遍历ipv6Map → ipv6s列表
    ↓
返回IPv4列表、IPv6列表和错误
```

### 6. 获取域名IP池流程 (GetDomainPool)

```
调用 GetDomainPool(domain)
    ↓
加锁保护 (mutex.RLock())
    ↓
从内存缓存获取数据
    ├─ 找到 → 深拷贝数据
    └─ 未找到 → 返回 false
    ↓
释放锁 (defer mutex.RUnlock())
    ↓
返回数据快照和是否找到的标志
```

### 7. 停止流程 (Stop)

```
调用 Stop()
    ↓
检查是否已停止
    ├─ 已停止 → 直接返回
    └─ 未停止 → 继续
    ↓
关闭停止通道
    └─ close(stopChan)
    ↓
后台goroutine收到停止信号
    └─ 退出循环
    ↓
完成
```

## 使用方法

### 1. 目录结构与配置文件

建议采用以下目录结构：

```
/your_project
|-- /cmd
|   |-- /your_app
|       |-- main.go
|-- /src
|   |-- RemoteDomainIPPool.go
|   |-- DNSServerNames.json
|-- /domain_ips/  <-- (此目录会自动创建)
    |-- kh_google_com.json
    |-- earth_google_com.json
```

**DNSServerNames.json**:
这个文件用于配置您希望使用的所有DNS服务器。程序会读取这个文件，并并发地向其中所有服务器发起请求。

```json
{
  "dns_servers": {
    "Google-Public": "8.8.8.8",
    "Cloudflare": "1.1.1.1",
    "AliDNS": "223.5.5.5"
  }
}
```

### 2. 初始化与配置

**推荐方式：使用统一配置文件**

项目现在支持通过 `config/config.toml` 统一管理所有配置。推荐使用配置文件方式：

```go
package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"utlsProxy/config"
	"utlsProxy/src"
)

// DNSDatabaseConfig 用于解析DNSServerNames.json
type DNSDatabaseConfig struct {
	Servers map[string]string `json:"servers"`
}

func main() {
	// 1. 加载统一配置文件
	cfg, err := config.LoadConfig("./config/config.toml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 从配置文件加载DNS服务器列表
	var dnsServers []string
	dnsData, err := os.ReadFile(cfg.DNSDomain.DNSServerFilePath)
	if err != nil {
		log.Printf("无法读取DNS服务器文件，使用默认DNS服务器")
		dnsServers = cfg.DNSDomain.DefaultDNSServers
	} else {
		var dnsDB DNSDatabaseConfig
		if err := json.Unmarshal(dnsData, &dnsDB); err != nil {
			log.Printf("解析DNS服务器文件失败，使用默认DNS服务器")
			dnsServers = cfg.DNSDomain.DefaultDNSServers
		} else {
			// 提取并去重DNS服务器IP
			uniqueServers := make(map[string]bool)
			for _, ip := range dnsDB.Servers {
				if !uniqueServers[ip] {
					uniqueServers[ip] = true
					dnsServers = append(dnsServers, ip)
				}
			}
		}
	}
	log.Printf("成功加载 %d 个DNS服务器。\n", len(dnsServers))

	// 3. 创建MonitorConfig（使用配置文件中的值）
	monitorConfig := src.MonitorConfig{
		Domains:        cfg.DNSDomain.HostName,
		DNSServers:     dnsServers,
		IPInfoToken:    cfg.IPInfo.Token,
		UpdateInterval: cfg.DNSDomain.GetUpdateInterval(),
		StorageDir:     cfg.DNSDomain.StorageDir,
		StorageFormat:  cfg.DNSDomain.StorageFormat,
	}

	// 4. 初始化监视器，注意变量类型是接口
	var monitor src.DomainMonitor
	monitor, err = src.NewRemoteIPMonitor(monitorConfig)
	if err != nil {
		log.Fatalf("无法创建监视器: %v", err)
	}

	// ... (见下一节)
}
```

**传统方式：硬编码配置**

如果不想使用配置文件，也可以直接创建配置：

```go
config := src.MonitorConfig{
	Domains:        []string{"kh.google.com", "earth.google.com", "khmdb.google.com"},
	DNSServers:     dnsServers,
	IPInfoToken:    "YOUR_IPINFO_TOKEN",
	UpdateInterval: 10 * time.Minute,
	StorageDir:     "./domain_ips",
	StorageFormat:  "json",
}
```

**配置文件说明**

在 `config/config.toml` 中的 `[DNSDomain]` 段包含以下配置：

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

详细配置说明请参考 [配置管理文档](./Config.md)。

### 3. 启动与停止服务

`DomainMonitor` 是一个长期运行的后台服务，您需要管理它的生命周期，并在程序退出时优雅地关闭它。

```go
// ... (在main函数中继续)

	// 4. 启动监视器 (它将在后台运行)
	monitor.Start()
	
	// 5. 在需要时，从其他goroutine或服务中安全地获取数据
	go func() {
		// 等待一段时间，让第一次更新完成
		time.Sleep(30 * time.Second) 
		
		pool, found := monitor.GetDomainPool("kh.google.com")
		if found {
			log.Printf("从缓存中成功获取 'kh.google.com' 的IP池，包含 %d 个IPv4地址。", len(pool["ipv4"]))
		}
	}()

	// 6. 优雅地处理程序退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // 阻塞主goroutine，直到收到退出信号
	log.Println("收到退出信号，准备关闭...")

	// 7. 停止监视器
	monitor.Stop()
	log.Println("程序已优雅退出。")
}
```

### 4. 输出结果

程序运行后，会在您指定的 `StorageDir`（例如 `./domain_ips`）下，为每个域名生成一个独立的JSON文件。

**`./domain_ips/kh_google_com.json` 的内容示例:**
```json
{
  "ipv4": [
    {
      "ip": "172.217.160.78",
      "ip_info": {
        "ip": "172.217.160.78",
        "city": "Mountain View",
        "region": "California",
        "country": "US",
        "org": "AS15169 Google LLC"
        // ... 更多信息
      }
    }
    // ... 更多IPv4记录
  ],
  "ipv6": [
    {
      "ip": "2607:f8b0:4004:808::200e",
      "ip_info": {
        "ip": "2607:f8b0:4004:808::200e",
        "city": "Mountain View",
        // ... 更多信息
      }
    }
    // ... 更多IPv6记录
  ]
}
```
该文件会在每次更新后被覆盖，但其中的IP列表是**只增不减**的。

## 状态转换图

```
域名IP监视器状态：
    ┌─────────────┐
    │   初始化     │
    └──────┬──────┘
           │
           │ NewRemoteIPMonitor()
           ↓
    ┌─────────────┐
    │   已创建     │
    └──────┬──────┘
           │
           │ Start()
           ↓
    ┌─────────────┐
    │   运行中     │ ────→ 定时更新 ────→ updateAllDomains()
    └──────┬──────┘                        │
           │                              │
           │ GetDomainPool()              │ processSingleDomain()
           ↓                              │
    ┌─────────────┐                       │
    │  获取数据    │                       │
    └─────────────┘                       │
           │                              │
           │                              ↓
           │                       ┌──────────────┐
           │                       │ DNS解析+IP查询│
           │                       └──────┬───────┘
           │                              │
           │                              ↓
           │                       ┌──────────────┐
           │                       │  保存到文件   │
           │                       └──────────────┘
           │
           │ Stop()
           ↓
    ┌─────────────┐
    │   已停止     │
    └─────────────┘

IP记录状态：
    ┌─────────────┐
    │   未知IP     │
    └──────┬──────┘
           │
           │ DNS解析发现
           ↓
    ┌─────────────┐
    │   新IP       │
    └──────┬──────┘
           │
           │ 查询IP信息
           ↓
    ┌─────────────┐
    │   已查询     │ ────→ 追加到历史数据库
    └──────┬──────┘
           │
           │ 保存到文件
           ↓
    ┌─────────────┐
    │   已持久化   │
    └─────────────┘
```

## 性能特点

1. **并发DNS解析**：最多50个并发查询，快速收集IP地址
2. **增量更新**：只查询新IP，节省API调用次数
3. **内存缓存**：最新数据缓存在内存中，快速读取
4. **文件持久化**：数据保存到文件，程序重启后仍可用
5. **隔离处理**：每个域名独立处理，互不影响

## 注意事项

1. **接口依赖**：业务代码应依赖于 `DomainMonitor` 接口，而非 `RemoteIPMonitor` 具体实现
2. **资源清理**：程序退出时应调用 `Stop()` 停止监视器
3. **API限制**：ipinfo.io API有调用频率限制，注意控制更新间隔
4. **存储格式**：支持JSON、YAML、TOML格式，根据需求选择
5. **DNS服务器**：配置多个DNS服务器可以提高IP收集的多样性
6. **只增不减**：IP池是累加式的，已记录的IP不会删除，确保IP池持续增长
