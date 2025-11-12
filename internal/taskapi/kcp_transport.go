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

// DefaultKCPConfig 返回默认KCP配置（高性能模式）
func DefaultKCPConfig() *KCPConfig {
	return &KCPConfig{
		DataShard:    10,
		ParityShard: 3,
		NoDelay:      1,      // 启用快速模式
		Interval:     5,      // 降低到5ms，减少延迟（原10ms）
		Resend:       2,      // 快速重传
		NoCongestion: 1,      // 关闭流控，提高吞吐量
		RxMinRto:     10,
		MTU:          1400,   // 增加到1400，提高吞吐量（原1350）
		SndWnd:       2048,   // 增加到2048，提高并发性能（原1024）
		RcvWnd:       2048,   // 增加到2048，提高并发性能（原1024）
	}
}

// kcpListener KCP监听器包装
type kcpListener struct {
	*kcp.Listener
	config *KCPConfig // 保存配置以便在Accept时使用
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

	return &kcpListener{
		Listener: listener,
		config:   config, // 保存配置
	}, nil
}

// Accept 接受连接并配置KCP参数
func (l *kcpListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.AcceptKCP()
	if err != nil {
		return nil, err
	}

	// 使用保存的配置
	config := l.config
	if config == nil {
		config = DefaultKCPConfig()
	}
	
	// 配置KCP会话参数（使用配置中的参数）
	conn.SetNoDelay(config.NoDelay, config.Interval, config.Resend, config.NoCongestion)
	conn.SetWindowSize(config.SndWnd, config.RcvWnd)
	conn.SetMtu(config.MTU)
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
		// 如果解析失败，尝试手动处理IPv6地址格式（如 "2607:8700:5500:2943::2:9091"）
		// 查找最后一个冒号，尝试将其作为端口分隔符
		lastColonIndex := strings.LastIndex(address, ":")
		if lastColonIndex > 0 && lastColonIndex < len(address)-1 {
			// 检查最后一个冒号后面的部分是否是数字（端口）
			possiblePort := address[lastColonIndex+1:]
			possibleHost := address[:lastColonIndex]
			
			// 尝试解析端口号
			var portNum int
			if _, err := fmt.Sscanf(possiblePort, "%d", &portNum); err == nil && portNum > 0 && portNum <= 65535 {
				// 可能是端口号，检查前面的部分是否是IPv6地址
				ip := net.ParseIP(possibleHost)
				if ip != nil && ip.To4() == nil && ip.To16() != nil {
					// 是IPv6地址，使用方括号包裹
					return fmt.Sprintf("[%s]:%s", possibleHost, possiblePort)
				}
			}
		}
		// 如果无法解析，直接返回原地址
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

