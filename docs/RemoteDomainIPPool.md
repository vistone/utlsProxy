# 域名IP监视器 (DomainMonitor)

本文档详细介绍了 `DomainMonitor` 组件的设计理念、核心功能和使用方法。这是一个高度可配置的、健壮的后台服务，用于持续监控指定域名的IP地址，并将结果持久化存储。

## 核心功能

- **接口驱动设计**: 组件通过 `DomainMonitor` 接口暴露功能，实现了业务逻辑与具体实现的解耦，提高了代码的可测试性和可扩展性。

- **并发DNS解析**: 为了获取最多样化的IP地址列表，组件会使用一个并发工作池，同时向所有您提供的DNS服务器发起请求，并收集所有独特的IP地址。

- **增量更新 (累加模式)**: 这是组件的核心逻辑。它会为每个域名维护一个独立的IP历史数据库。
    - 在每次更新时，它会加载该域名的历史IP。
    - 只为本次新发现的IP地址查询详细信息，极大地节省了API调用次数。
    - 新发现的IP会被**追加**到历史数据库中，确保IP池**只增不减**。

- **隔离存储**: 每个被监控的域名都会生成一个独立的存储文件（例如 `kh_google_com.json`），所有数据按域名进行隔离，结构清晰。

- **健壮的后台服务**:
    - 提供 `Start()` 和 `Stop()` 方法，易于管理服务的生命周期。
    - 周期性地在后台自动执行更新任务。
    - 通过 `GetDomainPool()` 方法，可以安全地从内存缓存中读取最新的IP池数据。
    - 自动创建不存在的存储目录，避免了文件写入错误。
    - 内置了一个可复用的HTTP客户端，并优化了连接池，避免了在高并发网络请求中出现资源耗尽或死锁的问题。

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
