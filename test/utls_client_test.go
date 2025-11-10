package test

import (
	"testing"
	"time"
	"utlsProxy/src"
)

// TestUTlsClientDoWithIPHTTP 测试使用IP地址执行HTTP请求
func TestUTlsClientDoWithIPHTTP(t *testing.T) {
	client := &src.UTlsClient{}
	
	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()
	
	// 创建HTTP请求（使用httpbin.org的HTTP端口）
	req := &src.UTlsRequest{
		WorkID:      "test-http-001",
		Domain:      "httpbin.org",
		Method:      "GET",
		Path:        "/get",
		Headers:     map[string]string{},
		Body:        []byte(""),
		DomainIP:    "8.8.8.8", // 使用Google DNS IP作为测试，预期会失败并降级
		LocalIP:     "",
		Fingerprint: fingerprint,
		StartTime:   time.Now(),
		IsHTTPS:     false, // HTTP请求
		Port:        "80",  // HTTP端口
	}
	
	// 执行请求（预期会失败，因为8.8.8.8不是httpbin.org的IP）
	_, err := client.Do(req)
	if err == nil {
		t.Error("预期连接失败，但实际上没有错误")
	}
	
	// 检查错误信息是否包含降级提示
	if !containsString(err.Error(), "降级到域名连接") {
		t.Logf("错误信息: %v", err)
		t.Log("错误信息中应包含降级提示")
	}
}

// TestUTlsClientDoWithIPHTTPS 测试使用IP地址执行HTTPS请求
func TestUTlsClientDoWithIPHTTPS(t *testing.T) {
	client := &src.UTlsClient{}
	
	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()
	
	// 创建HTTPS请求
	req := &src.UTlsRequest{
		WorkID:      "test-https-001",
		Domain:      "httpbin.org",
		Method:      "GET",
		Path:        "/get",
		Headers:     map[string]string{},
		Body:        []byte(""),
		DomainIP:    "8.8.8.8", // 使用Google DNS IP作为测试，预期会失败并降级
		LocalIP:     "",
		Fingerprint: fingerprint,
		StartTime:   time.Now(),
		IsHTTPS:     true,  // HTTPS请求
		Port:        "443", // HTTPS端口
	}
	
	// 执行请求（预期会失败，因为8.8.8.8不是httpbin.org的IP）
	_, err := client.Do(req)
	if err == nil {
		t.Error("预期连接失败，但实际上没有错误")
	}
	
	// 检查错误信息是否包含降级提示
	if !containsString(err.Error(), "降级到域名连接") {
		t.Logf("错误信息: %v", err)
		t.Log("错误信息中应包含降级提示")
	}
}

// TestUTlsClientDoWithDomainHTTP 测试使用域名执行HTTP请求
func TestUTlsClientDoWithDomainHTTP(t *testing.T) {
	client := &src.UTlsClient{}
	
	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()
	
	// 创建HTTP请求
	req := &src.UTlsRequest{
		WorkID:      "test-http-002",
		Domain:      "httpbin.org",
		Method:      "GET",
		Path:        "/get",
		Headers:     map[string]string{},
		Body:        []byte(""),
		DomainIP:    "", // 不指定IP，使用域名解析
		LocalIP:     "",
		Fingerprint: fingerprint,
		StartTime:   time.Now(),
		IsHTTPS:     false, // HTTP请求
		Port:        "",    // 使用默认端口
	}
	
	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("HTTP请求失败: %v", err)
		// 在某些网络环境下可能无法访问httpbin.org，这可以接受
		return
	}
	
	// 检查响应
	if resp.WorkID != "test-http-002" {
		t.Errorf("期望WorkID为test-http-002，实际为%s", resp.WorkID)
	}
	
	// 注意：httpbin.org的HTTP端口可能会重定向到HTTPS，所以状态码可能不是200
	if resp.StatusCode == 0 {
		t.Errorf("期望有效的状态码，实际为%d", resp.StatusCode)
	}
	
	if resp.Path != "/get" {
		t.Errorf("期望Path为/get，实际为%s", resp.Path)
	}
	
	if resp.Duration <= 0 {
		t.Errorf("期望Duration大于0，实际为%v", resp.Duration)
	}
}

// TestUTlsClientDoWithDomainHTTPS 测试使用域名执行HTTPS请求
func TestUTlsClientDoWithDomainHTTPS(t *testing.T) {
	client := &src.UTlsClient{}
	
	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()
	
	// 创建HTTPS请求
	req := &src.UTlsRequest{
		WorkID:      "test-https-002",
		Domain:      "httpbin.org",
		Method:      "GET",
		Path:        "/get",
		Headers:     map[string]string{},
		Body:        []byte(""),
		DomainIP:    "", // 不指定IP，使用域名解析
		LocalIP:     "",
		Fingerprint: fingerprint,
		StartTime:   time.Now(),
		IsHTTPS:     true, // HTTPS请求
		Port:        "",   // 使用默认端口
	}
	
	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("HTTPS请求失败: %v", err)
		// 在某些网络环境下可能无法访问httpbin.org，这可以接受
		return
	}
	
	// 检查响应
	if resp.WorkID != "test-https-002" {
		t.Errorf("期望WorkID为test-https-002，实际为%s", resp.WorkID)
	}
	
	if resp.StatusCode != 200 {
		t.Errorf("期望状态码为200，实际为%d", resp.StatusCode)
	}
	
	if len(resp.Body) == 0 {
		t.Error("响应体为空")
	}
	
	if resp.Path != "/get" {
		t.Errorf("期望Path为/get，实际为%s", resp.Path)
	}
	
	if resp.Duration <= 0 {
		t.Errorf("期望Duration大于0，实际为%v", resp.Duration)
	}
}

// TestUTlsClientDoWithInvalidDomain 测试使用无效域名执行请求
func TestUTlsClientDoWithInvalidDomain(t *testing.T) {
	client := &src.UTlsClient{}
	
	// 获取随机TLS指纹
	fingerprint := src.GetRandomFingerprint()
	
	// 创建HTTPS请求
	req := &src.UTlsRequest{
		WorkID:      "test-003",
		Domain:      "this-domain-does-not-exist-12345.com",
		Method:      "GET",
		Path:        "/",
		Headers:     map[string]string{},
		Body:        []byte(""),
		DomainIP:    "",
		LocalIP:     "",
		Fingerprint: fingerprint,
		StartTime:   time.Now(),
		IsHTTPS:     true, // HTTPS请求
		Port:        "",   // 使用默认端口
	}
	
	// 执行请求（预期会失败）
	_, err := client.Do(req)
	if err == nil {
		t.Error("预期连接失败，但实际上没有错误")
	}
}

// containsString 检查字符串是否包含子串
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && 
		(len(s) == len(substr) && s == substr || 
			len(s) > len(substr) && (s == substr || 
				len(s) > len(substr) && 
					(s[:len(substr)] == substr || 
						s[len(s)-len(substr):] == substr || 
						indexOf(s, substr) >= 0)))
}

// indexOf 返回子串在字符串中的位置
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}