# UTLS 客户端 (UTlsClient)

本文档详细介绍了 `UTlsClient` 模块的设计、功能和使用方法。该模块提供了一个功能强大的HTTP/HTTPS客户端，支持TLS指纹伪装、IPv4/IPv6双栈、HTTP/2和HTTP/1.1协议自动协商，以及智能连接降级机制。

## 核心功能点

- **TLS指纹伪装**：使用UTLS库模拟真实浏览器的TLS握手特征，有效规避TLS指纹检测
- **多协议支持**：自动支持HTTP/2和HTTP/1.1协议，根据服务器能力自动协商
- **HTTP/2连接复用**：通过`http2ClientConns`缓存实现HTTP/2连接复用，减少连接建立开销
- **IPv4/IPv6双栈**：完整支持IPv4和IPv6地址，自动识别和格式化
- **智能连接降级**：IP直连失败时自动降级到域名连接，提高连接成功率
- **本地IP绑定**：支持指定本地IP地址进行连接，适用于多网卡环境
- **自动请求头填充**：自动填充User-Agent和Accept-Language等请求头，模拟真实浏览器行为
- **信息性响应处理**：正确处理HTTP 1xx信息性响应，确保获取最终响应
- **热连接池集成**：支持与`HotConnPool`集成，实现连接池级别的连接复用

## 架构设计

### 接口驱动设计

模块采用接口驱动设计，通过 `UTlsClientApi` 接口定义功能契约：

```go
type UTlsClientApi interface {
    Do(req *UTlsRequest) (*UTlsResponse, error)
}
```

使用方应依赖于 `UTlsClientApi` 接口，而不是具体的 `UTlsClient` 实现，这提高了代码的可测试性和可扩展性。

### 连接管理流程

```
请求发起
    ↓
判断协议类型（HTTP/HTTPS）
    ↓
选择连接方式（IP直连/域名连接）
    ↓
建立TCP连接
    ↓
HTTPS: TLS握手（使用UTLS指纹伪装）
    ↓
协议协商（HTTP/2优先，降级到HTTP/1.1）
    ↓
发送请求
    ↓
读取响应
    ↓
返回结果
```

## 类型定义

### UTlsRequest - 请求结构体

`UTlsRequest` 定义了完整的HTTP请求信息：

```go
type UTlsRequest struct {
    WorkID      string            // 工作ID，用于标识请求
    Domain      string            // 目标域名
    Method      string            // HTTP请求方法（GET、POST等）
    Path        string            // 请求路径（完整URL）
    Headers     map[string]string // HTTP请求头映射
    Body        []byte            // 请求体内容
    DomainIP    string            // 目标域名的IP地址（可选）
    LocalIP     string            // 本地绑定的IP地址（可选）
    Fingerprint Profile           // TLS指纹配置
    StartTime   time.Time         // 请求开始时间
}
```

**字段说明**：

- `WorkID`: 用于追踪和标识请求的唯一标识符，建议使用UUID或时间戳
- `Domain`: 目标服务器域名，用于SNI（Server Name Indication）和Host头
- `Method`: HTTP方法，如 "GET"、"POST"、"PUT"、"DELETE" 等
- `Path`: 完整的请求URL，必须以 "http://" 或 "https://" 开头
- `Headers`: 自定义请求头映射，会与自动填充的请求头合并
- `Body`: 请求体字节数组，对于GET请求通常为空
- `DomainIP`: 可选的目标IP地址，如果提供则优先使用IP直连
- `LocalIP`: 可选的本地IP地址，用于在多网卡环境下指定出站网卡
- `Fingerprint`: TLS指纹配置，定义TLS握手特征（详见 `UTlsFingerPrint.go`）
- `StartTime`: 请求开始时间，用于性能统计

### UTlsResponse - 响应结构体

`UTlsResponse` 定义了HTTP响应信息：

```go
type UTlsResponse struct {
    WorkID     string        // 工作ID，与请求对应
    StatusCode int           // HTTP状态码
    Body       []byte        // 响应体内容
    Path       string        // 请求路径
    Duration   time.Duration // 请求耗时
}
```

**字段说明**：

- `WorkID`: 与请求中的WorkID对应，用于关联请求和响应
- `StatusCode`: HTTP状态码，如200、404、500等
- `Body`: 响应体字节数组，包含服务器返回的完整内容
- `Path`: 请求的原始路径
- `Duration`: 从请求开始到响应完成的总耗时

### Profile - TLS指纹配置

`Profile` 结构体定义在 `UTlsFingerPrint.go` 中，包含以下字段：

```go
type Profile struct {
    Name        string             // 配置文件名称
    HelloID     utls.ClientHelloID // TLS握手标识
    UserAgent   string             // 用户代理字符串
    Description string             // 描述信息
    Platform    string             // 平台信息
    Browser     string             // 浏览器信息
    Version     string             // 版本信息
}
```

## API 使用方法

### 1. 创建客户端实例

**推荐方式：使用统一配置文件**

项目现在支持通过 `config/config.toml` 统一管理UTLS客户端配置：

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

// 创建客户端并应用配置
client := src.NewUTlsClient()
client.ReadTimeout = cfg.UTlsClient.GetReadTimeout()  // 默认30秒
client.DialTimeout = cfg.UTlsClient.GetDialTimeout()   // 默认10秒
client.MaxRetries = cfg.UTlsClient.MaxRetries         // 默认0（不重试）
```

**传统方式：使用默认值或手动设置**

```go
import "utlsProxy/src"

// 使用默认值（ReadTimeout: 30秒, DialTimeout: 10秒, MaxRetries: 0）
client := src.NewUTlsClient()

// 或手动设置
client := &src.UTlsClient{
    ReadTimeout: 30 * time.Second,
    DialTimeout: 10 * time.Second,
    MaxRetries:  0,
}
```

**配置说明**

在 `config/config.toml` 中的 `[UTlsClient]` 段包含以下配置：

```toml
[UTlsClient]
ReadTimeoutSeconds=30  # 读取超时时间（秒）
DialTimeoutSeconds=10  # 连接超时时间（秒）
MaxRetries=0          # 最大重试次数（0表示不重试）
```

详细配置说明请参考 [配置管理文档](./Config.md)。

### 2. 准备请求

创建一个 `UTlsRequest` 对象，设置必要的请求参数：

```go
req := &src.UTlsRequest{
    WorkID:   "req-001",
    Domain:   "www.example.com",
    Method:   "GET",
    Path:     "https://www.example.com/api/data",
    Headers:  make(map[string]string),
    Body:     nil,
    DomainIP: "",  // 可选：指定IP地址
    LocalIP:  "", // 可选：指定本地IP
    Fingerprint: src.GetRandomFingerprint(), // 获取随机指纹
    StartTime: time.Now(),
}
```

### 3. 设置请求头（可选）

可以自定义请求头，未设置的请求头会自动填充：

```go
req.Headers["Authorization"] = "Bearer token123"
req.Headers["Content-Type"] = "application/json"
// User-Agent 和 Accept-Language 会自动填充
```

### 4. 执行请求

调用 `Do` 方法执行请求：

```go
resp, err := client.Do(req)
if err != nil {
    log.Fatalf("请求失败: %v", err)
}
```

### 5. 处理响应

检查响应状态并处理响应体：

```go
fmt.Printf("状态码: %d\n", resp.StatusCode)
fmt.Printf("响应体: %s\n", string(resp.Body))
fmt.Printf("耗时: %v\n", resp.Duration)
```

## 完整使用示例

### 示例1: 基本GET请求

```go
package main

import (
    "fmt"
    "log"
    "time"
    
    "utlsProxy/src"
)

func main() {
    client := &src.UTlsClient{}
    
    req := &src.UTlsRequest{
        WorkID:      fmt.Sprintf("req-%d", time.Now().Unix()),
        Domain:      "www.google.com",
        Method:      "GET",
        Path:        "https://www.google.com",
        Headers:     make(map[string]string),
        Body:        nil,
        Fingerprint: src.GetRandomFingerprint(),
        StartTime:   time.Now(),
    }
    
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("请求失败: %v", err)
    }
    
    fmt.Printf("状态码: %d\n", resp.StatusCode)
    fmt.Printf("响应长度: %d 字节\n", len(resp.Body))
    fmt.Printf("耗时: %v\n", resp.Duration)
}
```

### 示例2: POST请求（带请求体）

```go
func postExample() {
    client := &src.UTlsClient{}
    
    jsonBody := []byte(`{"name": "test", "value": 123}`)
    
    req := &src.UTlsRequest{
        WorkID:      "post-001",
        Domain:      "api.example.com",
        Method:      "POST",
        Path:        "https://api.example.com/v1/data",
        Headers: map[string]string{
            "Content-Type": "application/json",
        },
        Body:        jsonBody,
        Fingerprint: src.GetRandomFingerprint(),
        StartTime:   time.Now(),
    }
    
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("POST请求失败: %v", err)
    }
    
    fmt.Printf("POST响应状态码: %d\n", resp.StatusCode)
    fmt.Printf("响应内容: %s\n", string(resp.Body))
}
```

### 示例3: 使用指定IP地址连接

```go
func ipDirectConnectExample() {
    client := &src.UTlsClient{}
    
    req := &src.UTlsRequest{
        WorkID:      "ip-001",
        Domain:      "www.example.com",
        Method:      "GET",
        Path:        "https://www.example.com",
        Headers:     make(map[string]string),
        Body:        nil,
        DomainIP:    "93.184.216.34", // 指定目标IP
        LocalIP:     "",              // 使用默认本地IP
        Fingerprint: src.GetRandomFingerprint(),
        StartTime:   time.Now(),
    }
    
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("IP直连失败: %v", err)
    }
    
    fmt.Printf("IP直连成功，状态码: %d\n", resp.StatusCode)
}
```

### 示例4: 绑定本地IP地址

```go
func localIPBindingExample() {
    client := &src.UTlsClient{}
    
    req := &src.UTlsRequest{
        WorkID:      "local-ip-001",
        Domain:      "www.example.com",
        Method:      "GET",
        Path:        "https://www.example.com",
        Headers:     make(map[string]string),
        Body:        nil,
        DomainIP:    "",              // 使用域名解析
        LocalIP:     "192.168.1.100", // 绑定到指定本地IP
        Fingerprint: src.GetRandomFingerprint(),
        StartTime:   time.Now(),
    }
    
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("本地IP绑定失败: %v", err)
    }
    
    fmt.Printf("本地IP绑定成功，状态码: %d\n", resp.StatusCode)
}
```

### 示例5: IPv6连接示例

```go
func ipv6Example() {
    client := &src.UTlsClient{}
    
    req := &src.UTlsRequest{
        WorkID:      "ipv6-001",
        Domain:      "www.google.com",
        Method:      "GET",
        Path:        "https://www.google.com",
        Headers:     make(map[string]string),
        Body:        nil,
        DomainIP:    "2607:f8b0:4005:802::2004", // IPv6地址
        Fingerprint: src.GetRandomFingerprint(),
        StartTime:   time.Now(),
    }
    
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("IPv6连接失败: %v", err)
    }
    
    fmt.Printf("IPv6连接成功，状态码: %d\n", resp.StatusCode)
}
```

### 示例6: 使用特定指纹配置

```go
func specificFingerprintExample() {
    client := &src.UTlsClient{}
    
    // 获取指纹库实例
    lib := src.NewLibrary()
    
    // 根据名称获取特定指纹
    profile, err := lib.ProfileByName("Chrome 120")
    if err != nil {
        log.Fatalf("获取指纹失败: %v", err)
    }
    
    req := &src.UTlsRequest{
        WorkID:      "fingerprint-001",
        Domain:      "www.example.com",
        Method:      "GET",
        Path:        "https://www.example.com",
        Headers:     make(map[string]string),
        Body:        nil,
        Fingerprint: *profile, // 使用特定指纹
        StartTime:   time.Now(),
    }
    
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("请求失败: %v", err)
    }
    
    fmt.Printf("使用特定指纹成功，状态码: %d\n", resp.StatusCode)
}
```

## 完整工作流程详解

### 总体流程图

```
┌─────────────────────────────────────────────────────────────┐
│                    Do 方法入口                               │
│              (UTlsClient.Do(req *UTlsRequest))              │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
            ┌──────────────────────┐
            │  1. 记录开始时间      │
            │  startTime = Now()   │
            └──────────┬───────────┘
                       │
                       ▼
            ┌──────────────────────┐
            │  2. 判断协议类型      │
            │  检查Path是否以       │
            │  "https://"开头      │
            └──────────┬───────────┘
                       │
                       ▼
            ┌──────────────────────┐
            │  3. 设置端口号        │
            │  HTTPS → 443         │
            │  HTTP  → 80          │
            └──────────┬───────────┘
                       │
                       ▼
        ┌──────────────┴──────────────┐
        │  4. 选择连接方式             │
        └──────────┬──────────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
┌───────────────┐    ┌─────────────────┐
│ 有DomainIP?   │    │  无DomainIP     │
│  是 → IP连接  │    │  直接域名连接   │
└───────┬───────┘    └────────┬────────┘
        │                     │
        │                     │
        └──────────┬──────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  5. 建立连接          │
        │  connectWithIP()      │
        │  或                   │
        │  connectWithDomain()  │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  6. 协议协商          │
        │  (仅HTTPS)           │
        │  HTTP/2 优先          │
        │  降级到 HTTP/1.1      │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  7. 发送请求          │
        │  根据协议选择方法     │
        └──────────┬───────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
        ▼                     ▼
┌───────────────┐    ┌─────────────────┐
│  HTTP/2       │    │   HTTP/1.1      │
│  sendHTTP2    │    │  sendHTTP +     │
│  Request()    │    │  readHTTP       │
│               │    │  Response()     │
└───────┬───────┘    └────────┬────────┘
        │                     │
        └──────────┬──────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  8. 读取响应          │
        │  处理1xx信息性响应    │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  9. 计算耗时          │
        │  Duration = Now() -   │
        │        startTime      │
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  10. 关闭当前连接     │
        │  defer conn.Close()  │
        │  (关闭本次请求新建的  │
        │   连接，不涉及热连接池)│
        └──────────┬───────────┘
                   │
                   ▼
        ┌──────────────────────┐
        │  11. 返回响应         │
        │  UTlsResponse        │
        └──────────────────────┘
```

### 详细工作流程步骤

#### 阶段1: 请求初始化 (Do方法开始)

**步骤1.1: 记录开始时间**
```97:98:src/UTlsClient.go
func (c *UTlsClient) Do(req *UTlsRequest) (*UTlsResponse, error) {
	startTime := time.Now() // 记录请求开始时间
```

- **目的**: 用于计算请求总耗时
- **输入**: 无
- **输出**: `startTime` (time.Time)

**步骤1.2: 判断协议类型**
```100:107:src/UTlsClient.go
	isHTTPS := strings.HasPrefix(strings.ToLower(req.Path), "https://") // 根据路径判断是否使用HTTPS协议

	var port string // 声明端口变量
	if isHTTPS {    // 如果是HTTPS请求
		port = "443" // 设置HTTPS默认端口443
	} else { // 如果是HTTP请求
		port = "80" // 设置HTTP默认端口80
	}
```

- **目的**: 确定使用HTTP还是HTTPS协议，并设置对应端口
- **输入**: `req.Path` (请求路径)
- **输出**: `isHTTPS` (bool), `port` (string)
- **逻辑**: 
  - 检查Path是否以 "https://" 开头（不区分大小写）
  - HTTPS → 端口443
  - HTTP → 端口80

#### 阶段2: 连接建立

**步骤2.1: 选择连接策略**
```112:127:src/UTlsClient.go
	if req.DomainIP != "" { // 如果提供了目标IP地址
		connInfo, err = c.connectWithIP(req, isHTTPS, port) // 尝试使用IP地址建立连接
		if err != nil {                                     // 如果IP连接失败
			formattedIP := formatIPAddress(req.DomainIP)               // 格式化IP地址用于日志输出
			fmt.Printf("通过IP %s 连接失败，降级到域名连接: %v\n", formattedIP, err) // 输出降级日志
			connInfo, err = c.connectWithDomain(req, isHTTPS, port)    // 降级到使用域名连接
			if err != nil {                                            // 如果域名连接也失败
				return nil, fmt.Errorf("无法通过IP或域名建立连接: %w", err) // 返回连接失败错误
			}
		}
	} else { // 如果没有提供IP地址
		connInfo, err = c.connectWithDomain(req, isHTTPS, port) // 直接使用域名建立连接
		if err != nil {                                         // 如果连接失败
			return nil, fmt.Errorf("无法通过域名建立连接: %w", err) // 返回连接失败错误
		}
	}
```

- **目的**: 根据是否提供IP地址选择连接方式，实现智能降级
- **输入**: `req.DomainIP` (可选的目标IP)
- **输出**: `connInfo` (连接信息), `err` (错误)
- **策略**:
  1. **有IP地址**: 优先使用IP直连 → 失败则降级到域名连接
  2. **无IP地址**: 直接使用域名连接
- **降级机制**: IP连接失败时自动尝试域名连接，提高连接成功率

**步骤2.2: IP连接流程 (connectWithIP)**

```213:281:src/UTlsClient.go
func (c *UTlsClient) connectWithIP(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error) {
	ip := net.ParseIP(req.DomainIP) // 解析IP地址字符串为IP对象
	if ip == nil {                  // 如果解析失败
		return nil, fmt.Errorf("无效的IP地址: %s", req.DomainIP) // 返回无效IP地址错误
	}

	isIPv6 := ip.To4() == nil // 判断是否为IPv6地址（To4()返回nil表示是IPv6）

	var tcpConn net.Conn // 声明TCP连接变量
	var err error        // 声明错误变量

	dialer := net.Dialer{ // 创建拨号器对象
		Timeout: c.getDialTimeout(), // 设置连接超时时间
	}

	if req.LocalIP != "" { // 如果提供了本地IP地址
		localIP := net.ParseIP(req.LocalIP) // 解析本地IP地址字符串为IP对象
		if localIP == nil {                 // 如果解析失败
			return nil, fmt.Errorf("无效的本地IP地址: %s", req.LocalIP) // 返回无效本地IP地址错误
		}

		if localIP.To4() != nil { // 如果本地IP是IPv4地址
			dialer.LocalAddr = &net.TCPAddr{IP: localIP, Port: 0} // 设置IPv4本地地址（端口0表示自动分配）
		} else { // 如果本地IP是IPv6地址
			dialer.LocalAddr = &net.TCPAddr{IP: localIP, Port: 0} // 设置IPv6本地地址（端口0表示自动分配）
		}
	}

	tcpConn, err = dialer.Dial("tcp", net.JoinHostPort(req.DomainIP, port)) // 使用拨号器建立TCP连接

	if err != nil { // 如果TCP连接失败
		return nil, fmt.Errorf("TCP连接失败: %w", err) // 返回TCP连接失败错误
	}

	if isHTTPS { // 如果是HTTPS请求
		uConn := utls.UClient(tcpConn, &utls.Config{ // 创建UTLS客户端连接，使用TLS指纹伪装
			ServerName:         req.Domain,                 // 设置服务器名称（SNI）
			NextProtos:         []string{"h2", "http/1.1"}, // 设置支持的协议列表，优先HTTP/2，降级到HTTP/1.1
			InsecureSkipVerify: false,                      // 不跳过证书验证
		}, req.Fingerprint.HelloID) // 使用请求中的TLS指纹HelloID

		err = uConn.Handshake() // 执行TLS握手
		if err != nil {         // 如果握手失败
			tcpConn.Close()                            // 关闭TCP连接
			return nil, fmt.Errorf("TLS握手失败: %w", err) // 返回TLS握手失败错误
		}

		state := uConn.ConnectionState()               // 获取TLS连接状态
		negotiatedProtocol := state.NegotiatedProtocol // 获取协商后的协议类型

		if negotiatedProtocol == "" { // 如果没有协商到协议
			negotiatedProtocol = "http/1.1" // 默认使用HTTP/1.1协议
		}

		return &connInfo{ // 返回连接信息对象
			conn:     uConn,              // 设置UTLS连接
			protocol: negotiatedProtocol, // 设置协商的协议
			isHTTPS:  true,               // 标记为HTTPS连接
			isIPv6:   isIPv6,             // 设置IPv6标志
		}, nil // 返回nil错误表示成功
	}

	return &connInfo{ // 返回连接信息对象（HTTP请求）
		conn:     tcpConn,    // 设置TCP连接
		protocol: "http/1.1", // 设置协议为HTTP/1.1
		isHTTPS:  false,      // 标记为非HTTPS连接
		isIPv6:   isIPv6,     // 设置IPv6标志
	}, nil // 返回nil错误表示成功
}
```

**IP连接详细步骤**:

1. **解析IP地址** (行214-217)
   - 使用 `net.ParseIP()` 解析IP字符串
   - 验证IP地址有效性
   - 判断IPv4/IPv6类型

2. **创建拨号器** (行224-226)
   - 设置连接超时时间（默认10秒）
   - 如果提供了 `LocalIP`，绑定本地IP地址（行228-239）
     - 支持IPv4和IPv6本地绑定
     - 端口设为0表示自动分配

3. **建立TCP连接** (行241)
   - 使用拨号器建立TCP连接
   - 目标地址: `IP:Port` 格式

4. **TLS握手** (仅HTTPS, 行247-272)
   - 创建UTLS客户端连接
   - 配置参数:
     - `ServerName`: 设置为域名（用于SNI）
     - `NextProtos`: `["h2", "http/1.1"]` (优先HTTP/2)
     - `InsecureSkipVerify`: false (验证证书)
     - `HelloID`: 使用请求中的TLS指纹
   - 执行TLS握手
   - 获取协商后的协议类型
   - 如果未协商到协议，默认使用HTTP/1.1

5. **返回连接信息** (行267-280)
   - HTTP请求: 返回TCP连接，协议为 "http/1.1"
   - HTTPS请求: 返回UTLS连接，协议为协商结果

**步骤2.3: 域名连接流程 (connectWithDomain)**

```283:309:src/UTlsClient.go
func (c *UTlsClient) connectWithDomain(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error) {
	ips, err := net.LookupIP(req.Domain) // 解析域名获取所有IP地址
	if err != nil || len(ips) == 0 {     // 如果解析失败或没有IP地址
		return nil, fmt.Errorf("域名解析失败: %w", err) // 返回域名解析失败错误
	}

	var ip net.IP              // 声明IP变量
	for _, addr := range ips { // 遍历所有解析到的IP地址
		if addr.To4() == nil { // 如果当前地址是IPv6地址
			ip = addr // 选择IPv6地址
			break     // 跳出循环
		}
	}

	if ip == nil && len(ips) > 0 { // 如果没有找到IPv6地址且存在IP地址
		ip = ips[0] // 使用第一个IPv4地址
	}

	if ip == nil { // 如果仍然没有有效的IP地址
		return nil, fmt.Errorf("无法解析到有效的IP地址") // 返回无法解析IP地址错误
	}

	req.DomainIP = ip.String() // 将解析到的IP地址设置到请求的DomainIP字段

	return c.connectWithIP(req, isHTTPS, port) // 复用connectWithIP方法的逻辑建立连接
}
```

**域名连接详细步骤**:

1. **DNS解析** (行285)
   - 使用 `net.LookupIP()` 解析域名
   - 获取所有IP地址（IPv4和IPv6）

2. **IP地址选择策略** (行290-300)
   - **优先选择IPv6**: 遍历IP列表，优先选择IPv6地址
   - **降级到IPv4**: 如果没有IPv6，使用第一个IPv4地址
   - **错误处理**: 如果没有有效IP，返回错误

3. **设置DomainIP** (行306)
   - 将解析到的IP设置到 `req.DomainIP`
   - 便于后续复用 `connectWithIP` 逻辑

4. **复用IP连接逻辑** (行308)
   - 调用 `connectWithIP()` 建立连接
   - 避免代码重复

#### 阶段3: 请求发送与响应处理

**步骤3.1: 根据协议选择发送方法**

```129:148:src/UTlsClient.go
	defer connInfo.conn.Close() // 延迟关闭连接，确保函数返回时关闭

	var statusCode int                        // 声明状态码变量
	var body []byte                           // 声明响应体变量
	if connInfo.protocol == "h2" && isHTTPS { // 如果协商的协议是HTTP/2且是HTTPS连接
		statusCode, body, err = c.sendHTTP2Request(connInfo.conn, req) // 使用HTTP/2协议发送请求
		if err != nil {                                                // 如果发送失败
			return nil, fmt.Errorf("发送HTTP/2请求失败: %w", err) // 返回发送失败错误
		}
	} else { // 如果使用HTTP/1.1协议
		err = c.sendHTTPRequest(connInfo.conn, req) // 发送HTTP/1.1请求
		if err != nil {                             // 如果发送失败
			return nil, fmt.Errorf("发送HTTP请求失败: %w", err) // 返回发送失败错误
		}

		statusCode, body, err = c.readHTTPResponse(connInfo.conn) // 读取HTTP响应
		if err != nil {                                           // 如果读取失败
			return nil, fmt.Errorf("读取HTTP响应失败: %w", err) // 返回读取失败错误
		}
	}
```

- **目的**: 根据协商的协议选择对应的请求发送方法
- **逻辑**:
  - **HTTP/2 + HTTPS**: 使用 `sendHTTP2Request()` (一步完成发送和接收)
  - **HTTP/1.1**: 使用 `sendHTTPRequest()` + `readHTTPResponse()` (分两步)

**步骤3.2: HTTP/2请求流程 (sendHTTP2Request)**

```159:210:src/UTlsClient.go
func (c *UTlsClient) sendHTTP2Request(conn net.Conn, req *UTlsRequest) (int, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(c.getReadTimeout())) // 设置连接读取超时，使用客户端配置的超时时间

	transport := &http2.Transport{} // 创建HTTP/2传输对象

	clientConn, err := transport.NewClientConn(conn) // 使用已建立的连接创建HTTP/2客户端连接
	if err != nil {                                  // 如果创建连接失败
		return 0, nil, fmt.Errorf("创建HTTP/2客户端连接失败: %w", err) // 返回创建失败错误
	}

	httpReq, err := http.NewRequest(req.Method, req.Path, strings.NewReader(string(req.Body))) // 构建HTTP请求对象
	if err != nil {                                                                            // 如果创建请求失败
		clientConn.Close()                               // 关闭客户端连接
		return 0, nil, fmt.Errorf("创建HTTP请求失败: %w", err) // 返回创建失败错误
	}

	httpReq.Host = req.Domain             // 设置请求的Host头为域名
	for key, value := range req.Headers { // 遍历请求头映射
		httpReq.Header.Set(key, value) // 设置每个请求头
	}

	if _, exists := req.Headers["User-Agent"]; !exists { // 如果请求头中没有User-Agent
		if req.Fingerprint.UserAgent != "" { // 如果指纹配置中有User-Agent
			httpReq.Header.Set("User-Agent", req.Fingerprint.UserAgent) // 使用指纹中的User-Agent
		} else { // 如果指纹中也没有User-Agent
			randomFingerprint := fpLibrary.RandomProfile() // 随机选择一个指纹配置
			if randomFingerprint.UserAgent != "" {         // 如果随机指纹有User-Agent
				httpReq.Header.Set("User-Agent", randomFingerprint.UserAgent) // 使用随机指纹的User-Agent
			}
		}
	}

	if _, exists := req.Headers["Accept-Language"]; !exists { // 如果请求头中没有Accept-Language
		acceptLanguage := fpLibrary.RandomAcceptLanguage()    // 随机选择一个Accept-Language
		httpReq.Header.Set("Accept-Language", acceptLanguage) // 设置Accept-Language请求头
	}

	resp, err := clientConn.RoundTrip(httpReq) // 发送HTTP/2请求并获取响应
	if err != nil {                            // 如果发送失败
		clientConn.Close()                                 // 关闭客户端连接
		return 0, nil, fmt.Errorf("发送HTTP/2请求失败: %w", err) // 返回发送失败错误
	}
	defer resp.Body.Close() // 延迟关闭响应体

	body, err := io.ReadAll(resp.Body) // 读取响应体的所有内容
	if err != nil {                    // 如果读取失败
		return resp.StatusCode, nil, fmt.Errorf("读取HTTP/2响应体失败: %w", err) // 返回读取失败错误
	}

	return resp.StatusCode, body, nil // 返回状态码、响应体和nil错误
}
```

**HTTP/2请求详细步骤**:

1. **设置读取超时** (行161)
   - 使用客户端配置的读取超时（默认30秒）

2. **创建HTTP/2传输对象** (行163)
   - 创建 `http2.Transport` 实例

3. **建立HTTP/2客户端连接** (行165)
   - 使用已建立的TLS连接创建HTTP/2客户端连接
   - 复用底层连接，无需重新握手

4. **构建HTTP请求** (行170-179)
   - 使用 `http.NewRequest()` 创建请求对象
   - 设置请求方法、URL和请求体
   - 设置Host头为域名
   - 设置自定义请求头

5. **自动填充请求头** (行181-195)
   - **User-Agent填充规则**:
     - 如果请求头中已有，使用用户提供的值
     - 否则，优先使用指纹中的User-Agent
     - 如果指纹中没有，从全局指纹库随机选择
   - **Accept-Language填充规则**:
     - 如果请求头中已有，使用用户提供的值
     - 否则，从全局指纹库随机选择

6. **发送请求并获取响应** (行197)
   - 使用 `RoundTrip()` 发送请求
   - 自动处理HTTP/2的流和多路复用

7. **读取响应体** (行204)
   - 使用 `io.ReadAll()` 读取完整响应体

8. **返回结果** (行209)
   - 返回状态码、响应体字节数组和错误

**步骤3.3: HTTP/1.1请求发送流程 (sendHTTPRequest)**

```311:345:src/UTlsClient.go
func (c *UTlsClient) sendHTTPRequest(conn net.Conn, req *UTlsRequest) error {
	httpReq, err := http.NewRequest(req.Method, req.Path, strings.NewReader(string(req.Body))) // 构建HTTP请求对象
	if err != nil {                                                                            // 如果创建请求失败
		return fmt.Errorf("创建HTTP请求失败: %w", err) // 返回创建失败错误
	}

	httpReq.Host = req.Domain             // 设置请求的Host头为域名
	for key, value := range req.Headers { // 遍历请求头映射
		httpReq.Header.Set(key, value) // 设置每个请求头
	}

	if _, exists := req.Headers["User-Agent"]; !exists { // 如果请求头中没有User-Agent
		if req.Fingerprint.UserAgent != "" { // 如果指纹配置中有User-Agent
			httpReq.Header.Set("User-Agent", req.Fingerprint.UserAgent) // 使用指纹中的User-Agent
		} else { // 如果指纹中也没有User-Agent
			randomFingerprint := fpLibrary.RandomProfile() // 随机选择一个指纹配置
			if randomFingerprint.UserAgent != "" {         // 如果随机指纹有User-Agent
				httpReq.Header.Set("User-Agent", randomFingerprint.UserAgent) // 使用随机指纹的User-Agent
			}
		}
	}

	if _, exists := req.Headers["Accept-Language"]; !exists { // 如果请求头中没有Accept-Language
		acceptLanguage := fpLibrary.RandomAcceptLanguage()    // 随机选择一个Accept-Language
		httpReq.Header.Set("Accept-Language", acceptLanguage) // 设置Accept-Language请求头
	}

	err = httpReq.Write(conn) // 将HTTP请求写入连接
	if err != nil {           // 如果写入失败
		return fmt.Errorf("写入HTTP请求失败: %w", err) // 返回写入失败错误
	}

	return nil // 返回nil错误表示成功
}
```

**HTTP/1.1请求发送详细步骤**:

1. **构建HTTP请求** (行313-321)
   - 与HTTP/2相同，创建请求对象并设置基本参数

2. **自动填充请求头** (行323-337)
   - 与HTTP/2相同的填充规则

3. **写入连接** (行339)
   - 使用 `httpReq.Write()` 将请求写入TCP连接
   - HTTP/1.1是文本协议，直接写入连接

**步骤3.4: HTTP/1.1响应读取流程 (readHTTPResponse)**

```347:379:src/UTlsClient.go
func (c *UTlsClient) readHTTPResponse(conn net.Conn) (int, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(c.getReadTimeout())) // 设置连接读取超时，使用客户端配置的超时时间

	reader := bufio.NewReader(conn)             // 创建缓冲读取器
	resp, err := http.ReadResponse(reader, nil) // 读取HTTP响应
	if err != nil {                             // 如果读取失败
		return 0, nil, fmt.Errorf("读取HTTP响应失败: %w", err) // 返回读取失败错误
	}
	defer resp.Body.Close() // 延迟关闭响应体

	body := new(strings.Builder)                      // 创建字符串构建器用于存储响应体
	_, err = bufio.NewReader(resp.Body).WriteTo(body) // 将响应体内容写入字符串构建器
	if err != nil {                                   // 如果读取失败
		return resp.StatusCode, nil, fmt.Errorf("读取响应体失败: %w", err) // 返回读取失败错误
	}

	for resp.StatusCode >= 100 && resp.StatusCode < 200 { // 检查是否是信息性响应(1xx状态码)
		resp, err = http.ReadResponse(reader, nil) // 继续读取下一个响应
		if err != nil {                            // 如果读取失败
			return resp.StatusCode, []byte(body.String()), fmt.Errorf("读取最终HTTP响应失败: %w", err) // 返回读取失败错误
		}
		defer resp.Body.Close() // 延迟关闭响应体

		body.Reset()                                      // 重置字符串构建器
		_, err = bufio.NewReader(resp.Body).WriteTo(body) // 将新的响应体内容写入字符串构建器
		if err != nil {                                   // 如果读取失败
			return resp.StatusCode, []byte(body.String()), fmt.Errorf("读取最终响应体失败: %w", err) // 返回读取失败错误
		}
	}

	return resp.StatusCode, []byte(body.String()), nil // 返回状态码、响应体字节数组和nil错误
}
```

**HTTP/1.1响应读取详细步骤**:

1. **设置读取超时** (行349)
   - 使用客户端配置的读取超时（默认30秒）

2. **创建缓冲读取器** (行351)
   - 使用 `bufio.NewReader()` 包装连接
   - 提高读取效率

3. **读取HTTP响应** (行352)
   - 使用 `http.ReadResponse()` 解析HTTP响应
   - 自动解析状态行、响应头和响应体

4. **读取响应体** (行358-362)
   - 使用字符串构建器存储响应体
   - 通过 `WriteTo()` 方法读取完整响应体

5. **处理信息性响应** (行364-376)
   - **关键特性**: 正确处理HTTP 1xx信息性响应
   - **逻辑**:
     - 检查状态码是否在100-199范围内
     - 如果是信息性响应（如100 Continue），继续读取下一个响应
     - 重置响应体构建器，读取最终响应
     - 循环直到获取非1xx的最终响应

6. **返回结果** (行378)
   - 返回状态码、响应体字节数组和错误

#### 阶段4: 响应构建与返回

**步骤4.1: 构建响应对象**

```150:156:src/UTlsClient.go
	return &UTlsResponse{ // 返回响应对象
		WorkID:     req.WorkID,            // 设置工作ID
		StatusCode: statusCode,            // 设置状态码
		Body:       body,                  // 设置响应体
		Path:       req.Path,              // 设置请求路径
		Duration:   time.Since(startTime), // 计算请求耗时
	}, nil // 返回nil错误表示成功
```

- **目的**: 构建并返回完整的响应对象
- **字段说明**:
  - `WorkID`: 与请求中的WorkID对应
  - `StatusCode`: HTTP状态码
  - `Body`: 响应体字节数组
  - `Path`: 原始请求路径
  - `Duration`: 从开始到结束的总耗时

**步骤4.2: 资源清理**

```129:129:src/UTlsClient.go
	defer connInfo.conn.Close() // 延迟关闭连接，确保函数返回时关闭
```

- **目的**: 确保当前请求中创建的连接在函数返回时被正确关闭
- **机制**: 使用 `defer` 关键字，无论函数正常返回还是发生错误，都会执行关闭操作
- **重要说明**: 
  - **此处的连接关闭是指关闭当前请求中新建的连接**：`connInfo.conn` 是在本次 `Do` 调用中通过 `connectWithIP()` 或 `connectWithDomain()` 新创建的连接
  - **UTlsClient 不涉及热连接池**：UTlsClient 每次请求都创建新连接并在完成后关闭，它不管理或复用连接
  - **热连接池的连接管理**：如果需要连接复用，应使用 `UtlsClientHotConnPool`，它会在 `ReturnConn()` 时将连接归还到池中复用，而不是关闭连接
  - 参考文档：[热连接池文档](./HotConnPool.md)

### 工作流程关键特性总结

1. **智能连接降级**: IP连接失败自动降级到域名连接
2. **协议自动协商**: HTTPS优先尝试HTTP/2，失败降级到HTTP/1.1
3. **IPv6优先策略**: 域名解析时优先选择IPv6地址
4. **自动请求头填充**: 智能填充User-Agent和Accept-Language
5. **信息性响应处理**: 正确处理HTTP 1xx响应，确保获取最终响应
6. **资源自动管理**: 使用defer确保连接和响应体正确关闭
7. **超时控制**: 连接和读取都有超时保护
8. **错误处理**: 每个步骤都有详细的错误信息，便于问题定位

### ⚠️ 重要设计说明：连接管理策略

**UTlsClient 的连接管理策略**：

- **每次请求都创建新连接**：`Do` 方法每次调用都会通过 `connectWithIP()` 或 `connectWithDomain()` 建立新的TCP/TLS连接
- **请求完成后自动关闭当前连接**：使用 `defer connInfo.conn.Close()` 关闭本次请求中新建的连接（注意：这是关闭当前请求的连接，不是关闭热连接池中的连接）
- **不涉及热连接池**：UTlsClient 本身不管理连接池，每次都是创建新连接并关闭
- **设计理念**：简化使用，避免连接状态管理，适合低频请求场景

**如果需要热连接和连接复用**：

- **使用 UtlsClientHotConnPool**：提供了完整的连接池管理功能
  - 连接复用：从连接池获取已建立的连接，减少握手开销
  - 连接健康管理：自动区分健康和不健康连接
  - IP健康监控：自动管理IP黑白名单
  - 预热机制：启动时预热连接池
  - 后台维护：自动刷新IP列表、测试黑名单IP恢复
- **适用场景**：
  - 高频请求场景（需要连接复用）
  - 需要连接预热和健康管理
  - 需要IP健康监控和自动恢复
- **参考文档**：[热连接池完整文档](./HotConnPool.md)

### 数据流图

```
请求输入 (UTlsRequest)
    │
    ├─ WorkID ──────────────┐
    ├─ Domain ───────────────┤
    ├─ Method ───────────────┤
    ├─ Path ─────────────────┤
    ├─ Headers ───────────────┤
    ├─ Body ──────────────────┤
    ├─ DomainIP ──────────────┤
    ├─ LocalIP ───────────────┤
    └─ Fingerprint ───────────┤
                              │
                              ▼
                    ┌─────────────────┐
                    │   Do 方法处理    │
                    └────────┬────────┘
                             │
                ┌────────────┴────────────┐
                │                         │
                ▼                         ▼
        ┌───────────────┐        ┌───────────────┐
        │  连接建立      │        │  请求发送      │
        │  (connInfo)   │───────▶│  (statusCode) │
        └───────────────┘        └───────┬───────┘
                                          │
                                          ▼
                                ┌───────────────┐
                                │  响应读取      │
                                │  (body)       │
                                └───────┬───────┘
                                        │
                                        ▼
                            ┌───────────────────────┐
                            │  响应输出 (UTlsResponse)│
                            │                       │
                            ├─ WorkID              │
                            ├─ StatusCode          │
                            ├─ Body                │
                            ├─ Path                │
                            └─ Duration            │
```

## 核心方法详解

### Do 方法

`Do` 方法是 `UTlsClient` 的核心方法，负责执行完整的HTTP请求流程。

**方法签名**：
```go
func (c *UTlsClient) Do(req *UTlsRequest) (*UTlsResponse, error)
```

**执行流程**：

1. **协议判断**：根据 `Path` 字段判断是HTTP还是HTTPS请求
2. **端口设置**：HTTPS使用443端口，HTTP使用80端口
3. **连接建立**：
   - 如果提供了 `DomainIP`，优先使用IP直连
   - IP连接失败时，自动降级到域名连接
   - 如果没有提供 `DomainIP`，直接使用域名连接
4. **TLS握手**（仅HTTPS）：
   - 使用UTLS库创建TLS连接
   - 应用指定的TLS指纹配置
   - 协商协议（优先HTTP/2，降级到HTTP/1.1）
5. **请求发送**：
   - HTTP/2：使用 `sendHTTP2Request` 方法
   - HTTP/1.1：使用 `sendHTTPRequest` 和 `readHTTPResponse` 方法
6. **响应处理**：读取响应体，处理信息性响应（1xx状态码）
7. **资源清理**：自动关闭连接

**返回值**：
- `*UTlsResponse`: 成功时返回响应对象
- `error`: 失败时返回错误信息

### connectWithIP 方法

使用指定的IP地址建立连接。

**方法签名**：
```go
func (c *UTlsClient) connectWithIP(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error)
```

**功能**：
- 解析并验证IP地址（支持IPv4和IPv6）
- 如果提供了 `LocalIP`，绑定到指定本地IP
- 建立TCP连接
- 如果是HTTPS，执行TLS握手并协商协议
- 返回连接信息和协议类型

### connectWithDomain 方法

使用域名建立连接。

**方法签名**：
```go
func (c *UTlsClient) connectWithDomain(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error)
```

**功能**：
- 解析域名获取IP地址列表
- 优先选择IPv6地址，如果没有则选择IPv4地址
- 将解析到的IP设置到 `req.DomainIP`
- 调用 `connectWithIP` 方法建立连接

### sendHTTP2Request 方法

使用HTTP/2协议发送请求。

**方法签名**：
```go
func (c *UTlsClient) sendHTTP2Request(conn net.Conn, req *UTlsRequest) (int, []byte, error)
```

**功能**：
- 设置30秒读取超时
- 创建HTTP/2传输对象
- 构建HTTP请求
- 自动填充User-Agent和Accept-Language
- 发送请求并读取响应
- 返回状态码、响应体和错误

### sendHTTPRequest 方法

发送HTTP/1.1请求（仅发送，不读取响应）。

**方法签名**：
```go
func (c *UTlsClient) sendHTTPRequest(conn net.Conn, req *UTlsRequest) error
```

**功能**：
- 构建HTTP/1.1请求
- 设置请求头
- 自动填充User-Agent和Accept-Language
- 将请求写入连接

### readHTTPResponse 方法

读取HTTP/1.1响应。

**方法签名**：
```go
func (c *UTlsClient) readHTTPResponse(conn net.Conn) (int, []byte, error)
```

**功能**：
- 设置30秒读取超时
- 读取HTTP响应
- 处理信息性响应（1xx状态码），继续读取最终响应
- 返回状态码、响应体和错误

## 自动请求头填充机制

### User-Agent 填充规则

1. 如果请求头中已存在 `User-Agent`，使用用户提供的值
2. 如果请求头中没有 `User-Agent`：
   - 优先使用 `req.Fingerprint.UserAgent`
   - 如果指纹中没有User-Agent，从全局指纹库随机选择一个

### Accept-Language 填充规则

1. 如果请求头中已存在 `Accept-Language`，使用用户提供的值
2. 如果请求头中没有 `Accept-Language`：
   - 从全局指纹库随机选择一个语言代码

## 错误处理

### 常见错误类型

1. **连接错误**：
   - `无效的IP地址`: IP地址格式错误
   - `TCP连接失败`: 无法建立TCP连接
   - `TLS握手失败`: TLS握手过程中出错
   - `无法通过IP或域名建立连接`: 所有连接方式都失败

2. **请求错误**：
   - `创建HTTP请求失败`: 请求参数无效
   - `发送HTTP请求失败`: 请求发送失败
   - `发送HTTP/2请求失败`: HTTP/2请求发送失败

3. **响应错误**：
   - `读取HTTP响应失败`: 无法读取响应
   - `读取响应体失败`: 无法读取响应体

### 错误处理最佳实践

```go
resp, err := client.Do(req)
if err != nil {
    // 检查错误类型
    if strings.Contains(err.Error(), "连接失败") {
        // 处理连接错误
        log.Printf("连接失败，尝试重试")
    } else if strings.Contains(err.Error(), "TLS握手失败") {
        // 处理TLS错误
        log.Printf("TLS握手失败，可能需要更换指纹")
    } else {
        // 处理其他错误
        log.Printf("请求失败: %v", err)
    }
    return
}
```

## 性能优化建议

### 1. 连接复用

**UTlsClient 的连接复用机制**：

#### HTTP/2连接复用（内置支持）

`UTlsClient` 内置了HTTP/2连接复用机制，通过 `http2ClientConns` 缓存实现：

1. **自动缓存**：当使用同一个 `utls.UConn` 发送HTTP/2请求时，会自动缓存 `http2.ClientConn`
2. **智能复用**：下次使用相同的 `utls.UConn` 时，直接复用缓存的 `http2.ClientConn`，无需重新创建
3. **失效清理**：当连接失败（如 `unexpected EOF`、`403错误`）时，自动从缓存中移除失效的连接

**性能影响**：
- 首次请求：TCP握手 + TLS握手 + HTTP/2连接建立：~60-250ms
- 复用请求：仅HTTP/2请求发送：~10-50ms
- **性能提升**：复用场景下可减少80-90%的连接建立时间

**使用示例**：
```go
client := src.NewUTlsClient()

// 第一次请求：建立连接并缓存http2.ClientConn
req1 := &src.UTlsRequest{...}
resp1, err := client.Do(req1) // 建立连接，缓存http2.ClientConn

// 第二次请求：如果使用相同的utls.UConn，会复用http2.ClientConn
req2 := &src.UTlsRequest{...}
resp2, err := client.Do(req2) // 复用缓存的http2.ClientConn
```

**注意**：HTTP/2连接复用需要配合 `HotConnPool` 使用，因为：
- `UTlsClient` 每次 `Do` 调用默认会创建新连接并关闭
- 只有通过 `HotConnPool` 获取的连接才会被复用
- `HotConnPool` 管理 `utls.UConn` 的生命周期，`UTlsClient` 管理 `http2.ClientConn` 的缓存

#### 热连接池集成（推荐）

对于高频请求场景（QPS > 10），强烈建议使用 `UtlsClientHotConnPool`：

```go
// 创建热连接池
pool, err := src.NewDomainHotConnPool(config)
defer pool.Close()

// 设置热连接池
client.HotConnPool = pool

// 预热连接池
pool.Warmup()

// 使用客户端发送请求（自动使用连接池）
req := &src.UTlsRequest{...}
resp, err := client.Do(req) // 自动从连接池获取连接，HTTP/2连接会被复用
```

**热连接池的优势**：
- ✅ 连接复用：减少TCP和TLS握手开销
- ✅ HTTP/2复用：自动复用HTTP/2连接，进一步提升性能
- ✅ 连接健康管理：自动区分健康/不健康连接
- ✅ IP健康监控：自动管理IP黑白名单
- ✅ 预热机制：启动时预热连接池
- ✅ 性能提升：高频场景下可提升50-90%的吞吐量

详细使用说明请参考：[热连接池文档](./UtlsClientHotConnPool.md)

### 2. 指纹选择

- 使用随机指纹可以降低被检测的风险
- 对于特定目标，可以使用匹配的指纹（如Chrome指纹访问Google服务）

### 3. IP地址管理

- 优先使用IP直连可以跳过DNS解析，提高速度
- 使用IP池管理多个IP地址，实现负载均衡

### 4. 并发控制

虽然 `UTlsClient` 本身是线程安全的，但建议：
- 控制并发请求数量，避免资源耗尽
- 使用goroutine池管理并发请求

## 注意事项

### 1. 连接管理策略

**UTlsClient 的连接管理策略**：

#### 独立使用（无连接池）

当未设置 `HotConnPool` 时：
- ✅ **优点**：简化使用，无需管理连接状态，避免连接泄漏
- ⚠️ **缺点**：每次请求都需要TCP握手和TLS握手，开销较大
- 📌 **适用场景**：低频请求、一次性请求、测试场景
- **HTTP/2复用**：不支持（因为连接会被关闭）

#### 配合热连接池使用（推荐）

当设置 `HotConnPool` 时：
- ✅ **优点**：连接复用，减少握手开销，支持HTTP/2连接复用
- ✅ **HTTP/2复用**：自动缓存和复用 `http2.ClientConn`，进一步提升性能
- ✅ **连接管理**：连接池自动管理连接生命周期
- 📌 **适用场景**：高频请求、生产环境、需要高性能的场景
- **详细文档**：[热连接池文档](./UtlsClientHotConnPool.md)

### 2. 超时设置

- HTTP/2和HTTP/1.1响应读取超时均为30秒
- 如需自定义超时，需要修改源码或使用连接池

### 3. IPv6支持

- IPv6地址会自动添加方括号格式化
- 优先选择IPv6地址连接，提高连接速度

### 4. 协议协商

- 优先尝试HTTP/2协议
- 如果服务器不支持HTTP/2，自动降级到HTTP/1.1
- 协议选择对用户透明

### 5. 信息性响应处理

- 自动处理HTTP 1xx信息性响应（如100 Continue）
- 继续读取最终响应，确保获取完整结果

### 6. TLS指纹

- 必须提供有效的 `Fingerprint` 配置
- 使用 `src.GetRandomFingerprint()` 获取随机指纹
- 或使用指纹库的 `ProfileByName` 等方法获取特定指纹

## 依赖关系

### 外部依赖

- `github.com/refraction-networking/utls`: UTLS库，用于TLS指纹伪装
- `golang.org/x/net/http2`: HTTP/2协议支持

### 内部依赖

- `src.Profile`: TLS指纹配置（定义在 `UTlsFingerPrint.go`）
- `src.fpLibrary`: 全局指纹库实例（定义在 `UTlsFingerPrint.go`）

## 测试

参考 `test/utls_client_unit_test.go` 了解详细的测试用例和使用示例。

## 总结

`UTlsClient` 是一个功能完善的HTTP/HTTPS客户端，通过TLS指纹伪装、智能连接管理和自动协议协商，提供了强大的网络请求能力。适用于需要规避TLS指纹检测、支持多协议、多IP环境的网络应用场景。

