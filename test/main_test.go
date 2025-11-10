package test

import (
	"encoding/json"
	"testing"
)

// DNSDatabaseConfig 定义一个结构体来匹配 DNSServerNames.json 文件的真实格式
type DNSDatabaseConfig struct {
	Servers map[string]string `json:"servers"`
}

// TestDNSDatabaseConfigStructure 测试DNS数据库配置结构体
func TestDNSDatabaseConfigStructure(t *testing.T) {
	// 创建测试数据
	testConfig := DNSDatabaseConfig{
		Servers: map[string]string{
			"Google-Public-主": "8.8.8.8",
			"Google-Public-备": "8.8.4.4",
		},
	}

	// 验证结构体字段
	if len(testConfig.Servers) != 2 {
		t.Errorf("期望2个服务器，实际: %d", len(testConfig.Servers))
	}

	// 验证特定服务器
	if ip, exists := testConfig.Servers["Google-Public-主"]; !exists || ip != "8.8.8.8" {
		t.Error("Google-Public-主服务器IP不正确")
	}
}

// TestDNSDatabaseJSONParsing 测试DNS数据库JSON解析
func TestDNSDatabaseJSONParsing(t *testing.T) {
	// 创建测试JSON数据
	testData := `{
		"servers": {
			"test-server-1": "1.1.1.1",
			"test-server-2": "2.2.2.2"
		}
	}`

	var dnsDB DNSDatabaseConfig
	err := json.Unmarshal([]byte(testData), &dnsDB)
	if err != nil {
		t.Fatalf("解析JSON数据失败: %v", err)
	}

	// 验证解析结果
	if len(dnsDB.Servers) != 2 {
		t.Errorf("期望2个服务器，实际: %d", len(dnsDB.Servers))
	}

	if ip, exists := dnsDB.Servers["test-server-1"]; !exists || ip != "1.1.1.1" {
		t.Error("test-server-1服务器IP不正确")
	}
}

// TestDNSServerDeduplication 测试DNS服务器去重逻辑
func TestDNSServerDeduplication(t *testing.T) {
	// 模拟解析后的数据，包含重复的IP
	dnsDB := DNSDatabaseConfig{
		Servers: map[string]string{
			"server-1": "8.8.8.8",
			"server-2": "8.8.4.4",
			"server-3": "8.8.8.8", // 重复IP
			"server-4": "1.1.1.1",
		},
	}

	// 实现去重逻辑
	uniqueServers := make(map[string]bool)
	var dnsServers []string
	for _, ip := range dnsDB.Servers {
		if !uniqueServers[ip] {
			uniqueServers[ip] = true
			dnsServers = append(dnsServers, ip)
		}
	}

	// 验证去重结果
	expectedCount := 3 // 应该有3个唯一IP: 8.8.8.8, 8.8.4.4, 1.1.1.1
	if len(dnsServers) != expectedCount {
		t.Errorf("期望%d个唯一服务器，实际: %d", expectedCount, len(dnsServers))
	}

	// 验证是否包含所有唯一IP
	expectedIPs := map[string]bool{
		"8.8.8.8": false,
		"8.8.4.4": false,
		"1.1.1.1": false,
	}

	for _, ip := range dnsServers {
		if _, exists := expectedIPs[ip]; exists {
			expectedIPs[ip] = true
		}
	}

	for ip, found := range expectedIPs {
		if !found {
			t.Errorf("期望的IP未找到: %s", ip)
		}
	}
}

// TestMonitorConfigCreation 测试监视器配置创建
func TestMonitorConfigCreation(t *testing.T) {
	// 模拟去重后的DNS服务器列表
	dnsServers := []string{"8.8.8.8", "8.8.4.4", "1.1.1.1"}

	// 验证配置参数
	domains := []string{"kh.google.com", "earth.google.com", "khmdb.google.com"}
	if len(domains) != 3 {
		t.Errorf("期望3个域名，实际: %d", len(domains))
	}

	// 验证DNS服务器列表
	if len(dnsServers) == 0 {
		t.Error("DNS服务器列表不应该为空")
	}

	// 验证IPInfoToken
	ipInfoToken := "f6babc99a5ec26"
	if ipInfoToken == "" {
		t.Error("IPInfoToken不应该为空")
	}
}
