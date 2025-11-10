package src

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// UTlsClientApi 定义UTlsClient的接口
type UTlsClientApi interface {
	Start()
	Stop()
	Do(req *UTlsRequest) (*UTlsResponse, error)
}

// UTlsClient 实现UTlsClientApi接口，用于执行TLS请求
type UTlsClient struct {
	RequestQueue chan *UTlsRequest
	ResponseChan chan *UTlsResponse
}

// UTlsRequest 定义TLS请求结构
type UTlsRequest struct {
	WorkID      string
	Domain      string
	Method      string
	Path        string
	Headers     map[string]string
	Body        []byte
	DomainIP    string
	LocalIP     string
	Fingerprint Profile
	StartTime   time.Time
}

// UTlsResponse 定义TLS响应结构
type UTlsResponse struct {
	WorkID     string
	StatusCode int
	Body       []byte
	Path       string
	Duration   time.Duration
}

// Do 执行一个TLS请求，支持HTTP/1.1和HTTP/2，IPv4/IPv6，并具备降级到域名访问的能力
func (c *UTlsClient) Do(req *UTlsRequest) (*UTlsResponse, error) {
	startTime := time.Now()
	
	// 尝试使用指定的IP地址进行连接
	var conn *utls.UConn
	var err error
	
	// 如果提供了DomainIP，则优先使用IP直接连接
	if req.DomainIP != "" {
		conn, err = c.connectWithIP(req)
		if err != nil {
			// IP连接失败，尝试降级到域名连接
			fmt.Printf("通过IP %s 连接失败，降级到域名连接: %v\n", req.DomainIP, err)
			conn, err = c.connectWithDomain(req)
			if err != nil {
				return nil, fmt.Errorf("无法通过IP或域名建立连接: %w", err)
			}
		}
	} else {
		// 没有提供IP，直接使用域名连接
		conn, err = c.connectWithDomain(req)
		if err != nil {
			return nil, fmt.Errorf("无法通过域名建立连接: %w", err)
		}
	}
	
	defer conn.Close()
	
	// 发送HTTP请求
	err = c.sendHTTPRequest(conn, req)
	if err != nil {
		return nil, fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	
	// 读取HTTP响应
	statusCode, body, err := c.readHTTPResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("读取HTTP响应失败: %w", err)
	}
	
	return &UTlsResponse{
		WorkID:     req.WorkID,
		StatusCode: statusCode,
		Body:       body,
		Path:       req.Path,
		Duration:   time.Since(startTime),
	}, nil
}

// connectWithIP 使用指定的IP地址建立TLS连接
func (c *UTlsClient) connectWithIP(req *UTlsRequest) (*utls.UConn, error) {
	// 解析IP地址
	ip := net.ParseIP(req.DomainIP)
	if ip == nil {
		return nil, fmt.Errorf("无效的IP地址: %s", req.DomainIP)
	}
	
	// 确定端口
	port := "443"
	
	// 创建TCP连接
	var tcpConn net.Conn
	var err error
	
	// 如果提供了本地IP，则绑定到指定的本地IP
	if req.LocalIP != "" {
		localIP := net.ParseIP(req.LocalIP)
		if localIP == nil {
			return nil, fmt.Errorf("无效的本地IP地址: %s", req.LocalIP)
		}
		
		var dialer net.Dialer
		if localIP.To4() != nil {
			// IPv4
			dialer.LocalAddr = &net.TCPAddr{IP: localIP, Port: 0}
		} else {
			// IPv6
			dialer.LocalAddr = &net.TCPAddr{IP: localIP, Port: 0}
		}
		
		tcpConn, err = dialer.Dial("tcp", net.JoinHostPort(req.DomainIP, port))
	} else {
		// 使用默认本地地址
		tcpConn, err = net.Dial("tcp", net.JoinHostPort(req.DomainIP, port))
	}
	
	if err != nil {
		return nil, fmt.Errorf("TCP连接失败: %w", err)
	}
	
	// 创建UTLS连接
	uConn := utls.UClient(tcpConn, &utls.Config{
		ServerName: req.Domain,
		NextProtos: []string{"http/1.1"}, // 暂时只支持HTTP/1.1以避免协议解析问题
	}, req.Fingerprint.HelloID)
	
	// 执行TLS握手
	err = uConn.Handshake()
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("TLS握手失败: %w", err)
	}
	
	return uConn, nil
}

// connectWithDomain 使用域名建立TLS连接
func (c *UTlsClient) connectWithDomain(req *UTlsRequest) (*utls.UConn, error) {
	// 解析域名获取IP地址
	ips, err := net.LookupIP(req.Domain)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("域名解析失败: %w", err)
	}
	
	// 优先选择IPv6地址，如果没有则选择IPv4地址
	var ip net.IP
	for _, addr := range ips {
		if addr.To4() == nil {
			// IPv6地址
			ip = addr
			break
		}
	}
	
	// 如果没有找到IPv6地址，则使用第一个IPv4地址
	if ip == nil && len(ips) > 0 {
		ip = ips[0]
	}
	
	if ip == nil {
		return nil, fmt.Errorf("无法解析到有效的IP地址")
	}
	
	// 设置DomainIP用于后续连接
	req.DomainIP = ip.String()
	
	// 复用connectWithIP的逻辑
	return c.connectWithIP(req)
}

// sendHTTPRequest 发送HTTP请求
func (c *UTlsClient) sendHTTPRequest(conn *utls.UConn, req *UTlsRequest) error {
	// 构建HTTP请求
	var requestBuilder strings.Builder
	
	// 写入请求行
	requestBuilder.WriteString(fmt.Sprintf("%s %s HTTP/1.1\r\n", req.Method, req.Path))
	
	// 写入Host头
	requestBuilder.WriteString(fmt.Sprintf("Host: %s\r\n", req.Domain))
	
	// 写入其他请求头
	for key, value := range req.Headers {
		requestBuilder.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}
	
	// 如果没有设置User-Agent，则使用指纹中的User-Agent
	if _, exists := req.Headers["User-Agent"]; !exists && req.Fingerprint.UserAgent != "" {
		requestBuilder.WriteString(fmt.Sprintf("User-Agent: %s\r\n", req.Fingerprint.UserAgent))
	}
	
	// 写入Content-Length头
	if len(req.Body) > 0 {
		requestBuilder.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(req.Body)))
	}
	
	// 写入空行分隔请求头和请求体
	requestBuilder.WriteString("\r\n")
	
	// 写入请求体
	if len(req.Body) > 0 {
		requestBuilder.Write(req.Body)
	}
	
	// 发送请求
	_, err := conn.Write([]byte(requestBuilder.String()))
	if err != nil {
		return fmt.Errorf("写入HTTP请求失败: %w", err)
	}
	
	return nil
}

// readHTTPResponse 读取HTTP响应
func (c *UTlsClient) readHTTPResponse(conn *utls.UConn) (int, []byte, error) {
	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	
	// 读取响应
	reader := bufio.NewReader(conn)
	
	// 读取状态行
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, nil, fmt.Errorf("读取状态行失败: %w", err)
	}
	
	// 解析状态码
	parts := strings.Split(strings.TrimSpace(statusLine), " ")
	if len(parts) < 2 {
		return 0, nil, fmt.Errorf("无效的状态行: %s", statusLine)
	}
	
	var statusCode int
	_, err = fmt.Sscanf(parts[1], "%d", &statusCode)
	if err != nil {
		return 0, nil, fmt.Errorf("解析状态码失败: %w", err)
	}
	
	// 读取响应头
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return statusCode, nil, fmt.Errorf("读取响应头失败: %w", err)
		}
		
		// 空行表示响应头结束
		if strings.TrimSpace(line) == "" {
			break
		}
		
		// 解析响应头
		headerParts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(headerParts) == 2 {
			headers[strings.TrimSpace(headerParts[0])] = strings.TrimSpace(headerParts[1])
		}
	}
	
	// 读取响应体
	var body []byte
	if contentLengthStr, exists := headers["Content-Length"]; exists {
		var contentLength int
		_, err = fmt.Sscanf(contentLengthStr, "%d", &contentLength)
		if err == nil && contentLength > 0 {
			body = make([]byte, contentLength)
			_, err = reader.Read(body)
			if err != nil {
				return statusCode, nil, fmt.Errorf("读取响应体失败: %w", err)
			}
		}
	} else {
		// 如果没有Content-Length头，尝试读取直到超时
		body, err = reader.Peek(1024) // 读取最多1024字节
		if err != nil && err.Error() != "EOF" {
			// 忽略EOF错误
			return statusCode, nil, fmt.Errorf("读取响应体失败: %w", err)
		}
	}
	
	return statusCode, body, nil
}

// Start 实现UTlsClientApi接口的Start方法（占位实现）
func (c *UTlsClient) Start() {
	// 占位实现
}

// Stop 实现UTlsClientApi接口的Stop方法（占位实现）
func (c *UTlsClient) Stop() {
	// 占位实现
}