package src

import (
	"fmt"
	"net"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

// HotConnPool 定义热连接池接口
type HotConnPool interface {
	// GetConn 从连接池获取一个可用连接
	GetConn() (*utls.UConn, error)
	// ReturnConn 将连接返回到连接池
	ReturnConn(conn *utls.UConn, statusCode int) error
	// Close 关闭连接池并释放所有资源
	Close() error
}

// ConnStatus 连接状态
type ConnStatus int

const (
	// StatusUnknown 未知状态
	StatusUnknown ConnStatus = iota
	// StatusHealthy 健康状态(200)
	StatusHealthy
	// StatusUnhealthy 不健康状态(403或其他错误)
	StatusUnhealthy
)

// ipConnPool 表示基于IP的连接池
type ipConnPool struct {
	// 连接池相关字段
	healthyConns   chan *utls.UConn // 健康连接通道
	unhealthyConns chan *utls.UConn // 不健康连接通道
	
	// IP管理相关字段
	ipPool         IPPool           // IP地址池
	accessControl  IPAccessController // IP访问控制器
	fingerprint    Profile          // TLS指纹配置
	
	// 控制字段
	mutex      sync.RWMutex     // 读写锁保护连接池
	stopChan   chan struct{}    // 停止信号通道
	wg         sync.WaitGroup   // 等待组用于等待goroutine结束
	maxConns   int              // 最大连接数
	idleTime   time.Duration    // 连接空闲超时时间
	domain     string           // 目标域名
	port       string           // 目标端口
}

// ConnPoolConfig 定义连接池配置参数
type ConnPoolConfig struct {
	IPPool         IPPool             // IP地址池
	AccessControl  IPAccessController // IP访问控制器
	Fingerprint    Profile            // TLS指纹配置
	Domain         string             // 目标域名
	Port           string             // 目标端口，默认443
	MaxConns       int                // 最大连接数，默认为10
	IdleTimeout    time.Duration      // 连接空闲超时时间，默认为5分钟
}

// NewUtlsHotConnPool 创建并初始化一个新的utls热连接池
func NewUtlsHotConnPool(config ConnPoolConfig) (HotConnPool, error) {
	// 设置默认值
	if config.Port == "" {
		config.Port = "443"
	}
	if config.MaxConns <= 0 {
		config.MaxConns = 10
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	
	// 创建连接池实例
	pool := &ipConnPool{
		healthyConns:   make(chan *utls.UConn, config.MaxConns),
		unhealthyConns: make(chan *utls.UConn, config.MaxConns),
		ipPool:         config.IPPool,
		accessControl:  config.AccessControl,
		fingerprint:    config.Fingerprint,
		stopChan:       make(chan struct{}),
		maxConns:       config.MaxConns,
		idleTime:       config.IdleTimeout,
		domain:         config.Domain,
		port:           config.Port,
	}
	
	// 启动后台任务
	pool.startBackgroundTasks()
	
	return pool, nil
}

// GetConn 从连接池获取一个可用连接
func (p *ipConnPool) GetConn() (*utls.UConn, error) {
	// 首先尝试从健康连接池获取
	select {
	case conn := <-p.healthyConns:
		// 检查连接是否仍然有效
		if conn != nil && conn.ConnectionState().HandshakeComplete {
			return conn, nil
		}
		// 连接无效，关闭它
		if conn != nil {
			conn.Close()
		}
	default:
		// 健康连接池为空，尝试从不健康连接池获取
		select {
		case conn := <-p.unhealthyConns:
			// 检查连接是否仍然有效
			if conn != nil && conn.ConnectionState().HandshakeComplete {
				return conn, nil
			}
			// 连接无效，关闭它
			if conn != nil {
				conn.Close()
			}
		default:
			// 所有连接池都为空，创建新连接
			return p.createConn()
		}
	}
	
	// 如果上面没有返回，创建新连接
	return p.createConn()
}

// ReturnConn 将连接返回到连接池，并根据状态码更新IP状态
func (p *ipConnPool) ReturnConn(conn *utls.UConn, statusCode int) error {
	if conn == nil {
		return nil
	}
	
	// 检查连接是否仍然有效
	if !conn.ConnectionState().HandshakeComplete {
		conn.Close()
		return nil
	}
	
	// 根据状态码处理连接和IP
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		conn.Close()
		return fmt.Errorf("无法解析远程地址: %w", err)
	}
	
	switch {
	case statusCode == 200:
		// 状态码为200，将IP加入白名单，并将连接放入健康连接池
		p.accessControl.AddIP(host, true)
		
		// 尝试将连接放回健康连接池
		select {
		case p.healthyConns <- conn:
			// 成功放回
		default:
			// 连接池已满，关闭连接
			conn.Close()
		}
	case statusCode == 403:
		// 状态码为403，将IP加入黑名单，并将连接放入不健康连接池
		p.accessControl.AddIP(host, false)
		
		// 尝试将连接放回不健康连接池
		select {
		case p.unhealthyConns <- conn:
			// 成功放回
		default:
			// 连接池已满，关闭连接
			conn.Close()
		}
	default:
		// 其他状态码，暂时将连接放入不健康连接池
		select {
		case p.unhealthyConns <- conn:
			// 成功放回
		default:
			// 连接池已满，关闭连接
			conn.Close()
		}
	}
	
	return nil
}

// Close 关闭连接池并释放所有资源
func (p *ipConnPool) Close() error {
	close(p.stopChan)
	p.wg.Wait()
	
	// 关闭所有连接
	close(p.healthyConns)
	for conn := range p.healthyConns {
		if conn != nil {
			conn.Close()
		}
	}
	
	close(p.unhealthyConns)
	for conn := range p.unhealthyConns {
		if conn != nil {
			conn.Close()
		}
	}
	
	return nil
}

// createConn 创建一个新的utls连接
func (p *ipConnPool) createConn() (*utls.UConn, error) {
	// 获取IP地址
	ip := p.ipPool.GetIP()
	if ip == nil {
		return nil, fmt.Errorf("无法从IP池获取IP地址")
	}
	
	// 创建TCP连接
	tcpConn, err := net.Dial("tcp", net.JoinHostPort(ip.String(), p.port))
	if err != nil {
		// 连接失败，将IP加入黑名单
		p.accessControl.AddIP(ip.String(), false)
		return nil, fmt.Errorf("无法连接到 %s: %w", ip.String(), err)
	}
	
	// 创建utls连接
	uConn := utls.UClient(tcpConn, &utls.Config{
		ServerName: p.domain,
	}, p.fingerprint.HelloID)
	
	// 执行握手
	if err := uConn.Handshake(); err != nil {
		tcpConn.Close()
		// 握手失败，将IP加入黑名单
		p.accessControl.AddIP(ip.String(), false)
		return nil, fmt.Errorf("TLS握手失败: %w", err)
	}
	
	return uConn, nil
}

// startBackgroundTasks 启动后台任务
func (p *ipConnPool) startBackgroundTasks() {
	p.wg.Add(1)
	go p.retryUnhealthyIPs()
}

// retryUnhealthyIPs 定期重试不健康的IP
func (p *ipConnPool) retryUnhealthyIPs() {
	defer p.wg.Done()
	
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟重试一次
	defer ticker.Stop()
	
	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.retryIPs()
		}
	}
}

// retryIPs 重试不健康的IP
func (p *ipConnPool) retryIPs() {
	// 这里应该实现重试逻辑
	// 为了简化，我们只记录日志
	fmt.Println("正在重试不健康的IP...")
}