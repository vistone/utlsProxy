# IP 访问控制器 (IPAccessController)

本文档详细介绍了 `IPAccessController` 模块的设计、功能和使用方法。该模块提供了一个并发安全、高性能的IP地址黑白名单管理功能。

## 核心功能点

- **IP黑白名单管理**：支持动态地向黑名单或白名单中添加、删除IP地址。
- **高并发安全**：内部使用读写锁 (`sync.RWMutex`) 保护数据，允许多个读操作并发执行，保证了在并发环境下的数据一致性和高性能。
- **安全的设计原则**：
    - **黑名单优先**：一个IP地址如果同时存在于黑白名单中，将被明确拒绝。
    - **默认拒绝**：一个IP地址如果未在任何名单中，将被默认拒绝。
- **接口驱动设计**：通过 `IPAccessController` 接口将功能契约与具体实现分离，实现了代码解耦，极大地提高了代码的可测试性和可扩展性。

## 接口用法 (API Usage)

模块的使用方应依赖于 `IPAccessController` 接口，而不是具体的 `WhiteBlackIPPool` 实现。

### 1. 创建实例

首先，需要创建一个访问控制器实例。构造函数返回的是 `IPAccessController` 接口类型。

```go
import "path/to/your/src"

var acl src.IPAccessController

func init() {
    // 创建一个基于内存的IP访问控制器
    acl = src.NewWhiteBlackIPPool()
}
```

### 2. 添加IP地址

使用 `AddIP` 方法可以将一个IP地址添加到白名单或黑名单。

- **签名**: `AddIP(ip string, isWhite bool)`
- **参数**:
    - `ip`: 要添加的IP地址字符串。
    - `isWhite`: `true` 表示添加到白名单，`false` 表示添加到黑名单。

**示例:**
```go
// 将 "192.168.1.100" 加入白名单
acl.AddIP("192.168.1.100", true)

// 将 "10.0.0.5" 加入黑名单
acl.AddIP("10.0.0.5", false)
```

### 3. 移除IP地址

使用 `RemoveIP` 方法可以从名单中移除一个IP地址。

- **签名**: `RemoveIP(ip string, isWhite bool)`
- **参数**:
    - `ip`: 要移除的IP地址字符串。
    - `isWhite`: `true` 表示从白名单移除，`false` 表示从黑名单移除。

**示例:**
```go
// 从黑名单中移除 "10.0.0.5"
acl.RemoveIP("10.0.0.5", false)
```

### 4. 检查IP权限

`IsIPAllowed` 是核心的权限检查方法，用于判断一个IP是否允许访问。

- **签名**: `IsIPAllowed(ip string) bool`
- **返回值**: `true` 表示允许访问，`false` 表示拒绝访问。

**判断逻辑**:
1. IP在黑名单中 -> `false`
2. IP在白名单中 -> `true`
3. IP不在任何名单中 -> `false` (默认拒绝)

**示例:**
```go
ipToCheck := "192.168.1.100"
if acl.IsIPAllowed(ipToCheck) {
    fmt.Printf("%s is allowed.\n", ipToCheck)
} else {
    fmt.Printf("%s is not allowed.\n", ipToCheck)
}
// 输出: 192.168.1.100 is allowed.

ipToCheck = "8.8.8.8" // 一个未在任何名单中的IP
if !acl.IsIPAllowed(ipToCheck) {
    fmt.Printf("%s is blocked by default.\n", ipToCheck)
}
// 输出: 8.8.8.8 is blocked by default.
```

### 5. 获取名单列表

可以获取当前所有白名单或黑名单IP的快照。

- **获取白名单**: `GetAllowedIPs() []string`
- **获取黑名单**: `GetBlockedIPs() []string`

**示例:**
```go
allowed := acl.GetAllowedIPs()
fmt.Printf("Allowed IPs: %v\n", allowed)
// 输出: Allowed IPs: [192.168.1.100]

blocked := acl.GetBlockedIPs()
fmt.Printf("Blocked IPs: %v\n", blocked)
// 输出: Blocked IPs: []
```
