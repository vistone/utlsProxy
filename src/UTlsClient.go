package src // 定义包名为src

import ( // 导入标准库和第三方库
	"bufio"    // 用于缓冲IO操作
	"fmt"      // 用于格式化输出
	"io"       // 用于IO操作
	"net"      // 用于网络操作
	"net/http" // 用于HTTP协议操作
	"strings"  // 用于字符串操作
	"time"     // 用于时间操作

	utls "github.com/refraction-networking/utls" // UTLS库，用于TLS指纹伪装
	"golang.org/x/net/http2"                     // HTTP/2协议支持
)

// UTlsClientApi 定义UTlsClient的接口
type UTlsClientApi interface { // 定义UTlsClient的接口类型
	Do(req *UTlsRequest) (*UTlsResponse, error) // Do方法：执行请求并返回响应
}

// UTlsClient 定义UTLS客户端结构体
type UTlsClient struct { // 定义UTLS客户端结构体，实现UTlsClientApi接口
	ReadTimeout time.Duration // 读取超时时间，默认30秒
	DialTimeout time.Duration // 连接超时时间，默认10秒
	MaxRetries  int           // 最大重试次数，默认0（不重试）
}

// NewUTlsClient 创建并初始化一个新的UTLS客户端
func NewUTlsClient() *UTlsClient { // NewUTlsClient函数：创建UTLS客户端实例，返回客户端指针
	return &UTlsClient{ // 返回初始化的客户端实例
		ReadTimeout: 30 * time.Second, // 设置默认读取超时为30秒
		DialTimeout: 10 * time.Second, // 设置默认连接超时为10秒
		MaxRetries:  0,                // 设置默认最大重试次数为0（不重试）
	}
}

// getReadTimeout 获取读取超时时间，如果未设置则返回默认值
func (c *UTlsClient) getReadTimeout() time.Duration { // getReadTimeout方法：获取读取超时时间，返回超时时间
	if c.ReadTimeout > 0 { // 如果设置了读取超时时间
		return c.ReadTimeout // 返回设置的超时时间
	}
	return 30 * time.Second // 返回默认超时时间30秒
}

// getDialTimeout 获取连接超时时间，如果未设置则返回默认值
func (c *UTlsClient) getDialTimeout() time.Duration { // getDialTimeout方法：获取连接超时时间，返回超时时间
	if c.DialTimeout > 0 { // 如果设置了连接超时时间
		return c.DialTimeout // 返回设置的超时时间
	}
	return 10 * time.Second // 返回默认超时时间10秒
}

// connInfo 保存连接信息和协议类型
type connInfo struct { // 定义连接信息结构体
	conn     net.Conn // 网络连接对象
	protocol string   // 协议类型："h2" 或 "http/1.1"
	isHTTPS  bool     // 是否为HTTPS连接
	isIPv6   bool     // 是否为IPv6连接
}

// formatIPAddress 格式化IP地址，IPv6地址添加方括号
func formatIPAddress(ipStr string) string { // 格式化IP地址函数，参数为IP字符串，返回格式化后的字符串
	ip := net.ParseIP(ipStr) // 解析IP字符串为IP对象
	if ip == nil {           // 如果解析失败
		return ipStr // 返回原字符串
	}
	if ip.To4() == nil { // 判断是否为IPv6地址（To4()返回nil表示是IPv6）
		return "[" + ipStr + "]" // IPv6地址添加方括号并返回
	}
	return ipStr // IPv4地址直接返回
}

// UTlsRequest 定义TLS请求结构
type UTlsRequest struct { // 定义UTLS请求结构体
	WorkID      string            // 工作ID，用于标识请求
	Domain      string            // 目标域名
	Method      string            // HTTP请求方法（GET、POST等）
	Path        string            // 请求路径（完整URL）
	Headers     map[string]string // HTTP请求头映射
	Body        []byte            // 请求体内容
	DomainIP    string            // 目标域名的IP地址
	LocalIP     string            // 本地绑定的IP地址
	Fingerprint Profile           // TLS指纹配置
	StartTime   time.Time         // 请求开始时间
}

// UTlsResponse 定义TLS响应结构
type UTlsResponse struct { // 定义UTLS响应结构体
	WorkID     string        // 工作ID，与请求对应
	StatusCode int           // HTTP状态码
	Body       []byte        // 响应体内容
	Path       string        // 请求路径
	Duration   time.Duration // 请求耗时
}

// Do 执行一个请求，支持HTTP和HTTPS，IPv4/IPv6，并具备降级到域名访问的能力
func (c *UTlsClient) Do(req *UTlsRequest) (*UTlsResponse, error) { // Do方法：执行UTLS请求，返回响应和错误
	startTime := time.Now() // 记录请求开始时间

	isHTTPS := strings.HasPrefix(strings.ToLower(req.Path), "https://") // 根据路径判断是否使用HTTPS协议

	var port string // 声明端口变量
	if isHTTPS {    // 如果是HTTPS请求
		port = "443" // 设置HTTPS默认端口443
	} else { // 如果是HTTP请求
		port = "80" // 设置HTTP默认端口80
	}

	var connInfo *connInfo // 声明连接信息变量
	var err error          // 声明错误变量

	if req.DomainIP != "" { // 如果提供了目标IP地址
		connInfo, err = c.connectWithIP(req, isHTTPS, port) // 尝试使用IP地址建立连接
		if err != nil {                                     // 如果IP连接失败
			formattedIP := formatIPAddress(req.DomainIP)               // 格式化IP地址用于日志输出
			fmt.Printf("通过IP %s 连接失败，降级到域名连接: %v\n", formattedIP, err) // 输出降级日志
			connInfo, err = c.connectWithDomain(req, isHTTPS, port)    // 降级到使用域名连接
			if err != nil {                                            // 如果域名连接也失败
				return nil, fmt.Errorf("无法通过IP或域名建立连接: %w", err) // 返回连接失败错误
			}
		}
	} else { // 如果没有提供IP地址
		connInfo, err = c.connectWithDomain(req, isHTTPS, port) // 直接使用域名建立连接
		if err != nil {                                         // 如果连接失败
			return nil, fmt.Errorf("无法通过域名建立连接: %w", err) // 返回连接失败错误
		}
	}

	defer connInfo.conn.Close() // 延迟关闭连接，确保函数返回时关闭

	var statusCode int                        // 声明状态码变量
	var body []byte                           // 声明响应体变量
	if connInfo.protocol == "h2" && isHTTPS { // 如果协商的协议是HTTP/2且是HTTPS连接
		statusCode, body, err = c.sendHTTP2Request(connInfo.conn, req) // 使用HTTP/2协议发送请求
		if err != nil {                                                // 如果发送失败
			return nil, fmt.Errorf("发送HTTP/2请求失败: %w", err) // 返回发送失败错误
		}
	} else { // 如果使用HTTP/1.1协议
		err = c.sendHTTPRequest(connInfo.conn, req) // 发送HTTP/1.1请求
		if err != nil {                             // 如果发送失败
			return nil, fmt.Errorf("发送HTTP请求失败: %w", err) // 返回发送失败错误
		}

		statusCode, body, err = c.readHTTPResponse(connInfo.conn) // 读取HTTP响应
		if err != nil {                                           // 如果读取失败
			return nil, fmt.Errorf("读取HTTP响应失败: %w", err) // 返回读取失败错误
		}
	}

	return &UTlsResponse{ // 返回响应对象
		WorkID:     req.WorkID,            // 设置工作ID
		StatusCode: statusCode,            // 设置状态码
		Body:       body,                  // 设置响应体
		Path:       req.Path,              // 设置请求路径
		Duration:   time.Since(startTime), // 计算请求耗时
	}, nil // 返回nil错误表示成功
}

// sendHTTP2Request 使用HTTP/2协议发送请求
func (c *UTlsClient) sendHTTP2Request(conn net.Conn, req *UTlsRequest) (int, []byte, error) { // sendHTTP2Request方法：通过HTTP/2协议发送请求，返回状态码、响应体和错误
	conn.SetReadDeadline(time.Now().Add(c.getReadTimeout())) // 设置连接读取超时，使用客户端配置的超时时间

	transport := &http2.Transport{} // 创建HTTP/2传输对象

	clientConn, err := transport.NewClientConn(conn) // 使用已建立的连接创建HTTP/2客户端连接
	if err != nil {                                  // 如果创建连接失败
		return 0, nil, fmt.Errorf("创建HTTP/2客户端连接失败: %w", err) // 返回创建失败错误
	}

	httpReq, err := http.NewRequest(req.Method, req.Path, strings.NewReader(string(req.Body))) // 构建HTTP请求对象
	if err != nil {                                                                            // 如果创建请求失败
		clientConn.Close()                               // 关闭客户端连接
		return 0, nil, fmt.Errorf("创建HTTP请求失败: %w", err) // 返回创建失败错误
	}

	httpReq.Host = req.Domain             // 设置请求的Host头为域名
	for key, value := range req.Headers { // 遍历请求头映射
		httpReq.Header.Set(key, value) // 设置每个请求头
	}

	if _, exists := req.Headers["User-Agent"]; !exists { // 如果请求头中没有User-Agent
		if req.Fingerprint.UserAgent != "" { // 如果指纹配置中有User-Agent
			httpReq.Header.Set("User-Agent", req.Fingerprint.UserAgent) // 使用指纹中的User-Agent
		} else { // 如果指纹中也没有User-Agent
			randomFingerprint := fpLibrary.RandomProfile() // 随机选择一个指纹配置
			if randomFingerprint.UserAgent != "" {         // 如果随机指纹有User-Agent
				httpReq.Header.Set("User-Agent", randomFingerprint.UserAgent) // 使用随机指纹的User-Agent
			}
		}
	}

	if _, exists := req.Headers["Accept-Language"]; !exists { // 如果请求头中没有Accept-Language
		acceptLanguage := fpLibrary.RandomAcceptLanguage()    // 随机选择一个Accept-Language
		httpReq.Header.Set("Accept-Language", acceptLanguage) // 设置Accept-Language请求头
	}

	resp, err := clientConn.RoundTrip(httpReq) // 发送HTTP/2请求并获取响应
	if err != nil {                            // 如果发送失败
		clientConn.Close()                                 // 关闭客户端连接
		return 0, nil, fmt.Errorf("发送HTTP/2请求失败: %w", err) // 返回发送失败错误
	}
	defer resp.Body.Close() // 延迟关闭响应体

	body, err := io.ReadAll(resp.Body) // 读取响应体的所有内容
	if err != nil {                    // 如果读取失败
		return resp.StatusCode, nil, fmt.Errorf("读取HTTP/2响应体失败: %w", err) // 返回读取失败错误
	}

	return resp.StatusCode, body, nil // 返回状态码、响应体和nil错误
}

// connectWithIP 使用指定的IP地址建立连接
func (c *UTlsClient) connectWithIP(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error) { // connectWithIP方法：通过IP地址建立连接，返回连接信息和错误
	ip := net.ParseIP(req.DomainIP) // 解析IP地址字符串为IP对象
	if ip == nil {                  // 如果解析失败
		return nil, fmt.Errorf("无效的IP地址: %s", req.DomainIP) // 返回无效IP地址错误
	}

	isIPv6 := ip.To4() == nil // 判断是否为IPv6地址（To4()返回nil表示是IPv6）

	var tcpConn net.Conn // 声明TCP连接变量
	var err error        // 声明错误变量

	dialer := net.Dialer{ // 创建拨号器对象
		Timeout: c.getDialTimeout(), // 设置连接超时时间
	}

	if req.LocalIP != "" { // 如果提供了本地IP地址
		localIP := net.ParseIP(req.LocalIP) // 解析本地IP地址字符串为IP对象
		if localIP == nil {                 // 如果解析失败
			return nil, fmt.Errorf("无效的本地IP地址: %s", req.LocalIP) // 返回无效本地IP地址错误
		}

		if localIP.To4() != nil { // 如果本地IP是IPv4地址
			dialer.LocalAddr = &net.TCPAddr{IP: localIP, Port: 0} // 设置IPv4本地地址（端口0表示自动分配）
		} else { // 如果本地IP是IPv6地址
			dialer.LocalAddr = &net.TCPAddr{IP: localIP, Port: 0} // 设置IPv6本地地址（端口0表示自动分配）
		}
	}

	tcpConn, err = dialer.Dial("tcp", net.JoinHostPort(req.DomainIP, port)) // 使用拨号器建立TCP连接

	if err != nil { // 如果TCP连接失败
		return nil, fmt.Errorf("TCP连接失败: %w", err) // 返回TCP连接失败错误
	}

	if isHTTPS { // 如果是HTTPS请求
		uConn := utls.UClient(tcpConn, &utls.Config{ // 创建UTLS客户端连接，使用TLS指纹伪装
			ServerName:         req.Domain,                 // 设置服务器名称（SNI）
			NextProtos:         []string{"h2", "http/1.1"}, // 设置支持的协议列表，优先HTTP/2，降级到HTTP/1.1
			InsecureSkipVerify: false,                      // 不跳过证书验证
		}, req.Fingerprint.HelloID) // 使用请求中的TLS指纹HelloID

		err = uConn.Handshake() // 执行TLS握手
		if err != nil {         // 如果握手失败
			tcpConn.Close()                            // 关闭TCP连接
			return nil, fmt.Errorf("TLS握手失败: %w", err) // 返回TLS握手失败错误
		}

		state := uConn.ConnectionState()               // 获取TLS连接状态
		negotiatedProtocol := state.NegotiatedProtocol // 获取协商后的协议类型

		if negotiatedProtocol == "" { // 如果没有协商到协议
			negotiatedProtocol = "http/1.1" // 默认使用HTTP/1.1协议
		}

		return &connInfo{ // 返回连接信息对象
			conn:     uConn,              // 设置UTLS连接
			protocol: negotiatedProtocol, // 设置协商的协议
			isHTTPS:  true,               // 标记为HTTPS连接
			isIPv6:   isIPv6,             // 设置IPv6标志
		}, nil // 返回nil错误表示成功
	}

	return &connInfo{ // 返回连接信息对象（HTTP请求）
		conn:     tcpConn,    // 设置TCP连接
		protocol: "http/1.1", // 设置协议为HTTP/1.1
		isHTTPS:  false,      // 标记为非HTTPS连接
		isIPv6:   isIPv6,     // 设置IPv6标志
	}, nil // 返回nil错误表示成功
}

// connectWithDomain 使用域名建立连接
func (c *UTlsClient) connectWithDomain(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error) { // connectWithDomain方法：通过域名建立连接，返回连接信息和错误
	ips, err := net.LookupIP(req.Domain) // 解析域名获取所有IP地址
	if err != nil || len(ips) == 0 {     // 如果解析失败或没有IP地址
		return nil, fmt.Errorf("域名解析失败: %w", err) // 返回域名解析失败错误
	}

	var ip net.IP              // 声明IP变量
	for _, addr := range ips { // 遍历所有解析到的IP地址
		if addr.To4() == nil { // 如果当前地址是IPv6地址
			ip = addr // 选择IPv6地址
			break     // 跳出循环
		}
	}

	if ip == nil && len(ips) > 0 { // 如果没有找到IPv6地址且存在IP地址
		ip = ips[0] // 使用第一个IPv4地址
	}

	if ip == nil { // 如果仍然没有有效的IP地址
		return nil, fmt.Errorf("无法解析到有效的IP地址") // 返回无法解析IP地址错误
	}

	req.DomainIP = ip.String() // 将解析到的IP地址设置到请求的DomainIP字段

	return c.connectWithIP(req, isHTTPS, port) // 复用connectWithIP方法的逻辑建立连接
}

// sendHTTPRequest 发送HTTP请求
func (c *UTlsClient) sendHTTPRequest(conn net.Conn, req *UTlsRequest) error { // sendHTTPRequest方法：发送HTTP/1.1请求，返回错误
	httpReq, err := http.NewRequest(req.Method, req.Path, strings.NewReader(string(req.Body))) // 构建HTTP请求对象
	if err != nil {                                                                            // 如果创建请求失败
		return fmt.Errorf("创建HTTP请求失败: %w", err) // 返回创建失败错误
	}

	httpReq.Host = req.Domain             // 设置请求的Host头为域名
	for key, value := range req.Headers { // 遍历请求头映射
		httpReq.Header.Set(key, value) // 设置每个请求头
	}

	if _, exists := req.Headers["User-Agent"]; !exists { // 如果请求头中没有User-Agent
		if req.Fingerprint.UserAgent != "" { // 如果指纹配置中有User-Agent
			httpReq.Header.Set("User-Agent", req.Fingerprint.UserAgent) // 使用指纹中的User-Agent
		} else { // 如果指纹中也没有User-Agent
			randomFingerprint := fpLibrary.RandomProfile() // 随机选择一个指纹配置
			if randomFingerprint.UserAgent != "" {         // 如果随机指纹有User-Agent
				httpReq.Header.Set("User-Agent", randomFingerprint.UserAgent) // 使用随机指纹的User-Agent
			}
		}
	}

	if _, exists := req.Headers["Accept-Language"]; !exists { // 如果请求头中没有Accept-Language
		acceptLanguage := fpLibrary.RandomAcceptLanguage()    // 随机选择一个Accept-Language
		httpReq.Header.Set("Accept-Language", acceptLanguage) // 设置Accept-Language请求头
	}

	err = httpReq.Write(conn) // 将HTTP请求写入连接
	if err != nil {           // 如果写入失败
		return fmt.Errorf("写入HTTP请求失败: %w", err) // 返回写入失败错误
	}

	return nil // 返回nil错误表示成功
}

// readHTTPResponse 读取HTTP响应
func (c *UTlsClient) readHTTPResponse(conn net.Conn) (int, []byte, error) { // readHTTPResponse方法：读取HTTP响应，返回状态码、响应体和错误
	conn.SetReadDeadline(time.Now().Add(c.getReadTimeout())) // 设置连接读取超时，使用客户端配置的超时时间

	reader := bufio.NewReader(conn)             // 创建缓冲读取器
	resp, err := http.ReadResponse(reader, nil) // 读取HTTP响应
	if err != nil {                             // 如果读取失败
		return 0, nil, fmt.Errorf("读取HTTP响应失败: %w", err) // 返回读取失败错误
	}
	defer resp.Body.Close() // 延迟关闭响应体

	body := new(strings.Builder)                      // 创建字符串构建器用于存储响应体
	_, err = bufio.NewReader(resp.Body).WriteTo(body) // 将响应体内容写入字符串构建器
	if err != nil {                                   // 如果读取失败
		return resp.StatusCode, nil, fmt.Errorf("读取响应体失败: %w", err) // 返回读取失败错误
	}

	for resp.StatusCode >= 100 && resp.StatusCode < 200 { // 检查是否是信息性响应(1xx状态码)
		resp, err = http.ReadResponse(reader, nil) // 继续读取下一个响应
		if err != nil {                            // 如果读取失败
			return resp.StatusCode, []byte(body.String()), fmt.Errorf("读取最终HTTP响应失败: %w", err) // 返回读取失败错误
		}
		defer resp.Body.Close() // 延迟关闭响应体

		body.Reset()                                      // 重置字符串构建器
		_, err = bufio.NewReader(resp.Body).WriteTo(body) // 将新的响应体内容写入字符串构建器
		if err != nil {                                   // 如果读取失败
			return resp.StatusCode, []byte(body.String()), fmt.Errorf("读取最终响应体失败: %w", err) // 返回读取失败错误
		}
	}

	return resp.StatusCode, []byte(body.String()), nil // 返回状态码、响应体字节数组和nil错误
}
