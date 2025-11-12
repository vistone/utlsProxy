package taskapi

import (
	"context"
	"fmt"
	"net"
	"strings"

	kcp "github.com/xtaci/kcp-go/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// KCPConfig KCP配置
type KCPConfig struct {
	// 数据窗口大小（发送窗口）
	DataShard   int
	// 校验窗口大小（接收窗口）
	ParityShard int
	// 是否启用快速重传
	NoDelay     int
	// 快速重传触发阈值
	Interval    int
	// 快速重传触发次数
	Resend      int
	// 是否关闭流控
	NoCongestion int
	// 最大接收窗口大小
	RxMinRto    uint32
	// MTU大小
	MTU         int
	// 发送窗口大小
	SndWnd      int
	// 接收窗口大小
	RcvWnd      int
}

// DefaultKCPConfig 返回默认KCP配置
func DefaultKCPConfig() *KCPConfig {
	return &KCPConfig{
		DataShard:    10,
		ParityShard: 3,
		NoDelay:      1,
		Interval:     10,
		Resend:       2,
		NoCongestion: 1,
		RxMinRto:     10,
		MTU:          1350,
		SndWnd:       1024,
		RcvWnd:       1024,
	}
}

// kcpListener KCP监听器包装
type kcpListener struct {
	*kcp.Listener
}

// kcpConn KCP连接包装
type kcpConn struct {
	*kcp.UDPSession
}

// NewKCPListener 创建KCP监听器
func NewKCPListener(address string, config *KCPConfig) (net.Listener, error) {
	if config == nil {
		config = DefaultKCPConfig()
	}

	listener, err := kcp.ListenWithOptions(address, nil, config.DataShard, config.ParityShard)
	if err != nil {
		return nil, err
	}

	// 配置KCP监听器参数
	if err := listener.SetReadBuffer(4194304); err != nil { // 4MB
		return nil, err
	}
	if err := listener.SetWriteBuffer(4194304); err != nil { // 4MB
		return nil, err
	}

	return &kcpListener{Listener: listener}, nil
}

// Accept 接受连接并配置KCP参数
func (l *kcpListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.AcceptKCP()
	if err != nil {
		return nil, err
	}

	// 配置KCP会话参数（使用默认配置）
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetWindowSize(1024, 1024)
	conn.SetMtu(1350)
	conn.SetReadBuffer(4194304)  // 4MB
	conn.SetWriteBuffer(4194304) // 4MB

	return &kcpConn{UDPSession: conn}, nil
}

// NewKCPDialer 创建KCP拨号器
func NewKCPDialer(config *KCPConfig) func(context.Context, string) (net.Conn, error) {
	if config == nil {
		config = DefaultKCPConfig()
	}

	return func(ctx context.Context, address string) (net.Conn, error) {
		// 格式化地址，确保IPv6地址使用方括号
		formattedAddr := formatKCPAddress(address)
		conn, err := kcp.DialWithOptions(formattedAddr, nil, config.DataShard, config.ParityShard)
		if err != nil {
			return nil, err
		}

		// 配置KCP会话参数
		conn.SetNoDelay(config.NoDelay, config.Interval, config.Resend, config.NoCongestion)
		conn.SetWindowSize(config.SndWnd, config.RcvWnd)
		conn.SetMtu(config.MTU)
		conn.SetReadBuffer(4194304)  // 4MB
		conn.SetWriteBuffer(4194304) // 4MB

		return &kcpConn{UDPSession: conn}, nil
	}
}

// formatKCPAddress 格式化KCP地址，确保IPv6地址使用方括号包裹
func formatKCPAddress(address string) string {
	// 如果地址已经包含方括号，直接返回
	if strings.Contains(address, "[") && strings.Contains(address, "]") {
		return address
	}
	
	// 尝试解析地址，检查是否是IPv6地址
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// 如果解析失败，可能是格式不正确，直接返回原地址
		return address
	}
	
	// 解析IP地址
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() == nil && ip.To16() != nil {
		// 是IPv6地址，使用方括号包裹
		return fmt.Sprintf("[%s]:%s", host, port)
	}
	
	// IPv4地址或域名，直接返回
	return address
}

// DialKCP 使用KCP协议连接gRPC服务器
func DialKCP(address string, kcpConfig *KCPConfig, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// 格式化地址，确保IPv6地址使用方括号
	formattedAddr := formatKCPAddress(address)
	
	base := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec)),
		grpc.WithContextDialer(NewKCPDialer(kcpConfig)),
	}
	base = append(base, opts...)
	return grpc.Dial(formattedAddr, base...)
}

// NewServerKCP 创建使用KCP传输的gRPC服务器
func NewServerKCP(kcpConfig *KCPConfig, opts ...grpc.ServerOption) (*grpc.Server, func(string) (net.Listener, error)) {
	base := []grpc.ServerOption{
		grpc.ForceServerCodec(JSONCodec),
	}
	base = append(base, opts...)
	
	server := grpc.NewServer(base...)
	
	listenerFactory := func(address string) (net.Listener, error) {
		return NewKCPListener(address, kcpConfig)
	}
	
	return server, listenerFactory
}

