# IP访问控制器 (WhiteBlackIPPool)

本文档详细介绍了 `WhiteBlackIPPool` 模块的设计、功能点、工作流程和使用方法。该模块提供了一个并发安全、高性能的IP地址黑白名单管理功能。

## 核心功能点

### 1. IP黑白名单管理
- **动态添加**：支持向白名单或黑名单中添加IP地址
- **动态删除**：支持从白名单或黑名单中删除IP地址
- **查询功能**：检查IP是否被允许访问
- **列表获取**：获取当前白名单或黑名单的所有IP快照

### 2. 并发安全
- **读写锁保护**：使用 `sync.RWMutex` 保护数据
- **多读并发**：允许多个读操作并发执行
- **写操作互斥**：写操作互斥，保证数据一致性

### 3. 安全策略
- **黑名单优先**：IP同时存在于黑白名单中时，明确拒绝
- **默认拒绝**：IP不在任何名单中时，默认拒绝访问
- **白名单允许**：IP在白名单中时，明确允许访问

### 4. 接口驱动设计
- **接口抽象**：通过 `IPAccessController` 接口定义行为契约
- **实现解耦**：业务逻辑依赖于接口，而非具体实现
- **易于扩展**：可以轻松替换实现，无需修改业务代码

## 数据结构

### IPAccessController 接口

```go
type IPAccessController interface {
    // AddIP 将一个IP地址添加到指定的名单中
    AddIP(ip string, isWhite bool)
    
    // RemoveIP 从指定的名单中删除一个IP地址
    RemoveIP(ip string, isWhite bool)
    
    // IsIPAllowed 检查一个IP地址是否被允许访问
    IsIPAllowed(ip string) bool
    
    // GetAllowedIPs 返回当前白名单中所有IP地址的快照
    GetAllowedIPs() []string
    
    // GetBlockedIPs 返回当前黑名单中所有IP地址的快照
    GetBlockedIPs() []string
}
```

### WhiteBlackIPPool 实现

```go
type WhiteBlackIPPool struct {
    whiteList IPSet        // 白名单集合 (map[string]bool)
    blackList IPSet        // 黑名单集合 (map[string]bool)
    mutex     sync.RWMutex // 读写互斥锁
}
```

## 工作流程

### 1. 初始化流程

```
创建实例
    ↓
调用 NewWhiteBlackIPPool()
    ↓
初始化结构体
    ├─ whiteList = make(IPSet)  // 空白名单
    ├─ blackList = make(IPSet)  // 空黑名单
    └─ mutex = sync.RWMutex{}   // 初始化锁
    ↓
返回 IPAccessController 接口实例
```

### 2. 添加IP流程

```
调用 AddIP(ip, isWhite)
    ↓
加写锁 (mutex.Lock())
    ↓
判断 isWhite
    ├─ true  → whiteList[ip] = true   // 加入白名单
    └─ false → blackList[ip] = true   // 加入黑名单
    ↓
释放写锁 (defer mutex.Unlock())
    ↓
完成
```

### 3. 删除IP流程

```
调用 RemoveIP(ip, isWhite)
    ↓
加写锁 (mutex.Lock())
    ↓
判断 isWhite
    ├─ true  → delete(whiteList, ip)  // 从白名单删除
    └─ false → delete(blackList, ip)  // 从黑名单删除
    ↓
释放写锁 (defer mutex.Unlock())
    ↓
完成
```

### 4. 检查IP权限流程

```
调用 IsIPAllowed(ip)
    ↓
加读锁 (mutex.RLock())
    ↓
检查黑名单
    ├─ IP在黑名单中 → 返回 false (拒绝)
    └─ IP不在黑名单 → 继续
    ↓
检查白名单
    ├─ IP在白名单中 → 返回 true (允许)
    └─ IP不在白名单 → 继续
    ↓
默认拒绝
    └─ 返回 false (拒绝)
    ↓
释放读锁 (defer mutex.RUnlock())
    ↓
返回结果
```

**判断优先级**：
1. **黑名单检查**（最高优先级）
2. **白名单检查**
3. **默认拒绝**（如果不在任何名单中）

### 5. 获取名单列表流程

```
调用 GetAllowedIPs() 或 GetBlockedIPs()
    ↓
加读锁 (mutex.RLock())
    ↓
创建结果切片
    ↓
遍历名单集合
    └─ 将所有IP添加到切片
    ↓
释放读锁 (defer mutex.RUnlock())
    ↓
返回IP列表快照
```

## 使用示例

### 1. 创建实例

```go
import "utlsProxy/src"

// 创建IP访问控制器
var acl src.IPAccessController
acl = src.NewWhiteBlackIPPool()
```

### 2. 添加IP地址

```go
// 将IP加入白名单
acl.AddIP("192.168.1.100", true)

// 将IP加入黑名单
acl.AddIP("10.0.0.5", false)
```

### 3. 检查IP权限

```go
ipToCheck := "192.168.1.100"
if acl.IsIPAllowed(ipToCheck) {
    fmt.Printf("%s is allowed.\n", ipToCheck)
} else {
    fmt.Printf("%s is not allowed.\n", ipToCheck)
}
// 输出: 192.168.1.100 is allowed.

// 未在任何名单中的IP默认拒绝
ipToCheck = "8.8.8.8"
if !acl.IsIPAllowed(ipToCheck) {
    fmt.Printf("%s is blocked by default.\n", ipToCheck)
}
// 输出: 8.8.8.8 is blocked by default.
```

### 4. 移除IP地址

```go
// 从黑名单中移除IP
acl.RemoveIP("10.0.0.5", false)

// 从白名单中移除IP
acl.RemoveIP("192.168.1.100", true)
```

### 5. 获取名单列表

```go
// 获取白名单
allowed := acl.GetAllowedIPs()
fmt.Printf("Allowed IPs: %v\n", allowed)
// 输出: Allowed IPs: [192.168.1.100]

// 获取黑名单
blocked := acl.GetBlockedIPs()
fmt.Printf("Blocked IPs: %v\n", blocked)
// 输出: Blocked IPs: [10.0.0.5]
```

## 状态转换图

```
IP状态转换：
    ┌─────────┐
    │  未知   │ (不在任何名单中)
    └────┬────┘
         │
         │ AddIP(ip, true)
         ↓
    ┌─────────┐
    │  白名单  │ ────→ IsIPAllowed() → true (允许)
    └────┬────┘
         │
         │ RemoveIP(ip, true)
         ↓
    ┌─────────┐
    │  未知   │
    └────┬────┘
         │
         │ AddIP(ip, false)
         ↓
    ┌─────────┐
    │  黑名单  │ ────→ IsIPAllowed() → false (拒绝)
    └─────────┘

特殊情况：
- IP同时在黑白名单中 → IsIPAllowed() → false (黑名单优先)
```

## 并发安全说明

### 读操作并发
- 多个 `IsIPAllowed()` 可以并发执行
- 多个 `GetAllowedIPs()` / `GetBlockedIPs()` 可以并发执行
- 使用 `RWMutex.RLock()` 实现多读并发

### 写操作互斥
- `AddIP()` 和 `RemoveIP()` 操作互斥
- 使用 `RWMutex.Lock()` 实现写操作互斥
- 写操作会阻塞所有读操作

### 读写互斥
- 写操作进行时，所有读操作等待
- 读操作进行时，写操作等待
- 保证数据一致性

## 性能特点

1. **O(1) 查询**：使用 `map[string]bool` 实现O(1)时间复杂度的IP查询
2. **高效并发**：读写锁允许多读并发，提高并发性能
3. **内存高效**：使用map存储，内存占用小
4. **无锁读取**：读操作使用读锁，不阻塞其他读操作

## 注意事项

1. **接口依赖**：业务代码应依赖于 `IPAccessController` 接口，而非 `WhiteBlackIPPool` 具体实现
2. **线程安全**：所有操作都是线程安全的，可以在多个goroutine中并发使用
3. **默认拒绝**：不在任何名单中的IP默认拒绝，符合安全原则
4. **黑名单优先**：即使IP同时在黑白名单中，也会被拒绝
5. **快照返回**：`GetAllowedIPs()` 和 `GetBlockedIPs()` 返回的是快照，不会影响原始数据

## 应用场景

- **热连接池**：管理目标IP的黑白名单，控制哪些IP可以建立连接
- **访问控制**：实现基于IP的访问控制列表（ACL）
- **安全过滤**：过滤被封禁或可疑的IP地址
- **动态管理**：支持运行时动态添加和删除IP规则
