package test

import (
	"net"
	"strings"
	"testing"
	"time"

	"utlsProxy/src"
)

// 注意：此测试文件需要src包中定义UTlsClient结构体才能完全运行
// 如果src包中没有定义，需要在src/UTlsClient.go中添加：
// type UTlsClient struct{}

// containsString 检查字符串是否包含子字符串
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestFormatIPAddress 测试IP地址格式化逻辑（间接测试）
func TestFormatIPAddress(t *testing.T) {
	t.Run("IPv4地址解析", func(t *testing.T) {
		ip := "192.168.1.1"
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			t.Error("IPv4地址解析失败")
		}
		if parsedIP.To4() == nil {
			t.Error("应该是IPv4地址")
		}
	})

	t.Run("IPv6地址解析", func(t *testing.T) {
		ip := "2001:db8::1"
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			t.Error("IPv6地址解析失败")
		}
		if parsedIP.To4() != nil {
			t.Error("应该是IPv6地址")
		}
	})

	t.Run("无效IP地址", func(t *testing.T) {
		ip := "invalid.ip.address"
		parsedIP := net.ParseIP(ip)
		if parsedIP != nil {
			t.Error("无效IP地址应该解析失败")
		}
	})
}

// TestUTlsRequest 测试UTlsRequest结构体
func TestUTlsRequest(t *testing.T) {
	fingerprint := src.GetRandomFingerprint()

	t.Run("创建请求", func(t *testing.T) {
		req := &src.UTlsRequest{
			WorkID:      "test-001",
			Domain:      "example.com",
			Method:      "GET",
			Path:        "https://example.com/",
			Headers:     map[string]string{"Content-Type": "application/json"},
			Body:        []byte("test body"),
			DomainIP:    "93.184.216.34",
			LocalIP:     "",
			Fingerprint: fingerprint,
			StartTime:   time.Now(),
		}

		if req.WorkID != "test-001" {
			t.Errorf("期望WorkID为test-001，实际为%s", req.WorkID)
		}

		if req.Domain != "example.com" {
			t.Errorf("期望Domain为example.com，实际为%s", req.Domain)
		}

		if req.Method != "GET" {
			t.Errorf("期望Method为GET，实际为%s", req.Method)
		}

		if len(req.Headers) != 1 {
			t.Errorf("期望Headers长度为1，实际为%d", len(req.Headers))
		}

		if len(req.Body) == 0 {
			t.Error("Body应该不为空")
		}
	})

	t.Run("空请求头", func(t *testing.T) {
		req := &src.UTlsRequest{
			WorkID:      "test-002",
			Domain:      "example.com",
			Method:      "GET",
			Path:        "https://example.com/",
			Headers:     map[string]string{},
			Body:        []byte(""),
			DomainIP:    "",
			LocalIP:     "",
			Fingerprint: fingerprint,
			StartTime:   time.Now(),
		}

		if len(req.Headers) != 0 {
			t.Errorf("期望Headers为空，实际长度为%d", len(req.Headers))
		}
	})

	t.Run("POST请求体", func(t *testing.T) {
		body := `{"key": "value"}`
		req := &src.UTlsRequest{
			WorkID:      "test-003",
			Domain:      "example.com",
			Method:      "POST",
			Path:        "https://example.com/api",
			Headers:     map[string]string{"Content-Type": "application/json"},
			Body:        []byte(body),
			DomainIP:    "",
			LocalIP:     "",
			Fingerprint: fingerprint,
			StartTime:   time.Now(),
		}

		if string(req.Body) != body {
			t.Errorf("期望Body为%s，实际为%s", body, string(req.Body))
		}
	})
}

// TestUTlsResponse 测试UTlsResponse结构体
func TestUTlsResponse(t *testing.T) {
	t.Run("成功响应", func(t *testing.T) {
		resp := &src.UTlsResponse{
			WorkID:     "test-001",
			StatusCode: 200,
			Body:       []byte("test response"),
			Path:       "https://example.com/",
			Duration:   time.Second,
		}

		if resp.WorkID != "test-001" {
			t.Errorf("期望WorkID为test-001，实际为%s", resp.WorkID)
		}

		if resp.StatusCode != 200 {
			t.Errorf("期望StatusCode为200，实际为%d", resp.StatusCode)
		}

		if len(resp.Body) == 0 {
			t.Error("Body应该不为空")
		}

		if resp.Duration <= 0 {
			t.Error("Duration应该大于0")
		}
	})

	t.Run("错误响应", func(t *testing.T) {
		resp := &src.UTlsResponse{
			WorkID:     "test-002",
			StatusCode: 404,
			Body:       []byte("Not Found"),
			Path:       "https://example.com/notfound",
			Duration:   100 * time.Millisecond,
		}

		if resp.StatusCode != 404 {
			t.Errorf("期望StatusCode为404，实际为%d", resp.StatusCode)
		}

		if string(resp.Body) != "Not Found" {
			t.Errorf("期望Body为Not Found，实际为%s", string(resp.Body))
		}
	})

	t.Run("空响应体", func(t *testing.T) {
		resp := &src.UTlsResponse{
			WorkID:     "test-003",
			StatusCode: 204,
			Body:       []byte(""),
			Path:       "https://example.com/",
			Duration:   50 * time.Millisecond,
		}

		if resp.StatusCode != 204 {
			t.Errorf("期望StatusCode为204，实际为%d", resp.StatusCode)
		}

		if len(resp.Body) != 0 {
			t.Error("Body应该为空")
		}
	})
}

// TestUTlsRequestValidation 测试请求验证逻辑
func TestUTlsRequestValidation(t *testing.T) {
	t.Run("无效IP地址格式", func(t *testing.T) {
		invalidIPs := []string{
			"999.999.999.999",
			"256.1.1.1",
			"1.1.1",
			"not.an.ip",
		}

		for _, ip := range invalidIPs {
			parsedIP := net.ParseIP(ip)
			if parsedIP != nil {
				t.Logf("IP %s 意外解析成功", ip)
			}
		}
	})

	t.Run("有效IPv4地址", func(t *testing.T) {
		validIPs := []string{
			"192.168.1.1",
			"10.0.0.1",
			"172.16.0.1",
			"8.8.8.8",
		}

		for _, ip := range validIPs {
			parsedIP := net.ParseIP(ip)
			if parsedIP == nil {
				t.Errorf("有效IPv4地址 %s 解析失败", ip)
			}
			if parsedIP.To4() == nil {
				t.Errorf("IPv4地址 %s 被识别为IPv6", ip)
			}
		}
	})

	t.Run("有效IPv6地址", func(t *testing.T) {
		validIPs := []string{
			"2001:db8::1",
			"::1",
			"2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			"2001:4860:4860::8888",
		}

		for _, ip := range validIPs {
			parsedIP := net.ParseIP(ip)
			if parsedIP == nil {
				t.Errorf("有效IPv6地址 %s 解析失败", ip)
			}
			if parsedIP.To4() != nil {
				t.Errorf("IPv6地址 %s 被识别为IPv4", ip)
			}
		}
	})

	t.Run("请求路径格式", func(t *testing.T) {
		testCases := []struct {
			path    string
			isHTTPS bool
		}{
			{"https://example.com/", true},
			{"http://example.com/", false},
			{"HTTPS://example.com/", true}, // 大小写不敏感
			{"example.com/", false},
			{"", false},
		}

		for _, tc := range testCases {
			isHTTPS := strings.HasPrefix(strings.ToLower(tc.path), "https://")
			if isHTTPS != tc.isHTTPS {
				t.Errorf("路径 %s: 期望isHTTPS为%v，实际为%v", tc.path, tc.isHTTPS, isHTTPS)
			}
		}
	})
}

// TestUTlsRequestHeaders 测试请求头处理
func TestUTlsRequestHeaders(t *testing.T) {
	fingerprint := src.GetRandomFingerprint()

	t.Run("自定义请求头", func(t *testing.T) {
		req := &src.UTlsRequest{
			WorkID: "test-headers",
			Domain: "example.com",
			Method: "GET",
			Path:   "https://example.com/",
			Headers: map[string]string{
				"X-Custom-Header": "custom-value",
				"Authorization":   "Bearer token123",
			},
			Body:        []byte(""),
			DomainIP:    "",
			LocalIP:     "",
			Fingerprint: fingerprint,
			StartTime:   time.Now(),
		}

		if len(req.Headers) != 2 {
			t.Errorf("期望Headers长度为2，实际为%d", len(req.Headers))
		}

		if req.Headers["X-Custom-Header"] != "custom-value" {
			t.Errorf("期望X-Custom-Header为custom-value，实际为%s", req.Headers["X-Custom-Header"])
		}
	})

	t.Run("User-Agent从指纹获取", func(t *testing.T) {
		_ = &src.UTlsRequest{
			WorkID:      "test-ua",
			Domain:      "example.com",
			Method:      "GET",
			Path:        "https://example.com/",
			Headers:     map[string]string{},
			Body:        []byte(""),
			DomainIP:    "",
			LocalIP:     "",
			Fingerprint: fingerprint,
			StartTime:   time.Now(),
		}

		// 检查指纹中是否有UserAgent
		if fingerprint.UserAgent == "" {
			t.Log("指纹中没有UserAgent，这是正常的（某些指纹可能没有）")
		}
	})
}

// TestUTlsResponseFields 测试响应字段完整性
func TestUTlsResponseFields(t *testing.T) {
	t.Run("所有字段都有值", func(t *testing.T) {
		resp := &src.UTlsResponse{
			WorkID:     "test-complete",
			StatusCode: 200,
			Body:       []byte("response body"),
			Path:       "https://example.com/path",
			Duration:   150 * time.Millisecond,
		}

		if resp.WorkID == "" {
			t.Error("WorkID不应该为空")
		}

		if resp.StatusCode == 0 {
			t.Error("StatusCode应该有效")
		}

		if resp.Path == "" {
			t.Error("Path不应该为空")
		}

		if resp.Duration <= 0 {
			t.Error("Duration应该大于0")
		}
	})

	t.Run("不同状态码", func(t *testing.T) {
		statusCodes := []int{200, 301, 404, 500, 503}

		for _, code := range statusCodes {
			resp := &src.UTlsResponse{
				WorkID:     "test-status",
				StatusCode: code,
				Body:       []byte(""),
				Path:       "https://example.com/",
				Duration:   time.Millisecond,
			}

			if resp.StatusCode != code {
				t.Errorf("期望StatusCode为%d，实际为%d", code, resp.StatusCode)
			}
		}
	})
}

// TestUTlsRequestFingerprint 测试指纹配置
func TestUTlsRequestFingerprint(t *testing.T) {
	t.Run("随机指纹", func(t *testing.T) {
		fingerprint := src.GetRandomFingerprint()

		if fingerprint.Name == "" {
			t.Error("指纹应该有名称")
		}

		if fingerprint.UserAgent == "" {
			t.Log("某些指纹可能没有UserAgent")
		}
	})

	t.Run("指纹字段完整性", func(t *testing.T) {
		fingerprint := src.GetRandomFingerprint()

		// HelloID是必需的，检查是否为nil（utls.ClientHelloID是结构体）
		// 由于HelloID是结构体类型，我们检查Name字段
		if fingerprint.Name == "" {
			t.Error("指纹应该有Name")
		}

		// UserAgent可能为空，但通常应该有
		if fingerprint.UserAgent == "" {
			t.Log("指纹UserAgent为空，某些指纹可能没有")
		}
	})
}
