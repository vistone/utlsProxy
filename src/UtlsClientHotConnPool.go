package src // 定义src包

import ( // 导入所需的标准库和第三方库
	"fmt"       // 用于格式化输入输出
	"math/rand" // 用于随机数生成
	"net"       // 用于网络操作
	"sync"      // 用于同步原语
	"time"      // 用于时间处理

	utls "github.com/refraction-networking/utls" // UTLS库，用于TLS指纹伪装
)

// HotConnPool 定义热连接池接口
type HotConnPool interface {
	// GetConn 从连接池获取一个可用连接
	GetConn() (*utls.UConn, error)
	// ReturnConn 将连接返回到连接池
	ReturnConn(conn *utls.UConn, statusCode int) error
	// UpdateIPStats 更新IP统计信息（不返回连接到池中，只更新统计）
	UpdateIPStats(targetIP string, statusCode int)
	// Close 关闭连接池并释放所有资源
	Close() error
	// Warmup 预热连接池
	Warmup() error
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

// connMetadata 连接元数据，包含连接及其相关信息
type connMetadata struct {
	conn      *utls.UConn // UTLS连接
	targetIP  string      // 目标IP地址
	localIP   string      // 本地绑定IP地址
	createdAt time.Time   // 连接创建时间
	lastUsed  time.Time   // 最后使用时间
}

// ipStats IP统计信息
type ipStats struct {
	SuccessCount int64 // 成功请求次数（状态码200）
	FailureCount int64 // 失败请求次数（状态码非200，包括403等）
}

// domainConnPool 表示基于域名的连接池实现
type domainConnPool struct {
	// 连接池相关字段（使用连接元数据）
	healthyConns   chan *connMetadata // 健康连接通道
	unhealthyConns chan *connMetadata // 不健康连接通道

	// 依赖组件
	domainMonitor   DomainMonitor      // 域名IP监控器
	ipAccessControl IPAccessController // IP访问控制器（黑白名单）
	fingerprint     Profile            // TLS指纹配置

	// 本地IP池（出站IP绑定）
	localIPv4Pool  IPPool // IPv4本地IP池（备用）
	localIPv6Pool  IPPool // IPv6本地IP池（优先，可为nil）
	hasIPv6Support bool   // 是否支持IPv6

	// 目标IP管理（从DomainMonitor获取）
	targetIPv6List []string // 目标IPv6地址列表（优先）
	targetIPv4List []string // 目标IPv4地址列表（备用）
	ipListMutex    sync.RWMutex

	// 控制字段
	mutex        sync.RWMutex // 保护连接池操作
	cleanupMutex sync.Mutex   // 保护清理操作，避免并发清理
	stopChan     chan struct{}
	wg           sync.WaitGroup
	closed       bool // 连接池是否已关闭

	// 配置
	maxConns          int
	idleTime          time.Duration
	domain            string
	port              string
	warmupPath        string
	warmupMethod      string
	warmupHeaders     map[string]string
	warmupConcurrency int

	// 定时器
	blacklistTestInterval time.Duration
	ipRefreshInterval     time.Duration

	// UTlsClient用于健康检查
	healthCheckClient *UTlsClient

	// 随机数生成器
	rand *rand.Rand

	// IP统计信息（线程安全）
	ipStatsMap   map[string]*ipStats // IP地址 -> 统计信息
	ipStatsMutex sync.RWMutex        // 保护ipStatsMap的读写锁

	// TLS会话缓存（用于连接复用和会话恢复）
	sessionCache utls.ClientSessionCache // TLS客户端会话缓存（utls库的接口）
}

// DomainConnPoolConfig 定义基于域名的连接池配置参数
type DomainConnPoolConfig struct {
	DomainMonitor         DomainMonitor      // 域名IP监控器
	IPAccessControl       IPAccessController // IP访问控制器
	LocalIPv4Pool         IPPool             // 本地IPv4地址池（备用）
	LocalIPv6Pool         IPPool             // 本地IPv6地址池（优先，可为nil）
	Fingerprint           Profile            // TLS指纹配置
	Domain                string             // 目标域名
	Port                  string             // 目标端口，默认443
	MaxConns              int                // 最大连接数，默认为100
	IdleTimeout           time.Duration      // 连接空闲超时时间，默认为5分钟
	WarmupPath            string             // 预热测试路径
	WarmupMethod          string             // 预热请求方法，默认GET
	WarmupHeaders         map[string]string  // 预热请求头
	WarmupConcurrency     int                // 预热并发数，默认10
	BlacklistTestInterval time.Duration      // 黑名单测试间隔，默认5分钟
	IPRefreshInterval     time.Duration      // IP列表刷新间隔，默认10分钟
	DialTimeout           time.Duration      // 连接超时时间
}

// NewDomainHotConnPool 创建并初始化一个新的基于域名的热连接池
func NewDomainHotConnPool(config DomainConnPoolConfig) (HotConnPool, error) {
	// 设置默认值
	if config.Port == "" {
		config.Port = "443"
	}
	if config.MaxConns == 0 {
		config.MaxConns = 100
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.WarmupMethod == "" {
		config.WarmupMethod = "GET"
	}
	if config.WarmupConcurrency == 0 {
		config.WarmupConcurrency = 10
	}
	if config.BlacklistTestInterval == 0 {
		config.BlacklistTestInterval = 5 * time.Minute
	}
	if config.IPRefreshInterval == 0 {
		config.IPRefreshInterval = 10 * time.Minute
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 10 * time.Second
	}

	// 检测IPv6支持
	hasIPv6Support := config.LocalIPv6Pool != nil

	// 打印TLS指纹策略信息
	// 注意：每个连接都会使用独立的随机指纹，不再使用统一的指纹
	fmt.Printf("[连接池] TLS指纹策略: 每个连接使用独立的随机指纹（从真实浏览器指纹库中随机选择）\n")
	fmt.Printf("[连接池] TLS会话缓存: 已启用（支持TLS会话恢复，提高连接复用性能，特别是对HTTP/2连接）\n")

	// 创建TLS会话缓存（使用utls库的LRU缓存实现，支持最多1000个会话）
	sessionCache := utls.NewLRUClientSessionCache(1000)

	pool := &domainConnPool{
		healthyConns:          make(chan *connMetadata, config.MaxConns),
		unhealthyConns:        make(chan *connMetadata, config.MaxConns),
		domainMonitor:         config.DomainMonitor,
		ipAccessControl:       config.IPAccessControl,
		fingerprint:           config.Fingerprint,
		localIPv4Pool:         config.LocalIPv4Pool,
		localIPv6Pool:         config.LocalIPv6Pool,
		hasIPv6Support:        hasIPv6Support,
		maxConns:              config.MaxConns,
		ipStatsMap:            make(map[string]*ipStats),
		idleTime:              config.IdleTimeout,
		domain:                config.Domain,
		port:                  config.Port,
		warmupPath:            config.WarmupPath,
		warmupMethod:          config.WarmupMethod,
		warmupHeaders:         config.WarmupHeaders,
		warmupConcurrency:     config.WarmupConcurrency,
		blacklistTestInterval: config.BlacklistTestInterval,
		ipRefreshInterval:     config.IPRefreshInterval,
		stopChan:              make(chan struct{}),
		rand:                  rand.New(rand.NewSource(time.Now().UnixNano())),
		healthCheckClient:     NewUTlsClient(),
		closed:                false,
		sessionCache:          sessionCache, // TLS会话缓存
	}

	// 设置健康检查客户端的超时时间
	pool.healthCheckClient.DialTimeout = config.DialTimeout
	pool.healthCheckClient.ReadTimeout = 30 * time.Second

	// 初始化IP列表
	pool.refreshTargetIPList()

	// 启动后台任务
	pool.startBackgroundTasks()

	return pool, nil
}

// refreshTargetIPList 刷新目标IP列表（从DomainMonitor获取）
func (p *domainConnPool) refreshTargetIPList() {
	pool, found := p.domainMonitor.GetDomainPool(p.domain)
	if !found {
		fmt.Printf("[连接池] 警告: 域名 [%s] 的IP数据不存在，跳过刷新\n", p.domain)
		return // 域名数据不存在
	}

	var ipv6List, ipv4List []string

	// 提取IPv6地址
	if ipv6Records, ok := pool["ipv6"]; ok {
		for _, record := range ipv6Records {
			ipv6List = append(ipv6List, record.IP)
		}
	}

	// 提取IPv4地址
	if ipv4Records, ok := pool["ipv4"]; ok {
		for _, record := range ipv4Records {
			ipv4List = append(ipv4List, record.IP)
		}
	}

	// 更新IP列表
	p.ipListMutex.Lock()
	p.targetIPv6List = ipv6List
	p.targetIPv4List = ipv4List
	p.ipListMutex.Unlock()

	fmt.Printf("[连接池] 域名 [%s] IP列表已刷新：IPv6=%d个, IPv4=%d个\n", p.domain, len(ipv6List), len(ipv4List))
}

// filterAllowedIPs 过滤出白名单中的IP
func (p *domainConnPool) filterAllowedIPs(ipList []string) []string {
	var allowed []string
	for _, ip := range ipList {
		if p.ipAccessControl.IsIPAllowed(ip) {
			allowed = append(allowed, ip)
		}
	}
	return allowed
}

// randomSelectIP 从IP列表中随机选择一个IP
func (p *domainConnPool) randomSelectIP(ipList []string) string {
	if len(ipList) == 0 {
		return ""
	}
	return ipList[p.rand.Intn(len(ipList))]
}

// getLocalIP 获取本地绑定IP（IPv6优先）
// 返回值：(本地IP地址, 是否为IPv6)
// 如果返回空字符串，表示不绑定本地IP，让系统自动选择路由（适用于IPv6隧道模式）
func (p *domainConnPool) getLocalIP() (string, bool) {
	// 优先使用IPv6
	if p.hasIPv6Support && p.localIPv6Pool != nil {
		ip := p.localIPv6Pool.GetIP()
		if ip != nil {
			return ip.String(), true // 返回IPv6地址
		}
		// ip为nil表示隧道模式，不绑定本地IP，但返回空字符串和true表示使用IPv6
		// 这样调用方可以知道应该使用IPv6连接，但不绑定本地IP
		return "", true // 隧道模式：不绑定本地IP，但使用IPv6
	}

	// 降级到IPv4
	if p.localIPv4Pool != nil {
		ip := p.localIPv4Pool.GetIP()
		if ip != nil {
			return ip.String(), false // 返回IPv4地址
		}
	}

	return "", false // 无可用本地IP
}

// getTargetIP 获取目标IP（IPv6优先）
func (p *domainConnPool) getTargetIP(preferIPv6 bool) (string, bool) {
	p.ipListMutex.RLock()
	defer p.ipListMutex.RUnlock()

	// 优先使用IPv6
	if preferIPv6 {
		allowedIPv6 := p.filterAllowedIPs(p.targetIPv6List)
		if len(allowedIPv6) > 0 {
			return p.randomSelectIP(allowedIPv6), true
		}
	}

	// 降级到IPv4
	allowedIPv4 := p.filterAllowedIPs(p.targetIPv4List)
	if len(allowedIPv4) > 0 {
		return p.randomSelectIP(allowedIPv4), false
	}

	// 如果优先IPv6但失败，尝试IPv4
	if preferIPv6 {
		if len(allowedIPv4) > 0 {
			return p.randomSelectIP(allowedIPv4), false
		}
	}

	return "", false // 无可用目标IP
}

// createConnection 创建单个UTLS连接
// skipWhitelistCheck: 如果为true，跳过白名单检查（用于预热阶段）
func (p *domainConnPool) createConnection(localIP, targetIP string, skipWhitelistCheck bool) (*utls.UConn, error) {
	// 验证目标IP是否在白名单（预热阶段可以跳过）
	if !skipWhitelistCheck && !p.ipAccessControl.IsIPAllowed(targetIP) {
		return nil, fmt.Errorf("目标IP %s 不在白名单中", targetIP)
	}

	// 解析IP地址
	targetIPAddr := net.ParseIP(targetIP)
	if targetIPAddr == nil {
		return nil, fmt.Errorf("无效的目标IP地址: %s", targetIP)
	}

	// 创建拨号器
	dialer := net.Dialer{
		Timeout: p.healthCheckClient.DialTimeout,
	}

	// 设置本地IP绑定
	if localIP != "" {
		localIPAddr := net.ParseIP(localIP)
		if localIPAddr == nil {
			return nil, fmt.Errorf("无效的本地IP地址: %s", localIP)
		}
		dialer.LocalAddr = &net.TCPAddr{
			IP:   localIPAddr,
			Port: 0, // 自动分配端口
		}
	}

	// 建立TCP连接
	// 注意：Port设置为0时，系统会自动分配一个随机端口
	tcpConn, err := dialer.Dial("tcp", net.JoinHostPort(targetIP, p.port))
	if err != nil {
		// 错误信息中显示的是我们设置的Port值（0），实际绑定成功时系统会分配随机端口
		if localIP != "" {
			return nil, fmt.Errorf("TCP连接失败 [本地IP: %s (端口自动分配), 目标: %s:%s]: %w", localIP, targetIP, p.port, err)
		}
		return nil, fmt.Errorf("TCP连接失败 [目标: %s:%s]: %w", targetIP, p.port, err)
	}

	// 为每个连接随机选择一个TLS指纹（确保每个连接使用不同的指纹）
	randomFingerprint := GetRandomFingerprint()

	// 创建UTLS连接
	// 注意：设置OmitEmptyPsk为true，避免某些指纹（如Chrome 112_PSK）的PSK扩展导致握手失败
	// 使用ClientSessionCache支持TLS会话恢复，提高连接复用性能（特别是对HTTP/2连接）
	uConn := utls.UClient(tcpConn, &utls.Config{
		ServerName:             p.domain,
		NextProtos:             []string{"h2", "http/1.1"}, // 支持HTTP/2和HTTP/1.1
		InsecureSkipVerify:     false,
		OmitEmptyPsk:           true,           // 避免empty PSK扩展导致握手失败
		ClientSessionCache:     p.sessionCache, // 使用会话缓存支持TLS会话恢复
		SessionTicketsDisabled: false,          // 启用会话票据（支持会话恢复）
	}, randomFingerprint.HelloID)

	// 执行TLS握手
	err = uConn.Handshake()
	if err != nil {
		// 尝试读取服务器可能返回的错误信息
		var serverResponse []byte
		tcpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 1024)
		if n, readErr := tcpConn.Read(buf); readErr == nil && n > 0 {
			serverResponse = buf[:n]
		}
		tcpConn.Close()

		// 构建详细的错误信息
		errMsg := fmt.Sprintf("TLS握手失败: %v", err)
		if len(serverResponse) > 0 {
			errMsg += fmt.Sprintf(" | 服务器响应(前%d字节): %x", len(serverResponse), serverResponse)
		} else {
			errMsg += " | 服务器未返回数据（可能在握手前就失败了）"
		}
		// 注意：HelloID.Version可能是字符串类型，使用%v而不是%d
		errMsg += fmt.Sprintf(" | 使用的指纹: %s (HelloID.Client=%s, Version=%v)",
			randomFingerprint.Name, randomFingerprint.HelloID.Client, randomFingerprint.HelloID.Version)

		return nil, fmt.Errorf(errMsg)
	}

	return uConn, nil
}

// createConnectionWithFallback 创建连接（带降级策略）
// skipWhitelistCheck: 如果为true，跳过白名单检查（用于预热阶段）
func (p *domainConnPool) createConnectionWithFallback(skipWhitelistCheck bool) (*utls.UConn, string, string, error) {
	// 获取本地IP（IPv6优先）
	localIP, localIsIPv6 := p.getLocalIP()

	// 对于IPv6隧道模式，localIP可能为空但localIsIPv6为true，这是正常的
	// 只有在既没有本地IP又不是IPv6隧道模式时才报错
	if localIP == "" && !localIsIPv6 {
		return nil, "", "", fmt.Errorf("无可用的本地IP地址")
	}

	// 策略1：IPv6本地IP + IPv6目标IP（最优）
	// 或者：IPv6隧道模式（localIP为空但localIsIPv6为true）+ IPv6目标IP
	if localIsIPv6 {
		targetIP, targetIsIPv6 := p.getTargetIP(true) // 优先IPv6
		if targetIP != "" && targetIsIPv6 {
			// 对于隧道模式，localIP为空，createConnection会使用系统默认路由
			conn, err := p.createConnection(localIP, targetIP, skipWhitelistCheck)
			if err == nil {
				return conn, localIP, targetIP, nil
			}
			// 失败则继续降级
		}

		// 策略2：IPv6本地IP + IPv4目标IP（使用系统默认路由）
		targetIP, _ = p.getTargetIP(false) // 降级到IPv4
		if targetIP != "" {
			// 注意：IPv6本地IP无法直接连接IPv4目标IP，使用系统默认路由
			conn, err := p.createConnection("", targetIP, skipWhitelistCheck) // 不绑定本地IP
			if err == nil {
				return conn, "", targetIP, nil
			}
		}
	}

	// 策略3：IPv4本地IP + IPv4目标IP（降级）
	if !localIsIPv6 {
		targetIP, _ := p.getTargetIP(false) // 使用IPv4
		if targetIP != "" {
			conn, err := p.createConnection(localIP, targetIP, skipWhitelistCheck)
			if err == nil {
				return conn, localIP, targetIP, nil
			}
		}
	}

	return nil, "", "", fmt.Errorf("所有连接策略均失败")
}

// healthCheckIP 检查单个IP的健康状态（使用UTlsClient创建新连接）
func (p *domainConnPool) healthCheckIP(targetIP string) (int, error) {
	// 构建完整URL
	url := fmt.Sprintf("https://%s%s", p.domain, p.warmupPath)

	// 为每个健康检查请求随机选择一个TLS指纹（确保每个连接使用不同的指纹）
	randomFingerprint := GetRandomFingerprint()

	// 创建请求
	req := &UTlsRequest{
		WorkID:      fmt.Sprintf("health-check-%d", time.Now().UnixNano()),
		Domain:      p.domain,
		Method:      p.warmupMethod,
		Path:        url,
		Headers:     p.warmupHeaders,
		Body:        nil,
		DomainIP:    targetIP,
		Fingerprint: randomFingerprint,
		StartTime:   time.Now(),
	}

	// 执行请求
	resp, err := p.healthCheckClient.Do(req)
	if err != nil {
		return 0, err
	}

	return resp.StatusCode, nil
}

// healthCheckWithConn 使用已建立的连接进行健康检查（不关闭连接）
// 返回状态码、响应体长度和错误
func (p *domainConnPool) healthCheckWithConn(conn *utls.UConn, targetIP string) (int, int, error) {
	// 构建完整URL
	url := fmt.Sprintf("https://%s%s", p.domain, p.warmupPath)

	// 获取连接状态，判断协议类型
	state := conn.ConnectionState()
	negotiatedProtocol := state.NegotiatedProtocol
	if negotiatedProtocol == "" {
		negotiatedProtocol = "http/1.1"
	}

	// 为每个健康检查请求随机选择一个TLS指纹（确保每个连接使用不同的指纹）
	// 注意：虽然连接已经建立，但请求头中的User-Agent等仍需要使用随机指纹
	randomFingerprint := GetRandomFingerprint()

	// 创建请求对象
	req := &UTlsRequest{
		WorkID:      fmt.Sprintf("health-check-conn-%d", time.Now().UnixNano()),
		Domain:      p.domain,
		Method:      p.warmupMethod,
		Path:        url,
		Headers:     p.warmupHeaders,
		Body:        nil,
		DomainIP:    targetIP,
		Fingerprint: randomFingerprint,
		StartTime:   time.Now(),
	}

	// 根据协议类型发送请求
	if negotiatedProtocol == "h2" {
		// HTTP/2协议
		statusCode, body, err := p.healthCheckClient.sendHTTP2Request(conn, req)
		if err != nil {
			return 0, 0, err
		}
		return statusCode, len(body), nil
	} else {
		// HTTP/1.1协议
		err := p.healthCheckClient.sendHTTPRequest(conn, req)
		if err != nil {
			return 0, 0, err
		}
		statusCode, body, err := p.healthCheckClient.readHTTPResponse(conn)
		if err != nil {
			return 0, 0, err
		}
		return statusCode, len(body), nil
	}
}

// isConnValid 检查连接是否有效（未关闭）
func (p *domainConnPool) isConnValid(conn *utls.UConn) bool {
	if conn == nil {
		return false
	}
	// 检查握手是否完成
	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		return false
	}

	// 尝试获取远程地址，如果连接已关闭会返回 nil
	if remoteAddr := conn.RemoteAddr(); remoteAddr == nil {
		return false
	}

	// 注意：这里无法完全检测连接是否已被服务器关闭
	// 真正的检测在使用连接时进行（如 HTTP/2 的 NewClientConn）
	// 如果连接已关闭，会在实际使用时返回错误，然后通过重试机制处理
	return true
}

// GetConn 从连接池获取一个可用连接
func (p *domainConnPool) GetConn() (*utls.UConn, error) {
	p.mutex.RLock()
	closed := p.closed
	p.mutex.RUnlock()

	if closed {
		return nil, fmt.Errorf("连接池已关闭")
	}

	// 优先从健康连接池获取
	for {
		select {
		case connMeta := <-p.healthyConns:
			// 检查连接是否有效
			if !p.isConnValid(connMeta.conn) {
				// 连接已失效，关闭并继续尝试下一个
				fmt.Printf("[连接池] [GetConn] 健康池中的连接已失效，关闭 [目标IP: %s, 本地IP: %s]\n", connMeta.targetIP, connMeta.localIP)
				connMeta.conn.Close()
				continue
			}

			// 获取连接协议类型用于日志
			state := connMeta.conn.ConnectionState()
			protocol := state.NegotiatedProtocol
			if protocol == "" {
				protocol = "http/1.1"
			}

			// HTTP/2和HTTP/1.1连接都可以复用，不再过滤

			// 更新最后使用时间
			connMeta.lastUsed = time.Now()
			// 打印关键日志：从健康池获取连接的详细信息
			// 注意：不在这里调用printPoolStats()，因为它包含锁操作和统计计算，会影响性能
			// 统计信息会在ReturnConn时打印
			fmt.Printf("[连接池] [GetConn] 从健康池获取连接 [目标IP: %s, 本地IP: %s, 协议: %s, 连接年龄: %v]\n",
				connMeta.targetIP, connMeta.localIP, protocol, time.Since(connMeta.createdAt))
			return connMeta.conn, nil
		default:
			goto tryUnhealthy
		}
	}

tryUnhealthy:
	// 尝试从不健康连接池获取（仅用于临时错误的情况）
	for {
		select {
		case connMeta := <-p.unhealthyConns:
			// 检查连接是否有效
			if !p.isConnValid(connMeta.conn) {
				// 连接已失效，关闭并继续尝试下一个
				fmt.Printf("[连接池] [GetConn] 不健康池中的连接已失效，关闭 [目标IP: %s, 本地IP: %s]\n", connMeta.targetIP, connMeta.localIP)
				connMeta.conn.Close()
				continue
			}

			// 获取连接协议类型用于日志
			state := connMeta.conn.ConnectionState()
			protocol := state.NegotiatedProtocol
			if protocol == "" {
				protocol = "http/1.1"
			}

			// HTTP/2和HTTP/1.1连接都可以复用，不再过滤

			// 更新最后使用时间
			connMeta.lastUsed = time.Now()
			// 减少日志输出
			// fmt.Printf("[连接池] [GetConn] 从不健康池获取连接 [目标IP: %s, 本地IP: %s]\n", connMeta.targetIP, connMeta.localIP)
			// p.printPoolStats()
			return connMeta.conn, nil
		default:
			goto createNew
		}
	}

createNew:
	// 连接池为空，尝试快速创建新连接（如果白名单中有IP）
	// 注意：创建的新连接应该放入池中，以便后续复用
	localIP, _ := p.getLocalIP()
	targetIP, _ := p.getTargetIP(true) // IPv6优先

	if targetIP == "" {
		// 等待100ms后重试一次
		time.Sleep(100 * time.Millisecond)
		select {
		case connMeta := <-p.healthyConns:
			// 再次检查协议类型
			state := connMeta.conn.ConnectionState()
			protocol := state.NegotiatedProtocol
			if protocol == "" {
				protocol = "http/1.1"
			}
			// HTTP/2和HTTP/1.1连接都可以复用，不再过滤
			if !p.isConnValid(connMeta.conn) {
				connMeta.conn.Close()
				return nil, fmt.Errorf("连接池为空且等待后获取的连接无效")
			}
			connMeta.lastUsed = time.Now()
			return connMeta.conn, nil
		default:
			return nil, fmt.Errorf("连接池为空，请等待连接池预热完成")
		}
	}

	// 快速创建新连接（IP在白名单中）
	conn, err := p.createConnection(localIP, targetIP, false)
	if err != nil {
		return nil, fmt.Errorf("快速创建连接失败: %w", err)
	}

	// HTTP/2和HTTP/1.1连接都可以复用
	// 获取连接协议类型用于日志
	state := conn.ConnectionState()
	protocol := state.NegotiatedProtocol
	if protocol == "" {
		protocol = "http/1.1"
	}

	// 创建连接元数据，但不放入池中（直接返回给调用者使用）
	// 调用者使用后会通过 ReturnConn 放入池中
	fmt.Printf("[连接池] [GetConn] 快速创建新连接 [目标IP: %s, 本地IP: %s, 协议: %s]\n", targetIP, localIP, protocol)
	return conn, nil
}

// ReturnConn 将连接返回到连接池
// 注意：此方法需要知道连接的目标IP，但当前接口只接收conn和statusCode
// 为了兼容性，我们尝试从连接获取远程地址，如果失败则无法更新黑白名单
func (p *domainConnPool) ReturnConn(conn *utls.UConn, statusCode int) error {
	if conn == nil {
		return fmt.Errorf("连接不能为空")
	}

	p.mutex.RLock()
	closed := p.closed
	p.mutex.RUnlock()

	if closed {
		// 连接池已关闭，直接关闭连接
		conn.Close()
		return fmt.Errorf("连接池已关闭")
	}

	// 检查连接是否有效
	if !p.isConnValid(conn) {
		// 连接已失效，直接关闭
		conn.Close()
		return fmt.Errorf("连接已失效")
	}

	// 尝试从连接获取目标IP（用于更新黑白名单）
	// utls.UConn实现了net.Conn接口，可以直接使用RemoteAddr()
	var targetIP string
	if remoteAddr := conn.RemoteAddr(); remoteAddr != nil {
		if tcpAddr, ok := remoteAddr.(*net.TCPAddr); ok {
			targetIP = tcpAddr.IP.String()
		}
	}

	// 尝试从连接获取本地IP
	var localIP string
	if localAddr := conn.LocalAddr(); localAddr != nil {
		if tcpAddr, ok := localAddr.(*net.TCPAddr); ok {
			localIP = tcpAddr.IP.String()
		}
	}

	// 创建连接元数据
	// 注意：如果连接是从池中取出的，createdAt应该保持原值
	// 但这里无法区分，所以使用当前时间（实际使用中主要依赖lastUsed）
	now := time.Now()
	connMeta := &connMetadata{
		conn:      conn,
		targetIP:  targetIP,
		localIP:   localIP,
		createdAt: now, // 新创建的连接使用当前时间
		lastUsed:  now,
	}

	// 更新IP统计信息
	if targetIP != "" {
		p.updateIPStats(targetIP, statusCode)
	}

	// 根据状态码判断连接健康状态
	if statusCode == 200 {
		// 健康连接，加入白名单并放入健康池
		if targetIP != "" {
			wasAllowed := p.ipAccessControl.IsIPAllowed(targetIP)
			p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
			if !wasAllowed {
				fmt.Printf("[连接池] [ReturnConn] IP加入白名单 [%s]\n", targetIP)
			}
		}
		select {
		case p.healthyConns <- connMeta:
			// 打印关键日志：返回连接时输出统计信息（用于验证连接池状态）
			fmt.Printf("[连接池] [ReturnConn] 连接返回健康池 [目标IP: %s, 本地IP: %s, 状态码: %d]\n", targetIP, localIP, statusCode)
			p.printPoolStats()
			return nil
		default:
			// 健康池已满，尝试放入不健康池（保留连接，不关闭）
			select {
			case p.unhealthyConns <- connMeta:
				fmt.Printf("[连接池] [ReturnConn] 健康池已满，连接放入不健康池 [目标IP: %s, 本地IP: %s, 状态码: %d]\n", targetIP, localIP, statusCode)
				p.printPoolStats()
				return nil
			default:
				// 不健康池也满了，保留连接但不放入池（连接会被GC回收，但不主动关闭）
				fmt.Printf("[连接池] [ReturnConn] 健康池和不健康池都已满，保留连接但不放入池 [目标IP: %s, 本地IP: %s, 状态码: %d]\n", targetIP, localIP, statusCode)
				p.printPoolStats()
				return nil
			}
		}
	} else if statusCode == 403 {
		// 403错误，IP被封，从白名单移除并加入黑名单
		if targetIP != "" {
			wasAllowed := p.ipAccessControl.IsIPAllowed(targetIP)
			// 如果IP在白名单中，先从白名单移除
			if wasAllowed {
				p.ipAccessControl.RemoveIP(targetIP, true) // 从白名单移除
				fmt.Printf("[连接池] [ReturnConn] IP从白名单移除 [%s]\n", targetIP)
			}
			// 加入黑名单
			p.ipAccessControl.AddIP(targetIP, false) // 加入黑名单
			fmt.Printf("[连接池] [ReturnConn] IP加入黑名单 [%s]\n", targetIP)
		}
		// 被封的连接直接关闭，不放入池中
		fmt.Printf("[连接池] [ReturnConn] 连接被封，关闭连接 [目标IP: %s, 本地IP: %s, 状态码: %d]\n", targetIP, localIP, statusCode)
		conn.Close()
		p.printPoolStats()
		return nil
	} else {
		// 其他错误（如500、502等临时错误），放入不健康池
		// 这些连接可能只是临时故障，稍后可能恢复
		select {
		case p.unhealthyConns <- connMeta:
			// 打印关键日志：不健康连接需要记录并输出统计信息
			fmt.Printf("[连接池] [ReturnConn] 连接返回不健康池 [目标IP: %s, 本地IP: %s, 状态码: %d]\n", targetIP, localIP, statusCode)
			p.printPoolStats()
			return nil
		default:
			// 不健康池已满，保留连接但不放入池（连接会被GC回收，但不主动关闭）
			fmt.Printf("[连接池] [ReturnConn] 不健康池已满，保留连接但不放入池 [目标IP: %s, 本地IP: %s, 状态码: %d]\n", targetIP, localIP, statusCode)
			p.printPoolStats()
			return nil
		}
	}
}

// Warmup 预热连接池
// 预热阶段应该测试所有IP，而不是只测试白名单中的IP
// 因为系统启动时白名单是空的，需要通过预热来填充
func (p *domainConnPool) Warmup() error {
	fmt.Printf("[连接池] 开始预热域名 [%s]，并发数: %d\n", p.domain, p.warmupConcurrency)

	// 刷新IP列表
	p.refreshTargetIPList()

	// 获取所有目标IP（预热时不进行白名单过滤，测试所有IP）
	p.ipListMutex.RLock()
	allIPv6 := p.targetIPv6List // 直接使用所有IPv6，不进行白名单过滤
	allIPv4 := p.targetIPv4List // 直接使用所有IPv4，不进行白名单过滤
	p.ipListMutex.RUnlock()

	fmt.Printf("[连接池] 域名 [%s] 预热IP数量：IPv6=%d个, IPv4=%d个\n", p.domain, len(allIPv6), len(allIPv4))

	// 创建信号量控制并发
	semaphore := make(chan struct{}, p.warmupConcurrency)
	var wg sync.WaitGroup

	// 预热IPv6连接
	for _, ip := range allIPv6 {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(targetIP string) {
			defer wg.Done()
			defer func() { <-semaphore }()
			p.warmupSingleIP(targetIP, true) // true表示IPv6
		}(ip)
	}

	// 预热IPv4连接（作为备用）
	for _, ip := range allIPv4 {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(targetIP string) {
			defer wg.Done()
			defer func() { <-semaphore }()
			p.warmupSingleIP(targetIP, false) // false表示IPv4
		}(ip)
	}

	wg.Wait()

	// 预热完成后，打印详细的统计信息
	healthyCount := len(p.healthyConns)
	unhealthyCount := len(p.unhealthyConns)
	whitelistCount := len(p.ipAccessControl.GetAllowedIPs())
	blacklistCount := len(p.ipAccessControl.GetBlockedIPs())

	fmt.Printf("[连接池] 域名 [%s] 预热完成\n", p.domain)
	fmt.Printf("[连接池] [预热统计] 健康连接: %d, 不健康连接: %d, 白名单: %d, 黑名单: %d\n",
		healthyCount, unhealthyCount, whitelistCount, blacklistCount)

	// 如果白名单数量与健康连接数量不一致，说明有问题
	// 注意：HTTP/2连接不放入连接池是预期的行为（因为连接状态已改变，不能复用）
	// 所以如果所有连接都是HTTP/2，健康连接数量为0是正常的
	if whitelistCount != healthyCount {
		if healthyCount == 0 && whitelistCount > 0 {
			// 如果健康连接为0但白名单有IP，说明所有连接都是HTTP/2（这是正常的）
			fmt.Printf("[连接池] [信息] 白名单IP数量(%d)与健康连接数量(%d)不一致：所有连接都是HTTP/2，连接已关闭但IP已加入白名单（这是预期的行为，HTTP/2连接不能复用）\n", whitelistCount, healthyCount)
		} else if healthyCount > 0 && whitelistCount > healthyCount {
			// 如果健康连接数量小于白名单数量，可能是健康池已满或部分连接是HTTP/2
			fmt.Printf("[连接池] [警告] 白名单IP数量(%d)与健康连接数量(%d)不一致！可能原因：\n", whitelistCount, healthyCount)
			fmt.Printf("[连接池] [警告] 1. 健康池已满，部分连接被关闭但IP已加入白名单\n")
			fmt.Printf("[连接池] [警告] 2. 部分连接是HTTP/2，HTTP/2连接验证后不放入池（这是预期的行为）\n")
		}
	}

	return nil
}

// warmupSingleIP 预热单个IP
// 预热时跳过白名单检查，允许测试所有IP
func (p *domainConnPool) warmupSingleIP(targetIP string, isIPv6 bool) {
	// 统一使用getLocalIP获取本地IP（IPv6优先）
	localIP, localIsIPv6 := p.getLocalIP()

	// 对于IPv6隧道模式，localIP可能为空但localIsIPv6为true，这是正常的
	// 只有在既没有本地IP又不是IPv6隧道模式时才跳过
	if localIP == "" && !localIsIPv6 {
		fmt.Printf("[预热] 无法获取本地IP，跳过 [%s]\n", targetIP)
		return
	}

	// 如果是IPv6目标IP，但本地IP为空且不是IPv6模式，说明IPv6池不可用
	if isIPv6 && localIP == "" && !localIsIPv6 {
		fmt.Printf("[预热] IPv6池不可用，跳过IPv6目标 [%s]\n", targetIP)
		return
	}

	// 创建连接（预热时跳过白名单检查）
	conn, err := p.createConnection(localIP, targetIP, true) // skipWhitelistCheck = true
	if err != nil {
		fmt.Printf("[预热] 连接创建失败 [%s]: %v\n", targetIP, err)
		return
	}

	// 检查连接状态，判断协议类型
	state := conn.ConnectionState()
	negotiatedProtocol := state.NegotiatedProtocol
	if negotiatedProtocol == "" {
		negotiatedProtocol = "http/1.1"
	}

	// HTTP/2和HTTP/1.1连接都可以复用
	// 对于HTTP/2连接，只验证TLS握手，不发送HTTP请求（保持连接状态干净，可以复用）
	// 对于HTTP/1.1连接，使用已建立的连接进行健康检查（不关闭连接）
	if negotiatedProtocol == "h2" {
		// HTTP/2连接：只验证TLS握手，不发送HTTP请求（保持连接状态干净，可以复用）
		// 验证TLS握手成功后，直接放入连接池
		now := time.Now()
		connMeta := &connMetadata{
			conn:      conn,
			targetIP:  targetIP,
			localIP:   localIP,
			createdAt: now,
			lastUsed:  now,
		}
		select {
		case p.healthyConns <- connMeta:
			// 成功放入健康连接池，加入白名单
			p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
			fmt.Printf("[预热] 成功 [%s]: HTTP/2连接TLS握手成功 -> 连接已放入健康池，IP加入白名单（可复用）\n", targetIP)
		default:
			// 健康池已满，尝试放入不健康池
			select {
			case p.unhealthyConns <- connMeta:
				p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
				fmt.Printf("[预热] 警告 [%s]: HTTP/2连接TLS握手成功，健康池已满，连接放入不健康池，IP已加入白名单\n", targetIP)
			default:
				// 不健康池也满了，保留连接但不放入池，IP仍然加入白名单
				p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
				fmt.Printf("[预热] 警告 [%s]: HTTP/2连接TLS握手成功，健康池和不健康池都已满，保留连接但不放入池，IP已加入白名单\n", targetIP)
			}
		}
		return
	}

	// HTTP/1.1连接：使用已建立的连接进行健康检查（不关闭连接）
	// 验证返回200且body长度为13字节才是成功
	statusCode, bodyLen, err := p.healthCheckWithConn(conn, targetIP)
	if err != nil {
		// 健康检查失败，关闭连接
		conn.Close()
		fmt.Printf("[预热] 健康检查失败 [%s]: %v\n", targetIP, err)
		return
	}

	// HTTP/1.1连接：可以复用，放入连接池
	// 创建连接元数据
	now := time.Now()
	connMeta := &connMetadata{
		conn:      conn,
		targetIP:  targetIP,
		localIP:   localIP,
		createdAt: now,
		lastUsed:  now,
	}

	// 根据状态码和body长度更新黑白名单并将连接放入池中
	switch statusCode {
	case 200:
		// 验证body长度是否为13字节（PlanetoidMetadata的标准响应长度）
		if bodyLen == 13 {
			// 健康连接（200 + body长度13字节），先尝试放入健康连接池，成功后再加入白名单
			select {
			case p.healthyConns <- connMeta:
				// 成功放入健康连接池，再加入白名单
				p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
				fmt.Printf("[预热] 成功 [%s]: 200 (body=%d字节) -> 连接已放入健康池，IP加入白名单\n", targetIP, bodyLen)
			default:
				// 健康池已满，尝试放入不健康池（保留连接，不关闭）
				select {
				case p.unhealthyConns <- connMeta:
					p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
					fmt.Printf("[预热] 警告 [%s]: 200 (body=%d字节)，健康池已满，连接放入不健康池，IP已加入白名单\n", targetIP, bodyLen)
				default:
					// 不健康池也满了，保留连接但不放入池，IP仍然加入白名单
					p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
					fmt.Printf("[预热] 警告 [%s]: 200 (body=%d字节)，健康池和不健康池都已满，保留连接但不放入池，IP已加入白名单\n", targetIP, bodyLen)
				}
			}
		} else {
			// 状态码200但body长度不正确，放入不健康池
			fmt.Printf("[预热] 警告 [%s]: 200 但body长度不正确 (期望13字节，实际%d字节) -> 不健康池\n", targetIP, bodyLen)
			select {
			case p.unhealthyConns <- connMeta:
				// 成功放入不健康连接池
			default:
				// 不健康池已满，保留连接但不放入池（连接会被GC回收，但不主动关闭）
				fmt.Printf("[预热] 警告 [%s]: 200 但body长度不正确，不健康池已满，保留连接但不放入池\n", targetIP)
			}
		}
	case 403:
		// 403错误，IP被封，加入黑名单
		p.ipAccessControl.AddIP(targetIP, false) // 加入黑名单
		conn.Close()                             // 被封的连接直接关闭，不放入池中
		fmt.Printf("[预热] 失败 [%s]: 403 -> 黑名单，连接已关闭\n", targetIP)
	default:
		// 其他错误状态码，放入不健康连接池
		select {
		case p.unhealthyConns <- connMeta:
			// 成功放入不健康连接池，连接被保留用于复用
			fmt.Printf("[预热] 警告 [%s]: 状态码 %d (body=%d字节) -> 不健康池\n", targetIP, statusCode, bodyLen)
		default:
			// 不健康池已满，保留连接但不放入池（连接会被GC回收，但不主动关闭）
			fmt.Printf("[预热] 警告 [%s]: 状态码 %d，但不健康池已满，保留连接但不放入池\n", targetIP, statusCode)
		}
	}
}

// startBackgroundTasks 启动后台任务
func (p *domainConnPool) startBackgroundTasks() {
	// IP刷新任务
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.ipRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.refreshTargetIPList()
			case <-p.stopChan:
				return
			}
		}
	}()

	// 黑名单测试任务
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.blacklistTestInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.testBlacklistedIPs()
			case <-p.stopChan:
				return
			}
		}
	}()

	// 连接清理任务（只清理已失效的连接，不清理空闲连接）
	// 热连接应该长期保持，只有连接真正失效时才清理
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.cleanupIdleConns()
			case <-p.stopChan:
				return
			}
		}
	}()
}

// testBlacklistedIPs 测试黑名单中的IP
func (p *domainConnPool) testBlacklistedIPs() {
	blockedIPs := p.ipAccessControl.GetBlockedIPs()
	if len(blockedIPs) == 0 {
		return
	}

	fmt.Printf("[连接池] 开始测试 %d 个黑名单IP\n", len(blockedIPs))

	semaphore := make(chan struct{}, p.warmupConcurrency)
	var wg sync.WaitGroup

	for _, ip := range blockedIPs {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(targetIP string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			statusCode, err := p.healthCheckIP(targetIP)
			if err != nil {
				return // 测试失败，保持黑名单状态
			}

			if statusCode == 200 {
				// 恢复成功，从黑名单移除，加入白名单
				p.ipAccessControl.RemoveIP(targetIP, false) // 从黑名单移除
				p.ipAccessControl.AddIP(targetIP, true)     // 加入白名单
				fmt.Printf("[连接池] IP恢复 [%s]: 200 -> 从黑名单移除，加入白名单\n", targetIP)
			}
		}(ip)
	}

	wg.Wait()
}

// cleanupIdleConns 清理空闲连接（线程安全版本）
func (p *domainConnPool) cleanupIdleConns() {
	// 使用互斥锁保护清理操作，避免并发清理
	p.cleanupMutex.Lock()
	defer p.cleanupMutex.Unlock()

	// 检查连接池是否已关闭
	p.mutex.RLock()
	closed := p.closed
	p.mutex.RUnlock()

	if closed {
		return
	}

	cleanedCount := 0
	var validHealthy []*connMetadata
	var validUnhealthy []*connMetadata

	// 只清理健康连接池中已失效的连接（不检查空闲时间，热连接应该长期保持）
	for {
		select {
		case connMeta := <-p.healthyConns:
			// 只检查连接是否有效，不检查空闲时间
			if !p.isConnValid(connMeta.conn) {
				// 连接已失效，关闭
				fmt.Printf("[连接池] [清理] 健康池中的连接已失效，关闭 [目标IP: %s, 本地IP: %s]\n",
					connMeta.targetIP, connMeta.localIP)
				connMeta.conn.Close()
				cleanedCount++
				continue
			}
			// 连接仍然有效，保存到临时列表（无论空闲多久都保留）
			validHealthy = append(validHealthy, connMeta)
		default:
			goto unhealthy
		}
	}

unhealthy:
	// 只清理不健康连接池中已失效的连接（不检查空闲时间，热连接应该长期保持）
	for {
		select {
		case connMeta := <-p.unhealthyConns:
			// 只检查连接是否有效，不检查空闲时间
			if !p.isConnValid(connMeta.conn) {
				// 连接已失效，关闭
				fmt.Printf("[连接池] [清理] 不健康池中的连接已失效，关闭 [目标IP: %s, 本地IP: %s]\n",
					connMeta.targetIP, connMeta.localIP)
				connMeta.conn.Close()
				cleanedCount++
				continue
			}
			// 连接仍然有效，保存到临时列表（无论空闲多久都保留）
			validUnhealthy = append(validUnhealthy, connMeta)
		default:
			goto restore
		}
	}

restore:
	// 将有效连接放回池中
	for _, connMeta := range validHealthy {
		select {
		case p.healthyConns <- connMeta:
		default:
			// 健康池已满，尝试放入不健康池
			select {
			case p.unhealthyConns <- connMeta:
				// 成功放入不健康池
			default:
				// 不健康池也满了，保留连接但不放入池（连接会被GC回收，但不主动关闭）
				cleanedCount++
			}
		}
	}

	for _, connMeta := range validUnhealthy {
		select {
		case p.unhealthyConns <- connMeta:
		default:
			// 不健康池已满，保留连接但不放入池（连接会被GC回收，但不主动关闭）
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		fmt.Printf("[连接池] [清理] 清理了 %d 个无效连接 (健康池: %d个有效, 不健康池: %d个有效)\n",
			cleanedCount, len(validHealthy), len(validUnhealthy))
		p.printPoolStats()
	}
}

// printPoolStats 打印连接池统计信息
func (p *domainConnPool) printPoolStats() {
	healthyCount := len(p.healthyConns)
	unhealthyCount := len(p.unhealthyConns)

	p.ipListMutex.RLock()
	ipv6Count := len(p.targetIPv6List)
	ipv4Count := len(p.targetIPv4List)
	p.ipListMutex.RUnlock()

	whitelistCount := len(p.ipAccessControl.GetAllowedIPs())
	blacklistCount := len(p.ipAccessControl.GetBlockedIPs())

	// 获取IP统计信息
	ipStats := p.getIPStats()
	var totalSuccess int64
	var totalFailure int64
	for _, stats := range ipStats {
		totalSuccess += stats.SuccessCount
		totalFailure += stats.FailureCount
	}

	fmt.Printf("[连接池] [统计] 健康连接: %d, 不健康连接: %d, 目标IP: IPv6=%d IPv4=%d, 白名单: %d, 黑名单: %d\n",
		healthyCount, unhealthyCount, ipv6Count, ipv4Count, whitelistCount, blacklistCount)
	fmt.Printf("[连接池] [IP统计] 总成功: %d, 总失败: %d, 统计IP数: %d\n",
		totalSuccess, totalFailure, len(ipStats))

	// 如果IP数量不多（<=20），打印每个IP的详细统计
	if len(ipStats) > 0 && len(ipStats) <= 20 {
		fmt.Printf("[连接池] [IP详细统计] ")
		first := true
		for ip, stats := range ipStats {
			if !first {
				fmt.Printf(", ")
			}
			fmt.Printf("%s(成功:%d,失败:%d)", ip, stats.SuccessCount, stats.FailureCount)
			first = false
		}
		fmt.Printf("\n")
	}
}

// updateIPStats 更新IP统计信息（线程安全，内部方法）
func (p *domainConnPool) updateIPStats(targetIP string, statusCode int) {
	if targetIP == "" {
		return
	}

	p.ipStatsMutex.Lock()
	defer p.ipStatsMutex.Unlock()

	stats, exists := p.ipStatsMap[targetIP]
	if !exists {
		stats = &ipStats{}
		p.ipStatsMap[targetIP] = stats
	}

	if statusCode == 200 {
		stats.SuccessCount++
	} else {
		stats.FailureCount++
	}
}

// UpdateIPStats 更新IP统计信息（线程安全，公开方法，实现HotConnPool接口）
func (p *domainConnPool) UpdateIPStats(targetIP string, statusCode int) {
	p.updateIPStats(targetIP, statusCode)
}

// getIPStats 获取IP统计信息（线程安全）
func (p *domainConnPool) getIPStats() map[string]*ipStats {
	p.ipStatsMutex.RLock()
	defer p.ipStatsMutex.RUnlock()

	// 创建副本，避免外部修改
	result := make(map[string]*ipStats)
	for ip, stats := range p.ipStatsMap {
		result[ip] = &ipStats{
			SuccessCount: stats.SuccessCount,
			FailureCount: stats.FailureCount,
		}
	}
	return result
}

// Close 关闭连接池并释放所有资源
func (p *domainConnPool) Close() error {
	// 标记连接池为已关闭状态
	p.mutex.Lock()
	if p.closed {
		p.mutex.Unlock()
		return nil // 已经关闭
	}
	p.closed = true
	p.mutex.Unlock()

	// 发送停止信号（关闭stopChan）
	// 注意：只能关闭一次，所以先检查是否已关闭
	select {
	case <-p.stopChan:
		// 已经关闭
	default:
		close(p.stopChan)
	}

	// 等待所有后台任务结束
	p.wg.Wait()

	// 关闭健康连接池中的连接
	for {
		select {
		case connMeta := <-p.healthyConns:
			if connMeta != nil && connMeta.conn != nil {
				connMeta.conn.Close()
			}
		default:
			goto unhealthy
		}
	}

unhealthy:
	// 关闭不健康连接池中的连接
	for {
		select {
		case connMeta := <-p.unhealthyConns:
			if connMeta != nil && connMeta.conn != nil {
				connMeta.conn.Close()
			}
		default:
			goto cleanup
		}
	}

cleanup:
	// 关闭channel（在关闭所有连接后）
	// 注意：关闭已关闭的channel会panic，使用recover保护
	defer func() {
		if r := recover(); r != nil {
			// channel已关闭，忽略panic
		}
	}()

	// 尝试关闭channel，如果已关闭会panic，但会被recover捕获
	close(p.healthyConns)
	close(p.unhealthyConns)

	// 关闭本地IP池
	if p.localIPv4Pool != nil {
		p.localIPv4Pool.Close()
	}
	if p.localIPv6Pool != nil {
		p.localIPv6Pool.Close()
	}

	fmt.Println("[连接池] 已关闭")
	return nil
}

// NewDomainHotConnPoolFromConfig 从配置创建热连接池（便捷函数）
// 这个函数需要导入config包，但由于包依赖问题，建议在调用方实现
// 这里提供一个示例实现思路：
//
// func NewDomainHotConnPoolFromConfig(cfg *config.Config, domainMonitor DomainMonitor) (HotConnPool, error) {
//     // 创建本地IP池
//     localIPv4Pool, _ := NewLocalIPPool(
//         cfg.HotConnPool.LocalIPv4Addresses,
//         "",
//     )
//
//     localIPv6Pool, _ := NewLocalIPPool(
//         []string{},
//         cfg.HotConnPool.LocalIPv6SubnetCIDR,
//     )
//
//     // 获取TLS指纹
//     var fingerprint Profile
//     if cfg.HotConnPool.FingerprintName != "" {
//         profile, err := fpLibrary.ProfileByName(cfg.HotConnPool.FingerprintName)
//         if err != nil {
//             fingerprint = GetRandomFingerprint()
//         } else {
//             fingerprint = *profile
//         }
//     } else {
//         fingerprint = GetRandomFingerprint()
//     }
//
//     // 获取预热路径和请求头
//     warmupPath := cfg.GetWarmupPath()
//     warmupHeaders := cfg.GetWarmupHeaders()
//
//     // 创建连接池配置
//     poolConfig := DomainConnPoolConfig{
//         DomainMonitor:        domainMonitor,
//         IPAccessControl:     NewWhiteBlackIPPool(),
//         LocalIPv4Pool:       localIPv4Pool,
//         LocalIPv6Pool:        localIPv6Pool,
//         Fingerprint:         fingerprint,
//         Domain:              cfg.HotConnPool.Domain,
//         Port:                cfg.HotConnPool.Port,
//         MaxConns:            cfg.HotConnPool.MaxConns,
//         IdleTimeout:         cfg.HotConnPool.GetIdleTimeout(),
//         WarmupPath:          warmupPath,
//         WarmupMethod:        cfg.HotConnPool.WarmupMethod,
//         WarmupHeaders:       warmupHeaders,
//         WarmupConcurrency:   cfg.HotConnPool.WarmupConcurrency,
//         BlacklistTestInterval: cfg.HotConnPool.GetBlacklistTestInterval(),
//         IPRefreshInterval:    cfg.HotConnPool.GetIPRefreshInterval(),
//         DialTimeout:          cfg.UTlsClient.GetDialTimeout(),
//     }
//
//     return NewDomainHotConnPool(poolConfig)
// }
