# 测试目录说明

该目录包含了utlsProxy项目的单元测试和集成测试。

## 目录结构

```
test/
├── integration_test.go       # 项目集成测试
├── localip_pool_test.go     # LocalIPPool组件测试
├── main_test.go             # 主程序测试
├── remote_domain_ip_pool_test.go # RemoteDomainIPPool组件测试
├── utls_fingerprint_test.go # UTlsFingerPrint组件测试
└── whiteblack_ip_pool_test.go # WhiteBlackIPPool组件测试
```

## 测试运行方法

### 运行所有测试

```bash
go test ./test/...
```

### 运行特定测试文件

```bash
go test ./test/localip_pool_test.go
go test ./test/whiteblack_ip_pool_test.go
go test ./test/remote_domain_ip_pool_test.go
go test ./test/utls_fingerprint_test.go
go test ./test/main_test.go
go test ./test/integration_test.go
```

### 运行特定测试用例

```bash
go test -run TestNewLocalIPPoolWithIPv4Only
go test -run TestLibraryRandomProfile
```

### 详细输出模式

```bash
go test -v ./test/...
```

## 测试覆盖范围

### LocalIPPool测试
- IPv4地址池创建和管理
- IP地址获取功能
- 资源清理和关闭功能

### WhiteBlackIPPool测试
- IP黑白名单管理
- 访问控制策略（黑名单优先、默认拒绝）
- IP列表获取功能

### RemoteDomainIPPool测试
- 域名监控器创建和配置
- 域名IP池数据管理
- 错误处理和边界条件

### UTlsFingerPrint测试
- 浏览器指纹配置文件管理
- 随机指纹选择
- 按浏览器/平台筛选配置文件
- Accept-Language头部生成

### Main程序测试
- DNS服务器配置解析
- JSON数据处理
- 配置去重逻辑

### 集成测试
- 各组件协同工作测试
- 项目整体功能验证

## 注意事项

1. 某些测试可能需要网络连接（如RemoteDomainIPPool相关测试）
2. 部分测试涉及时间相关的操作，可能需要较长运行时间
3. 测试数据均为模拟数据，不会影响生产环境