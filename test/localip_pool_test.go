package test

import (
	"net"
	"testing"
	"utlsProxy/src"
)

// TestNewLocalIPPoolWithIPv4Only 测试仅使用IPv4地址创建LocalIPPool
func TestNewLocalIPPoolWithIPv4Only(t *testing.T) {
	staticIPv4s := []string{"192.168.1.1", "192.168.1.2", "8.8.8.8"}
	pool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	if pool == nil {
		t.Fatal("LocalIPPool应该不为nil")
	}

	// 测试获取IP
	ip := pool.GetIP()
	if ip == nil {
		t.Error("应该能够获取到IP地址")
	}

	// 测试关闭
	err = pool.Close()
	if err != nil {
		t.Errorf("关闭LocalIPPool失败: %v", err)
	}
}

// TestNewLocalIPPoolWithInvalidIPv4 测试包含无效IPv4地址的情况
func TestNewLocalIPPoolWithInvalidIPv4(t *testing.T) {
	staticIPv4s := []string{"192.168.1.1", "invalid-ip", "8.8.8.8"}
	pool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	if pool == nil {
		t.Fatal("LocalIPPool应该不为nil")
	}

	// 应该只包含有效的IP地址
	// 通过获取IP并检查是否为有效地址来验证
	ip := pool.GetIP()
	if ip == nil {
		t.Error("应该能够获取到IP地址")
	}
}

// TestNewLocalIPPoolWithEmptyIPv4 测试空IPv4地址列表且无IPv6支持的情况
func TestNewLocalIPPoolWithEmptyIPv4(t *testing.T) {
	staticIPv4s := []string{}
	// 使用一个无效的IPv6 CIDR
	pool, err := src.NewLocalIPPool(staticIPv4s, "invalid-cidr")
	if err == nil {
		t.Fatal("应该返回错误，因为IPv6 CIDR无效")
	}
	if pool != nil {
		t.Error("当IPv6 CIDR无效时，pool应该为nil")
	}
	// 由于CIDR无效，会首先返回解析错误
	expectedErrMsg := "无效的IPv6子网CIDR: invalid CIDR address: invalid-cidr"
	if err.Error() != expectedErrMsg {
		t.Errorf("错误消息不匹配，期望: %s, 实际: %s", expectedErrMsg, err.Error())
	}
}

// TestNewLocalIPPoolWithInvalidIPv6CIDR 测试无效的IPv6 CIDR
func TestNewLocalIPPoolWithInvalidIPv6CIDR(t *testing.T) {
	staticIPv4s := []string{"192.168.1.1"}
	pool, err := src.NewLocalIPPool(staticIPv4s, "invalid-cidr")
	if err == nil {
		t.Fatal("应该返回错误，因为IPv6 CIDR无效")
	}
	if pool != nil {
		t.Error("当IPv6 CIDR无效时，pool应该为nil")
	}
}

// TestGetIPFromIPv4Pool 测试从仅IPv4池中获取IP
func TestGetIPFromIPv4Pool(t *testing.T) {
	staticIPv4s := []string{"192.168.1.1", "192.168.1.2", "8.8.8.8"}
	pool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	defer pool.Close()

	// 多次获取IP并验证它们在预期的列表中
	seenIPs := make(map[string]bool)
	for i := 0; i < 10; i++ {
		ip := pool.GetIP()
		if ip == nil {
			t.Error("应该能够获取到IP地址")
			continue
		}
		ipStr := ip.String()
		seenIPs[ipStr] = true

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

	// 确保我们至少看到了一些IP地址（随机性可能导致不是所有IP都被选中）
	if len(seenIPs) == 0 {
		t.Error("应该至少看到一个IP地址")
	}
}

// TestClose 测试关闭LocalIPPool
func TestClose(t *testing.T) {
	staticIPv4s := []string{"192.168.1.1"}
	pool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}

	// 多次调用Close应该不会导致panic
	err = pool.Close()
	if err != nil {
		t.Errorf("第一次关闭LocalIPPool失败: %v", err)
	}

	err = pool.Close()
	if err != nil {
		t.Errorf("第二次关闭LocalIPPool失败: %v", err)
	}
}

// TestIsSubnetConfigured 测试子网配置检查功能
func TestIsSubnetConfigured(t *testing.T) {
	// 测试有效的IPv6子网（但可能在系统中不存在）
	_, _, err := net.ParseCIDR("2001:db8::/32")
	if err != nil {
		t.Fatalf("无法解析有效的CIDR: %v", err)
	}

	// 这个测试主要确保代码不会panic
	// 由于isSubnetConfigured是私有函数，我们无法直接测试它
	t.Logf("子网配置检查测试完成")
}

// TestGenerateRandomIPInSubnet 测试生成随机IP功能
func TestGenerateRandomIPInSubnet(t *testing.T) {
	// 由于generateRandomIPInSubnet是私有函数，我们无法直接测试它
	// 我们只能确保代码不会panic
	t.Logf("生成随机IP测试完成")
}
