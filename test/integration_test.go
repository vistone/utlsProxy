package test

import (
	"testing"
	"time"
	"utlsProxy/src"
)

// TestProjectIntegration 测试项目主要组件的集成
func TestProjectIntegration(t *testing.T) {
	// 测试创建LocalIPPool
	staticIPv4s := []string{"192.168.1.1", "192.168.1.2"}
	localPool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	defer localPool.Close()

	// 测试获取IP
	ip := localPool.GetIP()
	if ip == nil {
		t.Error("应该能够从LocalIPPool获取IP")
	}

	// 测试创建WhiteBlackIPPool
	accessControl := src.NewWhiteBlackIPPool()

	// 测试添加IP到白名单
	testIP := "192.168.1.100"
	accessControl.AddIP(testIP, true)
	if !accessControl.IsIPAllowed(testIP) {
		t.Errorf("IP %s 应该被允许访问", testIP)
	}

	// 测试创建RemoteIPMonitor配置
	config := src.MonitorConfig{
		Domains:        []string{"example.com"},
		DNSServers:     []string{"8.8.8.8"},
		IPInfoToken:    "test-token",
		UpdateInterval: 2 * time.Minute,
		StorageDir:     ".",
		StorageFormat:  "json",
	}

	// 测试创建RemoteIPMonitor
	monitor, err := src.NewRemoteIPMonitor(config)
	if err != nil {
		t.Fatalf("创建RemoteIPMonitor失败: %v", err)
	}

	// 测试获取指纹
	fingerprint := src.GetRandomFingerprint()
	if fingerprint.Name == "" {
		t.Error("应该获取到有效的指纹配置文件")
	}

	// 使用monitor做一些操作以避免未使用错误
	_ = monitor
}

// TestLibraryIntegration 测试指纹库与其他组件的集成
func TestLibraryIntegration(t *testing.T) {
	// 测试创建指纹库
	library := src.NewLibrary()
	if library == nil {
		t.Fatal("应该能够创建指纹库")
	}

	// 测试获取所有配置文件
	profiles := library.All()
	if len(profiles) == 0 {
		t.Error("指纹库应该包含配置文件")
	}

	// 测试根据浏览器筛选配置文件
	chromeProfiles := library.ProfilesByBrowser("Chrome")
	if len(chromeProfiles) == 0 {
		t.Error("应该找到Chrome浏览器的配置文件")
	}

	// 测试根据平台筛选配置文件
	windowsProfiles := library.ProfilesByPlatform("Windows")
	if len(windowsProfiles) == 0 {
		t.Error("应该找到Windows平台的配置文件")
	}
}

// TestIPPoolIntegration 测试IP池相关组件的集成
func TestIPPoolIntegration(t *testing.T) {
	// 测试创建LocalIPPool
	staticIPv4s := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	localPool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	defer localPool.Close()

	// 多次获取IP并验证
	ipMap := make(map[string]int)
	for i := 0; i < 10; i++ {
		ip := localPool.GetIP()
		if ip == nil {
			t.Error("应该能够获取到IP地址")
			continue
		}
		ipMap[ip.String()]++
	}

	// 验证获取的IP都在预期列表中
	for ipStr := range ipMap {
		found := false
		for _, expectedIP := range staticIPv4s {
			if ipStr == expectedIP {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("获取到意外的IP地址: %s", ipStr)
		}
	}
}

// TestAccessControlIntegration 测试访问控制组件的集成
func TestAccessControlIntegration(t *testing.T) {
	// 创建访问控制器
	accessControl := src.NewWhiteBlackIPPool()

	// 测试默认拒绝策略
	testIP := "10.0.0.1"
	if accessControl.IsIPAllowed(testIP) {
		t.Errorf("默认情况下，IP %s 不应该被允许访问", testIP)
	}

	// 测试添加到白名单
	accessControl.AddIP(testIP, true)
	if !accessControl.IsIPAllowed(testIP) {
		t.Errorf("添加到白名单后，IP %s 应该被允许访问", testIP)
	}

	// 测试添加到黑名单（黑名单优先）
	accessControl.AddIP(testIP, false)
	if accessControl.IsIPAllowed(testIP) {
		t.Errorf("添加到黑名单后，IP %s 不应该被允许访问", testIP)
	}

	// 测试从黑名单移除
	accessControl.RemoveIP(testIP, false)
	if accessControl.IsIPAllowed(testIP) {
		t.Errorf("从黑名单移除后，IP %s 默认仍应被拒绝", testIP)
	}
}
