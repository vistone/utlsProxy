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
	conn      *utls.UConn  // UTLS连接
	targetIP  string       // 目标IP地址
	localIP   string       // 本地绑定IP地址
	createdAt time.Time    // 连接创建时间
	lastUsed  time.Time    // 最后使用时间
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
	mutex    sync.RWMutex
	stopChan chan struct{}
	wg       sync.WaitGroup

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

	fmt.Printf("[连接池] IP列表已刷新：IPv6=%d个, IPv4=%d个\n", len(ipv6List), len(ipv4List))
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
func (p *domainConnPool) getLocalIP() (string, bool) {
	// 优先使用IPv6
	if p.hasIPv6Support && p.localIPv6Pool != nil {
		ip := p.localIPv6Pool.GetIP()
		if ip != nil {
			return ip.String(), true // 返回IPv6地址
		}
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
	tcpConn, err := dialer.Dial("tcp", net.JoinHostPort(targetIP, p.port))
	if err != nil {
		return nil, fmt.Errorf("TCP连接失败: %w", err)
	}

	// 创建UTLS连接
	uConn := utls.UClient(tcpConn, &utls.Config{
		ServerName:         p.domain,
		NextProtos:         []string{"h2", "http/1.1"},
		InsecureSkipVerify: false,
	}, p.fingerprint.HelloID)

	// 执行TLS握手
	err = uConn.Handshake()
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("TLS握手失败: %w", err)
	}

	return uConn, nil
}

// createConnectionWithFallback 创建连接（带降级策略）
// skipWhitelistCheck: 如果为true，跳过白名单检查（用于预热阶段）
func (p *domainConnPool) createConnectionWithFallback(skipWhitelistCheck bool) (*utls.UConn, string, string, error) {
	// 获取本地IP（IPv6优先）
	localIP, localIsIPv6 := p.getLocalIP()
	if localIP == "" {
		return nil, "", "", fmt.Errorf("无可用的本地IP地址")
	}

	// 策略1：IPv6本地IP + IPv6目标IP（最优）
	if localIsIPv6 {
		targetIP, targetIsIPv6 := p.getTargetIP(true) // 优先IPv6
		if targetIP != "" && targetIsIPv6 {
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

	// 创建请求
	req := &UTlsRequest{
		WorkID:      fmt.Sprintf("health-check-%d", time.Now().UnixNano()),
		Domain:      p.domain,
		Method:      p.warmupMethod,
		Path:        url,
		Headers:     p.warmupHeaders,
		Body:        nil,
		DomainIP:    targetIP,
		Fingerprint: p.fingerprint,
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
func (p *domainConnPool) healthCheckWithConn(conn *utls.UConn, targetIP string) (int, error) {
	// 构建完整URL
	url := fmt.Sprintf("https://%s%s", p.domain, p.warmupPath)

	// 获取连接状态，判断协议类型
	state := conn.ConnectionState()
	negotiatedProtocol := state.NegotiatedProtocol
	if negotiatedProtocol == "" {
		negotiatedProtocol = "http/1.1"
	}

	// 创建请求对象
	req := &UTlsRequest{
		WorkID:      fmt.Sprintf("health-check-conn-%d", time.Now().UnixNano()),
		Domain:      p.domain,
		Method:      p.warmupMethod,
		Path:        url,
		Headers:     p.warmupHeaders,
		Body:        nil,
		DomainIP:    targetIP,
		Fingerprint: p.fingerprint,
		StartTime:   time.Now(),
	}

	// 根据协议类型发送请求
	if negotiatedProtocol == "h2" {
		// HTTP/2协议
		statusCode, _, err := p.healthCheckClient.sendHTTP2Request(conn, req)
		if err != nil {
			return 0, err
		}
		return statusCode, nil
	} else {
		// HTTP/1.1协议
		err := p.healthCheckClient.sendHTTPRequest(conn, req)
		if err != nil {
			return 0, err
		}
		statusCode, _, err := p.healthCheckClient.readHTTPResponse(conn)
		if err != nil {
			return 0, err
		}
		return statusCode, nil
	}
}

// GetConn 从连接池获取一个可用连接
func (p *domainConnPool) GetConn() (*utls.UConn, error) {
	// 优先从健康连接池获取
	select {
	case connMeta := <-p.healthyConns:
		// 更新最后使用时间
		connMeta.lastUsed = time.Now()
		return connMeta.conn, nil
	default:
		// 健康池为空，继续尝试不健康池
	}

	// 尝试从不健康连接池获取
	select {
	case connMeta := <-p.unhealthyConns:
		// 更新最后使用时间
		connMeta.lastUsed = time.Now()
		return connMeta.conn, nil
	default:
		// 两个池都为空，创建新连接
	}

	// 创建新连接（正常使用时需要检查白名单）
	conn, localIP, targetIP, err := p.createConnectionWithFallback(false)
	if err != nil {
		return nil, fmt.Errorf("创建连接失败: %w", err)
	}

	// 创建连接元数据（虽然不会放入池中，但保持一致性）
	_ = &connMetadata{
		conn:      conn,
		targetIP:  targetIP,
		localIP:   localIP,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}

	return conn, nil
}

// ReturnConn 将连接返回到连接池
// 注意：此方法需要知道连接的目标IP，但当前接口只接收conn和statusCode
// 为了兼容性，我们尝试从连接获取远程地址，如果失败则无法更新黑白名单
func (p *domainConnPool) ReturnConn(conn *utls.UConn, statusCode int) error {
	if conn == nil {
		return fmt.Errorf("连接不能为空")
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
	connMeta := &connMetadata{
		conn:      conn,
		targetIP:  targetIP,
		localIP:   localIP,
		createdAt: time.Now(), // 如果是从池中取出的连接，这个时间可能不准确，但不影响使用
		lastUsed:  time.Now(),
	}

	// 根据状态码判断连接健康状态
	if statusCode == 200 {
		// 健康连接，加入白名单并放入健康池
		if targetIP != "" {
			p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
		}
		select {
		case p.healthyConns <- connMeta:
			return nil
		default:
			// 健康池已满，关闭连接
			conn.Close()
			return nil
		}
	} else if statusCode == 403 {
		// 403错误，IP被封，加入黑名单
		if targetIP != "" {
			p.ipAccessControl.AddIP(targetIP, false) // 加入黑名单
		}
		// 被封的连接直接关闭，不放入池中
		conn.Close()
		return nil
	} else {
		// 其他错误，放入不健康池
		select {
		case p.unhealthyConns <- connMeta:
			return nil
		default:
			conn.Close()
			return nil
		}
	}
}

// Warmup 预热连接池
// 预热阶段应该测试所有IP，而不是只测试白名单中的IP
// 因为系统启动时白名单是空的，需要通过预热来填充
func (p *domainConnPool) Warmup() error {
	fmt.Printf("[连接池] 开始预热，并发数: %d\n", p.warmupConcurrency)

	// 刷新IP列表
	p.refreshTargetIPList()

	// 获取所有目标IP（预热时不进行白名单过滤，测试所有IP）
	p.ipListMutex.RLock()
	allIPv6 := p.targetIPv6List // 直接使用所有IPv6，不进行白名单过滤
	allIPv4 := p.targetIPv4List // 直接使用所有IPv4，不进行白名单过滤
	p.ipListMutex.RUnlock()

	fmt.Printf("[连接池] 预热IP数量：IPv6=%d个, IPv4=%d个\n", len(allIPv6), len(allIPv4))

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
	fmt.Printf("[连接池] 预热完成\n")
	return nil
}

// warmupSingleIP 预热单个IP
// 预热时跳过白名单检查，允许测试所有IP
func (p *domainConnPool) warmupSingleIP(targetIP string, isIPv6 bool) {
	// 获取本地IP（优先使用IPv6）
	localIP, _ := p.getLocalIP()
	if localIP == "" && p.hasIPv6Support {
		// 如果没有本地IP但支持IPv6，尝试获取IPv6本地IP
		if p.localIPv6Pool != nil {
			ip := p.localIPv6Pool.GetIP()
			if ip != nil {
				localIP = ip.String()
			}
		}
	}

	// 创建连接（预热时跳过白名单检查）
	conn, err := p.createConnection(localIP, targetIP, true) // skipWhitelistCheck = true
	if err != nil {
		fmt.Printf("[预热] 连接创建失败 [%s]: %v\n", targetIP, err)
		return
	}

	// 使用已建立的连接进行健康检查（不关闭连接）
	statusCode, err := p.healthCheckWithConn(conn, targetIP)
	if err != nil {
		// 健康检查失败，关闭连接
		conn.Close()
		fmt.Printf("[预热] 健康检查失败 [%s]: %v\n", targetIP, err)
		return
	}

	// 创建连接元数据
	now := time.Now()
	connMeta := &connMetadata{
		conn:      conn,
		targetIP:  targetIP,
		localIP:   localIP,
		createdAt: now,
		lastUsed:  now,
	}

	// 根据状态码更新黑白名单并将连接放入池中
	switch statusCode {
	case 200:
		// 健康连接，加入白名单并放入健康连接池
		p.ipAccessControl.AddIP(targetIP, true) // 加入白名单
		select {
		case p.healthyConns <- connMeta:
			// 成功放入健康连接池，连接被保留用于复用
			fmt.Printf("[预热] 成功 [%s]: 200 -> 白名单，连接已放入健康池\n", targetIP)
		default:
			// 健康池已满，关闭连接
			conn.Close()
			fmt.Printf("[预热] 成功 [%s]: 200 -> 白名单，但健康池已满，连接已关闭\n", targetIP)
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
			fmt.Printf("[预热] 警告 [%s]: 状态码 %d -> 不健康池\n", targetIP, statusCode)
		default:
			// 不健康池已满，关闭连接
			conn.Close()
			fmt.Printf("[预热] 警告 [%s]: 状态码 %d，但不健康池已满，连接已关闭\n", targetIP, statusCode)
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

	// 连接清理任务（清理超时连接）
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

// cleanupIdleConns 清理空闲连接
func (p *domainConnPool) cleanupIdleConns() {
	now := time.Now()
	cleanedCount := 0

	// 清理健康连接池中的空闲连接
	for {
		select {
		case connMeta := <-p.healthyConns:
			// 检查连接是否超时
			if now.Sub(connMeta.lastUsed) > p.idleTime {
				// 连接空闲时间超过阈值，关闭连接
				connMeta.conn.Close()
				cleanedCount++
			} else {
				// 连接仍然有效，放回池中
				select {
				case p.healthyConns <- connMeta:
				default:
					// 池已满，关闭连接
					connMeta.conn.Close()
				}
			}
		default:
			goto unhealthy
		}
	}

unhealthy:
	// 清理不健康连接池中的空闲连接
	for {
		select {
		case connMeta := <-p.unhealthyConns:
			// 检查连接是否超时
			if now.Sub(connMeta.lastUsed) > p.idleTime {
				// 连接空闲时间超过阈值，关闭连接
				connMeta.conn.Close()
				cleanedCount++
			} else {
				// 连接仍然有效，放回池中
				select {
				case p.unhealthyConns <- connMeta:
				default:
					// 池已满，关闭连接
					connMeta.conn.Close()
				}
			}
		default:
			goto done
		}
	}

done:
	if cleanedCount > 0 {
		fmt.Printf("[连接池] 清理了 %d 个空闲连接\n", cleanedCount)
	}
}

// Close 关闭连接池并释放所有资源
func (p *domainConnPool) Close() error {
	// 发送停止信号
	close(p.stopChan)

	// 等待所有后台任务结束
	p.wg.Wait()

	// 关闭健康连接池中的连接
	for {
		select {
		case connMeta := <-p.healthyConns:
			connMeta.conn.Close()
		default:
			goto unhealthy
		}
	}

unhealthy:
	// 关闭不健康连接池中的连接
	for {
		select {
		case connMeta := <-p.unhealthyConns:
			connMeta.conn.Close()
		default:
			goto cleanup
		}
	}

cleanup:
	// 关闭channel
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
