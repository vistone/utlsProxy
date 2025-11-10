package test

import (
	"testing"
	"utlsProxy/src"
)

// TestNewWhiteBlackIPPool 测试创建新的黑白名单IP池
func TestNewWhiteBlackIPPool(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()
	if pool == nil {
		t.Error("NewWhiteBlackIPPool应该返回一个有效的IP访问控制器")
	}
}

// TestAddIPToWhiteList 测试添加IP到白名单
func TestAddIPToWhiteList(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 添加IP到白名单
	testIP := "192.168.1.1"
	pool.AddIP(testIP, true) // true表示添加到白名单

	// 检查IP是否被允许
	if !pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 应该被允许访问", testIP)
	}
}

// TestAddIPToBlackList 测试添加IP到黑名单
func TestAddIPToBlackList(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 添加IP到黑名单
	testIP := "192.168.1.100"
	pool.AddIP(testIP, false) // false表示添加到黑名单

	// 检查IP是否被拒绝
	if pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 不应该被允许访问", testIP)
	}
}

// TestRemoveIPFromWhiteList 测试从白名单中删除IP
func TestRemoveIPFromWhiteList(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 添加IP到白名单
	testIP := "192.168.1.1"
	pool.AddIP(testIP, true)

	// 确认IP在白名单中
	if !pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 应该被允许访问", testIP)
	}

	// 从白名单中删除IP
	pool.RemoveIP(testIP, true)

	// 确认IP不再被允许
	if pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 不应该被允许访问", testIP)
	}
}

// TestRemoveIPFromBlackList 测试从黑名单中删除IP
func TestRemoveIPFromBlackList(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 添加IP到黑名单
	testIP := "192.168.1.100"
	pool.AddIP(testIP, false)

	// 确认IP被拒绝
	if pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 不应该被允许访问", testIP)
	}

	// 从黑名单中删除IP
	pool.RemoveIP(testIP, false)

	// 确认IP默认被拒绝（不在任何名单中）
	if pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 不应该被允许访问", testIP)
	}
}

// TestBlackListPriority 测试黑名单优先原则
func TestBlackListPriority(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 同时添加到黑白名单
	testIP := "192.168.1.50"
	pool.AddIP(testIP, true)  // 添加到白名单
	pool.AddIP(testIP, false) // 添加到黑名单

	// 黑名单应该优先，IP应该被拒绝
	if pool.IsIPAllowed(testIP) {
		t.Errorf("IP %s 不应该被允许访问（黑名单优先）", testIP)
	}
}

// TestGetAllowedIPs 测试获取白名单IP列表
func TestGetAllowedIPs(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 添加一些IP到白名单
	whitelistIPs := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for _, ip := range whitelistIPs {
		pool.AddIP(ip, true)
	}

	// 获取白名单
	allowedIPs := pool.GetAllowedIPs()

	// 检查返回的IP数量
	if len(allowedIPs) != len(whitelistIPs) {
		t.Errorf("期望 %d 个允许的IP，实际得到 %d 个", len(whitelistIPs), len(allowedIPs))
	}

	// 检查所有IP都存在
	for _, expectedIP := range whitelistIPs {
		found := false
		for _, actualIP := range allowedIPs {
			if expectedIP == actualIP {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("期望的IP %s 没有在允许列表中找到", expectedIP)
		}
	}
}

// TestGetBlockedIPs 测试获取黑名单IP列表
func TestGetBlockedIPs(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 添加一些IP到黑名单
	blacklistIPs := []string{"192.168.1.100", "192.168.1.101", "192.168.1.102"}
	for _, ip := range blacklistIPs {
		pool.AddIP(ip, false)
	}

	// 获取黑名单
	blockedIPs := pool.GetBlockedIPs()

	// 检查返回的IP数量
	if len(blockedIPs) != len(blacklistIPs) {
		t.Errorf("期望 %d 个阻止的IP，实际得到 %d 个", len(blacklistIPs), len(blockedIPs))
	}

	// 检查所有IP都存在
	for _, expectedIP := range blacklistIPs {
		found := false
		for _, actualIP := range blockedIPs {
			if expectedIP == actualIP {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("期望的IP %s 没有在阻止列表中找到", expectedIP)
		}
	}
}

// TestDefaultDeny 测试默认拒绝策略
func TestDefaultDeny(t *testing.T) {
	pool := src.NewWhiteBlackIPPool()

	// 未在任何名单中的IP应该被拒绝
	testIP := "10.0.0.1"
	if pool.IsIPAllowed(testIP) {
		t.Errorf("默认情况下，IP %s 不应该被允许访问", testIP)
	}
}
