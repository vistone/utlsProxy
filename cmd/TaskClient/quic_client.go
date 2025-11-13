package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/quic-go/quic-go"

	"utlsProxy/internal/taskapi"
)

// QUICTransport QUIC传输实现
type QUICTransport struct {
	address      string
	tlsConfig    *tls.Config
	quicConfig   *quic.Config
	sessionPool  []*quic.Conn
	poolMutex    sync.RWMutex
	maxSessions  int
	currentIndex int64
	closed       int32
}

// NewQUICTransport 创建新的QUIC传输实例
func NewQUICTransport(address string, tlsConfig *tls.Config, quicConfig *quic.Config, maxSessions int) (*QUICTransport, error) {
	if maxSessions <= 0 {
		maxSessions = 4 // 默认4个会话
	}

	transport := &QUICTransport{
		address:     address,
		tlsConfig:   tlsConfig,
		quicConfig:  quicConfig,
		sessionPool: make([]*quic.Conn, 0, maxSessions),
		maxSessions: maxSessions,
	}

	// 预创建第一个会话
	if err := transport.ensureSession(); err != nil {
		return nil, fmt.Errorf("创建初始会话失败: %w", err)
	}

	return transport, nil
}

// ensureSession 确保至少有一个可用的会话
func (t *QUICTransport) ensureSession() error {
	t.poolMutex.Lock()
	defer t.poolMutex.Unlock()

	// 清理已关闭的会话
	t.cleanupClosedSessions()

	// 如果还有可用会话，直接返回
	if len(t.sessionPool) > 0 {
		return nil
	}

	// 创建新会话
	conn, err := quic.DialAddr(context.Background(), t.address, t.tlsConfig, t.quicConfig)
	if err != nil {
		return fmt.Errorf("建立QUIC连接失败: %w", err)
	}

	t.sessionPool = append(t.sessionPool, conn)
	log.Printf("[QUIC] 创建新会话，当前会话数: %d", len(t.sessionPool))
	return nil
}

// cleanupClosedSessions 清理已关闭的会话
func (t *QUICTransport) cleanupClosedSessions() {
	validSessions := make([]*quic.Conn, 0, len(t.sessionPool))
	for _, conn := range t.sessionPool {
		if conn.Context().Err() == nil {
			validSessions = append(validSessions, conn)
		}
	}
	t.sessionPool = validSessions
}

// getSession 获取一个可用的会话（轮询方式）
func (t *QUICTransport) getSession() (*quic.Conn, error) {
	t.poolMutex.RLock()
	poolSize := len(t.sessionPool)
	t.poolMutex.RUnlock()

	if poolSize == 0 {
		if err := t.ensureSession(); err != nil {
			return nil, err
		}
		t.poolMutex.RLock()
		poolSize = len(t.sessionPool)
		t.poolMutex.RUnlock()
	}

	if poolSize == 0 {
		return nil, fmt.Errorf("无法获取可用会话")
	}

	// 轮询选择会话
	index := int(atomic.AddInt64(&t.currentIndex, 1) % int64(poolSize))
	t.poolMutex.RLock()
	defer t.poolMutex.RUnlock()

	if index < len(t.sessionPool) {
		conn := t.sessionPool[index]
		// 检查会话是否仍然有效
		if conn.Context().Err() == nil {
			return conn, nil
		}
	}

	// 如果选中的会话无效，尝试其他会话
	for i := 0; i < len(t.sessionPool); i++ {
		conn := t.sessionPool[(index+i)%len(t.sessionPool)]
		if conn.Context().Err() == nil {
			return conn, nil
		}
	}

	// 所有会话都无效，尝试创建新会话
	t.poolMutex.RUnlock()
	if err := t.ensureSession(); err != nil {
		return nil, err
	}
	t.poolMutex.RLock()

	if len(t.sessionPool) > 0 {
		return t.sessionPool[0], nil
	}

	return nil, fmt.Errorf("无法获取可用会话")
}

// Execute 执行任务请求
func (t *QUICTransport) Execute(ctx context.Context, req *taskapi.TaskRequest) (*taskapi.TaskResponse, error) {
	if atomic.LoadInt32(&t.closed) == 1 {
		return nil, fmt.Errorf("传输已关闭")
	}

	// 获取会话
	conn, err := t.getSession()
	if err != nil {
		return nil, fmt.Errorf("获取会话失败: %w", err)
	}

	// 打开新的stream
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		// 如果打开stream失败，可能是会话已关闭，尝试重新获取
		if conn.Context().Err() != nil {
			t.poolMutex.Lock()
			t.cleanupClosedSessions()
			t.poolMutex.Unlock()
			// 重试一次
			conn, err = t.getSession()
			if err != nil {
				return nil, fmt.Errorf("重新获取会话失败: %w", err)
			}
			stream, err = conn.OpenStreamSync(ctx)
			if err != nil {
				return nil, fmt.Errorf("打开stream失败: %w", err)
			}
		} else {
			return nil, fmt.Errorf("打开stream失败: %w", err)
		}
	}
	defer func() { _ = stream.Close() }()

	// 序列化请求
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 发送请求（长度前缀 + 数据）
	if err := t.writePayload(stream, reqData); err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	// 读取响应
	respData, err := t.readPayload(stream)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 反序列化响应
	var resp taskapi.TaskResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("反序列化响应失败: %w", err)
	}

	return &resp, nil
}

// writePayload 写入带长度前缀的数据
func (t *QUICTransport) writePayload(stream *quic.Stream, data []byte) error {
	writer := bufio.NewWriter(stream)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))

	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	return writer.Flush()
}

// readPayload 读取带长度前缀的数据
func (t *QUICTransport) readPayload(stream *quic.Stream) ([]byte, error) {
	reader := bufio.NewReader(stream)

	var lengthBuf [4]byte
	if _, err := io.ReadFull(reader, lengthBuf[:]); err != nil {
		return nil, err
	}

	payloadLen := binary.BigEndian.Uint32(lengthBuf[:])
	if payloadLen == 0 {
		return nil, fmt.Errorf("响应长度为0")
	}
	if payloadLen > 50*1024*1024 { // 50MB限制
		return nil, fmt.Errorf("响应过大: %d 字节", payloadLen)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

// IsReady 检查传输是否就绪
func (t *QUICTransport) IsReady() bool {
	if atomic.LoadInt32(&t.closed) == 1 {
		return false
	}

	t.poolMutex.RLock()
	defer t.poolMutex.RUnlock()

	// 检查是否有至少一个有效会话
	for _, conn := range t.sessionPool {
		if conn.Context().Err() == nil {
			return true
		}
	}

	return false
}

// Close 关闭传输连接
func (t *QUICTransport) Close() error {
	if !atomic.CompareAndSwapInt32(&t.closed, 0, 1) {
		return nil // 已经关闭
	}

	t.poolMutex.Lock()
	defer t.poolMutex.Unlock()

	var lastErr error
	for _, conn := range t.sessionPool {
		if err := conn.CloseWithError(0, "client closing"); err != nil {
			lastErr = err
		}
	}
	t.sessionPool = nil

	return lastErr
}

// buildQUICClientTLSConfig 构建QUIC客户端TLS配置
func buildQUICClientTLSConfig(serverName string, caFile string, insecureSkipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: insecureSkipVerify,
		MinVersion:         tls.VersionTLS13,
	}

	if caFile != "" {
		certPool, err := loadCertPool(caFile)
		if err != nil {
			return nil, fmt.Errorf("加载CA证书失败: %w", err)
		}
		tlsConfig.RootCAs = certPool
	}

	return tlsConfig, nil
}

// loadCertPool 加载CA证书池
func loadCertPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 CA 文件失败: %w", err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(data); !ok {
		return nil, fmt.Errorf("解析 CA 证书失败: %s", path)
	}
	return pool, nil
}

