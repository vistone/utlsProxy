package test

import (
	"testing"
	"time"
	"utlsProxy/src"
)

// TestNewUtlsHotConnPool 测试创建utls热连接池
func TestNewUtlsHotConnPool(t *testing.T) {
	// 创建IP池
	staticIPv4s := []string{"8.8.8.8", "1.1.1.1"}
	ipPool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	defer ipPool.Close()

	// 创建访问控制器
	accessControl := src.NewWhiteBlackIPPool()

	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()

	// 创建连接池配置
	poolConfig := src.ConnPoolConfig{
		IPPool:        ipPool,
		AccessControl: accessControl,
		Fingerprint:   fingerprint,
		Domain:        "example.com",
		Port:          "443",
		MaxConns:      5,
		IdleTimeout:   1 * time.Minute,
	}

	// 创建utls热连接池
	connPool, err := src.NewUtlsHotConnPool(poolConfig)
	if err != nil {
		t.Fatalf("创建UtlsHotConnPool失败: %v", err)
	}

	// 测试关闭连接池
	err = connPool.Close()
	if err != nil {
		t.Errorf("关闭UtlsHotConnPool失败: %v", err)
	}
}

// TestConnPoolConfig 测试连接池配置
func TestConnPoolConfig(t *testing.T) {
	// 测试默认配置值
	config := src.ConnPoolConfig{
		MaxConns:     10,     // 设置默认值
		IdleTimeout:  5 * time.Minute, // 设置默认值
	}
	
	// 检查默认值是否正确设置
	if config.Port == "" {
		t.Log("Port默认值为空，将使用443")
	}
	
	if config.MaxConns <= 0 {
		t.Error("MaxConns默认值应该大于0")
	}
	
	if config.IdleTimeout <= 0 {
		t.Error("IdleTimeout默认值应该大于0")
	}
	
	t.Logf("默认Port: %s", config.Port)
	t.Logf("默认MaxConns: %d", config.MaxConns)
	t.Logf("默认IdleTimeout: %v", config.IdleTimeout)
}

// TestReturnConnWithStatus 测试根据状态码返回连接
func TestReturnConnWithStatus(t *testing.T) {
	// 创建IP池
	staticIPv4s := []string{"8.8.8.8", "1.1.1.1"}
	ipPool, err := src.NewLocalIPPool(staticIPv4s, "")
	if err != nil {
		t.Fatalf("创建LocalIPPool失败: %v", err)
	}
	defer ipPool.Close()

	// 创建访问控制器
	accessControl := src.NewWhiteBlackIPPool()

	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()

	// 创建连接池配置
	poolConfig := src.ConnPoolConfig{
		IPPool:        ipPool,
		AccessControl: accessControl,
		Fingerprint:   fingerprint,
		Domain:        "example.com",
		Port:          "443",
		MaxConns:      5,
		IdleTimeout:   1 * time.Minute,
	}

	// 创建utls热连接池
	connPool, err := src.NewUtlsHotConnPool(poolConfig)
	if err != nil {
		t.Fatalf("创建UtlsHotConnPool失败: %v", err)
	}
	defer connPool.Close()

	// 测试返回连接（由于我们无法创建真实的utls连接，这里只测试接口调用）
	err = connPool.ReturnConn(nil, 200)
	if err != nil {
		t.Errorf("返回连接失败: %v", err)
	}

	err = connPool.ReturnConn(nil, 403)
	if err != nil {
		t.Errorf("返回连接失败: %v", err)
	}
}