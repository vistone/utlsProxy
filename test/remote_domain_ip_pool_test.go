package test

import (
	"testing"
	"time"
	"utlsProxy/src"
)

// TestNewRemoteIPMonitor 测试创建RemoteIPMonitor实例
func TestNewRemoteIPMonitor(t *testing.T) {
	// 测试正常配置
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}
	if monitor == nil {
		t.Error("RemoteIPMonitor应该不为nil")
	}
}

// TestNewRemoteIPMonitorWithEmptyDomains 测试空域名列表
func TestNewRemoteIPMonitorWithEmptyDomains(t *testing.T) {
	config := src.MonitorConfig{
		Domains:        []string{}, // 空域名列表
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err == nil {
		t.Error("应该返回错误，因为域名列表为空")
	}
	if monitor != nil {
		t.Error("当域名列表为空时，monitor应该为nil")
	}
	expectedErrMsg := "域名列表不能为空"
	if err.Error() != expectedErrMsg {
		t.Errorf("错误消息不匹配，期望: %s, 实际: %s", expectedErrMsg, err.Error())
	}
}

// TestNewRemoteIPMonitorWithDefaultDNSServers 测试默认DNS服务器
func TestNewRemoteIPMonitorWithDefaultDNSServers(t *testing.T) {
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{}, // 空DNS服务器列表
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}
	if monitor == nil {
		t.Error("RemoteIPMonitor应该不为nil")
	}
}

// TestNewRemoteIPMonitorWithDefaultStorageDir 测试默认存储目录
func TestNewRemoteIPMonitorWithDefaultStorageDir(t *testing.T) {
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     "", // 空存储目录
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}
	if monitor == nil {
		t.Error("RemoteIPMonitor应该不为nil")
	}
}

// TestNewRemoteIPMonitorWithShortUpdateInterval 测试短更新间隔
func TestNewRemoteIPMonitorWithShortUpdateInterval(t *testing.T) {
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 30 * time.Second, // 短更新间隔
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}
	if monitor == nil {
		t.Error("RemoteIPMonitor应该不为nil")
	}
}

// TestRemoteIPMonitorStartAndStop 测试启动和停止监视器
func TestRemoteIPMonitorStartAndStop(t *testing.T) {
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}

	// 测试启动
	monitor.Start()

	// 测试停止
	monitor.Stop()
}

// TestGetDomainPoolWithNonExistentDomain 测试获取不存在域名的IP池
func TestGetDomainPoolWithNonExistentDomain(t *testing.T) {
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}

	// 尝试获取不存在域名的IP池
	pool, found := monitor.GetDomainPool("nonexistent.com")
	if found {
		t.Error("不应该找到不存在的域名")
	}
	if pool != nil {
		t.Error("对于不存在的域名，pool应该为nil")
	}
}

// TestIPRecordStructure 测试IPRecord结构体
func TestIPRecordStructure(t *testing.T) {
	record := src.IPRecord{
		IP: "192.168.1.1",
		IPInfo: &src.IPInfoResponse{
			IP:          "192.168.1.1",
			Hostname:    "test.example.com",
			City:        "Test City",
			Country:     "Test Country",
			CountryCode: "TC",
		},
	}

	if record.IP != "192.168.1.1" {
		t.Errorf("IP不匹配，期望: 192.168.1.1, 实际: %s", record.IP)
	}
	if record.IPInfo == nil {
		t.Error("IPInfo不应该为nil")
	}
	if record.IPInfo.IP != "192.168.1.1" {
		t.Errorf("IPInfo中的IP不匹配，期望: 192.168.1.1, 实际: %s", record.IPInfo.IP)
	}
}
