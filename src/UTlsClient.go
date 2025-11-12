package src

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// UTlsClientApi 定义UTlsClient的接口
type UTlsClientApi interface {
	Do(req *UTlsRequest) (*UTlsResponse, error)
}

// UTlsClient 定义UTLS客户端结构体
type UTlsClient struct {
	ReadTimeout time.Duration
	DialTimeout time.Duration
	MaxRetries  int
	HotConnPool HotConnPool
}

// NewUTlsClient 创建并初始化一个新的UTLS客户端
func NewUTlsClient() *UTlsClient {
	return &UTlsClient{
		ReadTimeout: 30 * time.Second,
		DialTimeout: 10 * time.Second,
		MaxRetries:  0,
	}
}

func (c *UTlsClient) getReadTimeout() time.Duration {
	if c.ReadTimeout > 0 {
		return c.ReadTimeout
	}
	return 30 * time.Second
}

func (c *UTlsClient) getDialTimeout() time.Duration {
	if c.DialTimeout > 0 {
		return c.DialTimeout
	}
	return 10 * time.Second
}

type connInfo struct {
	conn       net.Conn
	httpClient *http.Client // For HTTP/2
	protocol   string
	isHTTPS    bool
	isIPv6     bool
}

func formatIPAddress(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}
	if ip.To4() == nil {
		return "[" + ipStr + "]"
	}
	return ipStr
}

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
	Timeout     time.Duration
}

type UTlsResponse struct {
	WorkID     string
	StatusCode int
	Body       []byte
	Path       string
	Duration   time.Duration
	LocalIP    string
}

func (c *UTlsClient) Do(req *UTlsRequest) (*UTlsResponse, error) {
	startTime := time.Now()
	isHTTPS := strings.HasPrefix(strings.ToLower(req.Path), "https://")

	maxRetries := 3
	for retry := 0; retry <= maxRetries; retry++ {
		var connMeta *ConnMetadata
		var err error
		var usePool bool

		if c.HotConnPool != nil && isHTTPS {
			if req.DomainIP != "" {
				connMeta, err = c.HotConnPool.GetConnByIP(req.DomainIP)
				if err != nil {
					if retry < maxRetries {
						time.Sleep(10 * time.Millisecond)
						continue
					}
					return nil, fmt.Errorf("无法从连接池获取匹配IP %s 的连接: %w", req.DomainIP, err)
				}
			} else {
				connMeta, err = c.HotConnPool.GetConn()
				if err != nil {
					if retry < maxRetries {
						continue
					}
					return nil, fmt.Errorf("无法从连接池获取连接: %w", err)
				}
			}
			usePool = true
		} else {
			// Fallback to creating a new connection if pool is not used or not HTTPS
			var port string
			if isHTTPS {
				port = "443"
			} else {
				port = "80"
			}

			connInfo, err := c.createConnection(req, isHTTPS, port)
			if err != nil {
				if retry < maxRetries {
					continue
				}
				return nil, fmt.Errorf("无法建立连接: %w", err)
			}
			// This path is simplified, assuming pool is always used for HTTPS
			// For non-pooled connections, we'd need to wrap it in ConnMetadata
			// and handle its lifecycle. For now, we focus on the pooled path.
			defer connInfo.conn.Close()
			// This part of logic needs to be aligned with the new ConnMetadata structure if used.
		}

		var statusCode int
		var body []byte
		var localIP string
		var cancel context.CancelFunc
		timeout := req.Timeout

		if connMeta.HttpClient != nil { // HTTP/2 path
			ctx := context.Background()
			if timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, timeout)
			}
			statusCode, body, err = c.sendHTTP2Request(ctx, connMeta.HttpClient, req)
		} else { // HTTP/1.1 path
			if timeout > 0 && connMeta.Conn != nil {
				_ = connMeta.Conn.SetDeadline(time.Now().Add(timeout))
				defer func() { _ = connMeta.Conn.SetDeadline(time.Time{}) }()
			}
			err = c.sendHTTPRequest(connMeta.Conn, req)
			if err == nil {
				statusCode, body, err = c.readHTTPResponse(connMeta.Conn)
			}
		}

		if cancel != nil {
			cancel()
		}

		if connMeta != nil {
			if connMeta.LocalIP != "" {
				localIP = connMeta.LocalIP
			} else if connMeta.Conn != nil {
				if addr := connMeta.Conn.LocalAddr(); addr != nil {
					if tcpAddr, ok := addr.(*net.TCPAddr); ok && tcpAddr.IP != nil {
						localIP = tcpAddr.IP.String()
					} else {
						localIP = addr.String()
					}
				}
			}
		}

		if err != nil {
			isConnError := isConnectivityError(err)
			if usePool && c.HotConnPool != nil {
				if isConnError {
					// The pool will handle closing the connection.
					// We just need to signal that it was an error.
					_ = c.HotConnPool.ReturnConn(connMeta, 0)
				} else {
					_ = c.HotConnPool.ReturnConn(connMeta, 0)
				}
			}
			if retry < maxRetries && isConnError {
				fmt.Printf("[UTlsClient] 连接池连接失效 (%v)，重试...\n", err)
				continue
			}
			return nil, fmt.Errorf("请求执行失败: %w", err)
		}

		if usePool && c.HotConnPool != nil {
			_ = c.HotConnPool.ReturnConn(connMeta, statusCode)
		}

		if !usePool && c.HotConnPool != nil && statusCode > 0 {
			c.HotConnPool.UpdateIPStats(req.DomainIP, statusCode)
		}

		return &UTlsResponse{
			WorkID:     req.WorkID,
			StatusCode: statusCode,
			Body:       body,
			Path:       req.Path,
			Duration:   time.Since(startTime),
			LocalIP:    localIP,
		}, nil
	}

	return nil, fmt.Errorf("请求失败：超过最大重试次数")
}

// sendHTTP2Request uses a pre-configured http.Client to send a request.
func (c *UTlsClient) sendHTTP2Request(ctx context.Context, client *http.Client, req *UTlsRequest) (int, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return 0, nil, fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	httpReq.Host = req.Domain
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	if _, exists := req.Headers["User-Agent"]; !exists {
		if req.Fingerprint.UserAgent != "" {
			httpReq.Header.Set("User-Agent", req.Fingerprint.UserAgent)
		} else {
			randomFingerprint := fpLibrary.RandomProfile()
			if randomFingerprint.UserAgent != "" {
				httpReq.Header.Set("User-Agent", randomFingerprint.UserAgent)
			}
		}
	}

	if _, exists := req.Headers["Accept-Language"]; !exists {
		acceptLanguage := fpLibrary.RandomAcceptLanguage()
		httpReq.Header.Set("Accept-Language", acceptLanguage)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("发送HTTP/2请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取HTTP/2响应体失败: %w", err)
	}

	return resp.StatusCode, body, nil
}

func (c *UTlsClient) createConnection(req *UTlsRequest, isHTTPS bool, port string) (*connInfo, error) {
	// This function needs to be updated to return a ConnMetadata-like structure
	// including the http.Client for H2 connections.
	// The implementation is simplified for now.
	ip := net.ParseIP(req.DomainIP)
	if ip == nil {
		return nil, fmt.Errorf("无效的IP地址: %s", req.DomainIP)
	}

	dialer := net.Dialer{
		Timeout: c.getDialTimeout(),
	}
	// ... localIP binding logic ...

	tcpConn, err := dialer.Dial("tcp", net.JoinHostPort(req.DomainIP, port))
	if err != nil {
		return nil, fmt.Errorf("TCP连接失败: %w", err)
	}

	if isHTTPS {
		helloID := req.Fingerprint.HelloID
		if req.Fingerprint.Name == "" {
			helloID = fpLibrary.RandomProfile().HelloID
		}

		uConn := utls.UClient(tcpConn, &utls.Config{
			ServerName:         req.Domain,
			NextProtos:         []string{"h2", "http/1.1"},
			InsecureSkipVerify: false,
		}, helloID)

		if err := uConn.Handshake(); err != nil {
			_ = uConn.Close()
			return nil, fmt.Errorf("TLS握手失败: %w", err)
		}

		state := uConn.ConnectionState()
		protocol := state.NegotiatedProtocol
		if protocol == "" {
			protocol = "http/1.1"
		}

		var httpClient *http.Client
		if protocol == "h2" {
			transport := &http2.Transport{
				DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
					// The connection is already established, just return it.
					return uConn, nil
				},
			}
			httpClient = &http.Client{Transport: transport}
		}

		return &connInfo{
			conn:       uConn,
			httpClient: httpClient,
			protocol:   protocol,
			isHTTPS:    true,
			isIPv6:     ip.To4() == nil,
		}, nil
	}

	return &connInfo{conn: tcpConn, protocol: "http/1.1"}, nil
}

func (c *UTlsClient) sendHTTPRequest(conn net.Conn, req *UTlsRequest) error {
	httpReq, err := http.NewRequest(req.Method, req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	httpReq.Host = req.Domain
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}
	// ... header setting logic ...

	return httpReq.Write(conn)
}

func (c *UTlsClient) readHTTPResponse(conn net.Conn) (int, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(c.getReadTimeout()))
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("读取HTTP响应失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	// ... 1xx handling logic ...

	return resp.StatusCode, body, nil
}

func isConnectivityError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "closed network connection") ||
		strings.Contains(errStr, "FRAME_SIZE_ERROR")
}
