package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"

	"utlsProxy/internal/taskapi"
)

const (
	maxQUICRequestSize = 16 * 1024 * 1024 // 16MB
)

func (c *Crawler) startQUICServer() error {
	if c.config == nil {
		return fmt.Errorf("配置未初始化: config 为 nil")
	}
	if !c.config.ServerConfig.EnableQUIC {
		return nil
	}
	if c.config.ServerConfig.QUICPort == 0 {
		return fmt.Errorf("QUIC 端口未配置: QUICPort 为 0")
	}

	tlsConfig, err := c.buildQUICServerTLSConfig()
	if err != nil {
		return fmt.Errorf("构建 QUIC TLS 配置失败: %w", err)
	}

	address := fmt.Sprintf(":%d", c.config.ServerConfig.QUICPort)
	quicConfig := &quic.Config{
		KeepAlivePeriod: 5 * time.Second,
		MaxIdleTimeout:  c.config.ServerConfig.GetQUICMaxIdleTimeout(),
		EnableDatagrams: false,
	}

	listener, err := quic.ListenAddr(address, tlsConfig, quicConfig)
	if err != nil {
		return fmt.Errorf("监听 QUIC 端口失败: %w", err)
	}

	c.quicListener = listener
	log.Printf("任务 QUIC 服务启动，地址 %s", address)

	c.wg.Add(1)
	go c.acceptQUICConnections(listener)

	return nil
}

func (c *Crawler) buildQUICServerTLSConfig() (*tls.Config, error) {
	var certificate tls.Certificate
	var err error

	serverCfg := c.config.ServerConfig

	if serverCfg.QUICCertFile != "" && serverCfg.QUICKeyFile != "" {
		certificate, err = tls.LoadX509KeyPair(serverCfg.QUICCertFile, serverCfg.QUICKeyFile)
		if err != nil {
			return nil, fmt.Errorf("加载 QUIC TLS 证书失败: %w", err)
		}
	} else {
		certificate, err = generateSelfSignedCertificate()
		if err != nil {
			return nil, fmt.Errorf("生成自签名证书失败: %w", err)
		}
		log.Printf("[QUIC] 未配置证书，已生成临时自签名证书供测试使用")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{serverCfg.QUICALPN},
		MinVersion:   tls.VersionTLS13,
	}

	if serverCfg.QUICCAFile != "" {
		certPool, err := loadCertPool(serverCfg.QUICCAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	}

	return tlsConfig, nil
}

func generateSelfSignedCertificate() (tls.Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("生成私钥失败: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("生成序列号失败: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "utlsProxy QUIC",
			Organization: []string{"utlsProxy"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("生成证书失败: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("编码私钥失败: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})

	return tls.X509KeyPair(certPEM, keyPEM)
}

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

func (c *Crawler) acceptQUICConnections(listener *quic.Listener) {
	defer c.wg.Done()

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			if isListenerClosedErr(err) || atomic.LoadInt32(&c.stopped) == 1 {
				return
			}
			log.Printf("[QUIC] 接受连接失败: %v", err)
			continue
		}

		atomic.AddInt64(&c.stats.QUICSessions, 1)
		c.wg.Add(1)
		go c.handleQUICConnection(conn)
	}
}

func (c *Crawler) handleQUICConnection(conn *quic.Conn) {
	defer c.wg.Done()
	label := "[QUIC]"

	for {
		stream, err := conn.AcceptStream(conn.Context())
		if err != nil {
			if errors.Is(err, context.Canceled) || isConnectionClosedErr(err) || conn.Context().Err() != nil {
				return
			}
			log.Printf("%s 接受流失败: %v (remote=%s)", label, err, conn.RemoteAddr())
			return
		}

		atomic.AddInt64(&c.stats.QUICStreams, 1)
		c.wg.Add(1)
		go c.handleQUICStream(conn, stream)
	}
}

func (c *Crawler) handleQUICStream(conn *quic.Conn, stream *quic.Stream) {
	defer c.wg.Done()
	defer func() { _ = stream.Close() }()

	label := "[QUIC]"
	start := time.Now()
	reader := bufio.NewReader(stream)

	var lengthBuf [4]byte
	if _, err := io.ReadFull(reader, lengthBuf[:]); err != nil {
		c.writeQUICError(stream, label, "读取请求长度失败", err, start, 0)
		return
	}

	payloadLen := binary.BigEndian.Uint32(lengthBuf[:])
	if payloadLen == 0 {
		c.writeQUICError(stream, label, "请求负载为空", nil, start, 0)
		return
	}
	if payloadLen > maxQUICRequestSize {
		c.writeQUICError(stream, label, fmt.Sprintf("请求体过大（%d 字节）", payloadLen), nil, start, int64(payloadLen))
		return
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(reader, payload); err != nil {
		c.writeQUICError(stream, label, "读取请求体失败", err, start, int64(payloadLen))
		return
	}

	var req taskapi.TaskRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		c.writeQUICError(stream, label, "请求体解码失败", err, start, int64(payloadLen))
		return
	}

	ctx, cancel := context.WithTimeout(conn.Context(), maxTaskDuration)
	defer cancel()

	resp, err := c.executeTask(ctx, transportQUIC, &req, int64(payloadLen))
	if err != nil && (resp == nil || resp.ErrorMessage == "") {
		if resp == nil {
			resp = &taskapi.TaskResponse{ClientID: req.ClientID}
		}
		resp.ErrorMessage = err.Error()
	}

	responsePayload, err := json.Marshal(resp)
	if err != nil {
		c.writeQUICError(stream, label, "响应编码失败", err, start, int64(payloadLen))
		return
	}

	if err := c.writeQUICPayload(stream, responsePayload); err != nil {
		log.Printf("%s 发送响应失败: %v", label, err)
		return
	}
}

func (c *Crawler) writeQUICPayload(stream *quic.Stream, payload []byte) error {
	writer := bufio.NewWriter(stream)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

func (c *Crawler) writeQUICError(stream *quic.Stream, label, message string, err error, start time.Time, requestBytes int64) {
	metrics := c.metricsForTransport(transportQUIC)
	fullMsg := message
	if err != nil {
		fullMsg = fmt.Sprintf("%s: %v", message, err)
	}
	log.Printf("%s %s", label, fullMsg)

	if metrics.requests != nil {
		atomic.AddInt64(metrics.requests, 1)
	}
	if metrics.failed != nil {
		atomic.AddInt64(metrics.failed, 1)
	}
	if metrics.requestBytes != nil && requestBytes > 0 {
		atomic.AddInt64(metrics.requestBytes, requestBytes)
	}

	resp := &taskapi.TaskResponse{
		ErrorMessage: fullMsg,
	}

	responsePayload, marshalErr := json.Marshal(resp)
	if marshalErr == nil {
		if metrics.responseBytes != nil {
			atomic.AddInt64(metrics.responseBytes, int64(len(responsePayload)))
		}
		if err := c.writeQUICPayload(stream, responsePayload); err != nil {
			log.Printf("%s 发送错误响应失败: %v", label, err)
		}
	} else {
		log.Printf("%s 编码错误响应失败: %v", label, marshalErr)
	}

	c.addTransportDuration(metrics.duration, start)
}

func isListenerClosedErr(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

func isConnectionClosedErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}
	if nerr, ok := err.(net.Error); ok && !nerr.Timeout() {
		return true
	}
	return false
}
