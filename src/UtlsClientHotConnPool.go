// Package src defines the source code for the utlsProxy.
package src

import (
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// HotConnPool defines the hot connection pool interface
type HotConnPool interface {
	GetConn() (*ConnMetadata, error)
	GetConnByIP(targetIP string) (*ConnMetadata, error)
	ReturnConn(connMeta *ConnMetadata, statusCode int) error
	UpdateIPStats(targetIP string, statusCode int)
	Close() error
	Warmup() error
}

// ConnMetadata contains connection metadata
type ConnMetadata struct {
	Conn       *utls.UConn
	HttpClient *http.Client // For HTTP/2 connections
	Protocol   string       // "h2" or "http/1.1"
	TargetIP   string
	LocalIP    string
	CreatedAt  time.Time
	LastUsed   time.Time
}

// ipStats contains IP statistics
type ipStats struct {
	SuccessCount int64
	FailureCount int64
}

// ipWarmupJob represents an IP to be warmed up
type ipWarmupJob struct {
	ip     string
	isIPv6 bool
}

// domainConnPool implements the HotConnPool interface
type domainConnPool struct {
	healthyConns     chan *ConnMetadata
	unhealthyConns   chan *ConnMetadata
	ipConnPools      map[string]chan *ConnMetadata
	ipConnPoolsMutex sync.RWMutex

	domainMonitor   DomainMonitor
	ipAccessControl IPAccessController
	fingerprint     Profile
	localIPv4Pool   IPPool
	localIPv6Pool   IPPool
	hasIPv6Support  bool

	targetIPv6List []string
	targetIPv4List []string
	ipListMutex    sync.RWMutex

	knownTargetIPs    map[string]struct{}
	pendingWarmups    []ipWarmupJob
	autoWarmupEnabled int32

	mutex    sync.RWMutex
	stopChan chan struct{}
	wg       sync.WaitGroup
	closed   bool

	maxConns          int
	idleTime          time.Duration
	domain            string
	port              string
	warmupPath        string
	warmupMethod      string
	warmupHeaders     map[string]string
	warmupConcurrency int

	blacklistTestInterval time.Duration
	ipRefreshInterval     time.Duration

	healthCheckClient *UTlsClient
	rand              *rand.Rand
	ipStatsMap        map[string]*ipStats
	ipStatsMutex      sync.RWMutex
	sessionCache      utls.ClientSessionCache
}

// DomainConnPoolConfig defines the configuration for the domain connection pool
type DomainConnPoolConfig struct {
	DomainMonitor         DomainMonitor
	IPAccessControl       IPAccessController
	LocalIPv4Pool         IPPool
	LocalIPv6Pool         IPPool
	Fingerprint           Profile
	Domain                string
	Port                  string
	MaxConns              int
	IdleTimeout           time.Duration
	WarmupPath            string
	WarmupMethod          string
	WarmupHeaders         map[string]string
	WarmupConcurrency     int
	BlacklistTestInterval time.Duration
	IPRefreshInterval     time.Duration
	DialTimeout           time.Duration
}

// NewDomainHotConnPool creates a new domain-based hot connection pool
func NewDomainHotConnPool(config DomainConnPoolConfig) (HotConnPool, error) {
	// Set defaults
	if config.Port == "" {
		config.Port = "443"
	}
	if config.MaxConns == 0 {
		config.MaxConns = 1000000
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

	sessionCache := utls.NewLRUClientSessionCache(1000)

	pool := &domainConnPool{
		healthyConns:          make(chan *ConnMetadata, config.MaxConns),
		unhealthyConns:        make(chan *ConnMetadata, config.MaxConns),
		ipConnPools:           make(map[string]chan *ConnMetadata),
		domainMonitor:         config.DomainMonitor,
		ipAccessControl:       config.IPAccessControl,
		fingerprint:           config.Fingerprint,
		localIPv4Pool:         config.LocalIPv4Pool,
		localIPv6Pool:         config.LocalIPv6Pool,
		hasIPv6Support:        config.LocalIPv6Pool != nil,
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
		sessionCache:          sessionCache,
		knownTargetIPs:        make(map[string]struct{}),
	}

	pool.healthCheckClient.DialTimeout = config.DialTimeout
	pool.healthCheckClient.ReadTimeout = 30 * time.Second

	pool.refreshTargetIPList()
	pool.startBackgroundTasks()

	return pool, nil
}

// createConnection creates a single UTLS connection and wraps it in ConnMetadata
func (p *domainConnPool) createConnection(localIP, targetIP string, skipWhitelistCheck bool) (*ConnMetadata, error) {
	if !skipWhitelistCheck && !p.ipAccessControl.IsIPAllowed(targetIP) {
		// If whitelist is not empty, we must adhere to it.
		// If it's empty (e.g., during initial startup), allow attempts.
		if len(p.ipAccessControl.GetAllowedIPs()) > 0 {
			return nil, fmt.Errorf("目标IP %s 不在白名单中", targetIP)
		}
	}

	dialer := net.Dialer{Timeout: p.healthCheckClient.DialTimeout}
	if localIP != "" {
		localIPAddr := net.ParseIP(localIP)
		if localIPAddr == nil {
			return nil, fmt.Errorf("无效的本地IP地址: %s", localIP)
		}
		
		// 检查本地IP和目标IP的类型是否匹配
		targetIPAddr := net.ParseIP(targetIP)
		if targetIPAddr != nil {
			localIsIPv6 := localIPAddr.To4() == nil && localIPAddr.To16() != nil
			targetIsIPv6 := targetIPAddr.To4() == nil && targetIPAddr.To16() != nil
			
			// 如果类型不匹配，不绑定本地IP，让系统自动选择
			if localIsIPv6 != targetIsIPv6 {
				localIP = "" // 清空本地IP，让系统自动选择
			} else {
				dialer.LocalAddr = &net.TCPAddr{IP: localIPAddr, Port: 0}
			}
		} else {
			dialer.LocalAddr = &net.TCPAddr{IP: localIPAddr, Port: 0}
		}
	}

	// 尝试使用指定的本地IP连接
	tcpConn, err := dialer.Dial("tcp", net.JoinHostPort(targetIP, p.port))
	if err != nil {
		// 如果连接失败且使用了IPv6本地地址，标记为未使用（不立即删除）
		if localIP != "" {
			localIPAddr := net.ParseIP(localIP)
			if localIPAddr != nil && localIPAddr.To4() == nil && localIPAddr.To16() != nil {
				// 是IPv6地址，连接失败时标记为未使用
				if p.localIPv6Pool != nil {
					p.localIPv6Pool.MarkIPUnused(localIPAddr)
				}
			}
		}
		
		// 如果绑定本地IP失败（通常是IPv6地址未在系统上配置），尝试不绑定本地IP
		if localIP != "" && (strings.Contains(err.Error(), "cannot assign requested address") || 
			strings.Contains(err.Error(), "bind: cannot assign requested address") ||
			strings.Contains(err.Error(), "no suitable address found")) {
			// 回退到不绑定本地IP的方式
			dialerWithoutLocal := net.Dialer{Timeout: p.healthCheckClient.DialTimeout}
			tcpConn, err = dialerWithoutLocal.Dial("tcp", net.JoinHostPort(targetIP, p.port))
			if err != nil {
				return nil, fmt.Errorf("TCP连接失败（已尝试回退）: %w", err)
			}
			// 注意：这里不更新 localIP 变量，保持原始意图记录
		} else {
			return nil, fmt.Errorf("TCP连接失败: %w", err)
		}
	}

	fingerprint := p.fingerprint
	if fingerprint.HelloID.Client == "" {
		fingerprint = GetRandomFingerprint()
	}

	uConn := utls.UClient(tcpConn, &utls.Config{
		ServerName:         p.domain,
		NextProtos:         []string{"h2", "http/1.1"},
		InsecureSkipVerify: false,
		OmitEmptyPsk:       true,
		ClientSessionCache: p.sessionCache,
	}, fingerprint.HelloID)

	if err := uConn.Handshake(); err != nil {
		_ = uConn.Close()
		// TLS握手失败，如果是IPv6地址，标记为未使用（不立即删除）
		if localIP != "" {
			localIPAddr := net.ParseIP(localIP)
			if localIPAddr != nil && localIPAddr.To4() == nil && localIPAddr.To16() != nil {
				if p.localIPv6Pool != nil {
					p.localIPv6Pool.MarkIPUnused(localIPAddr)
				}
			}
		}
		return nil, fmt.Errorf("TLS握手失败: %w", err)
	}

	state := uConn.ConnectionState()
	protocol := state.NegotiatedProtocol
	if protocol == "" {
		protocol = "http/1.1"
	}

	meta := &ConnMetadata{
		Conn:      uConn,
		Protocol:  protocol,
		TargetIP:  targetIP,
		LocalIP:   localIP,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}

	if protocol == "h2" {
		transport := &http2.Transport{
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return uConn, nil
			},
			AllowHTTP: true,
		}
		meta.HttpClient = &http.Client{
			Transport: transport,
			Timeout:   p.healthCheckClient.ReadTimeout,
		}
	}

	return meta, nil
}

// GetConn gets a connection from the pool
func (p *domainConnPool) GetConn() (*ConnMetadata, error) {
	if p.isClosed() {
		return nil, fmt.Errorf("连接池已关闭")
	}

	select {
	case connMeta := <-p.healthyConns:
		if !p.isConnValid(connMeta.Conn) {
			_ = connMeta.Conn.Close()
			return p.GetConn() // Retry
		}
		connMeta.LastUsed = time.Now()
		return connMeta, nil
	default:
	}

	select {
	case connMeta := <-p.unhealthyConns:
		if !p.isConnValid(connMeta.Conn) {
			_ = connMeta.Conn.Close()
			return p.GetConn() // Retry
		}
		connMeta.LastUsed = time.Now()
		return connMeta, nil
	default:
	}

	connMeta, targetIP, err := p.createConnectionWithFallback(false)
	if err != nil {
		return nil, fmt.Errorf("无法创建新连接: %w", err)
	}
	if connMeta != nil {
		return connMeta, nil
	}
	return p.createConnection("", targetIP, false)
}

// GetConnByIP gets a connection for a specific IP from the pool
func (p *domainConnPool) GetConnByIP(targetIP string) (*ConnMetadata, error) {
	if targetIP == "" {
		return nil, fmt.Errorf("目标IP不能为空")
	}
	if p.isClosed() {
		return nil, fmt.Errorf("连接池已关闭")
	}

	p.ipConnPoolsMutex.RLock()
	ipPool, exists := p.ipConnPools[targetIP]
	p.ipConnPoolsMutex.RUnlock()

	if exists {
		select {
		case connMeta := <-ipPool:
			if !p.isConnValid(connMeta.Conn) {
				_ = connMeta.Conn.Close()
				return p.GetConnByIP(targetIP) // Retry
			}
			connMeta.LastUsed = time.Now()
			return connMeta, nil
		default:
		}
	}

	// 判断目标IP类型
	targetIPAddr := net.ParseIP(targetIP)
	isTargetIPv6 := targetIPAddr != nil && targetIPAddr.To4() == nil && targetIPAddr.To16() != nil
	// 根据目标IP类型选择相应的本地IP
	localIP := p.getLocalIPForTarget(targetIP, isTargetIPv6)
	return p.createConnection(localIP, targetIP, false)
}

// ReturnConn returns a connection to the pool
func (p *domainConnPool) ReturnConn(connMeta *ConnMetadata, statusCode int) error {
	if connMeta == nil || connMeta.Conn == nil {
		return fmt.Errorf("连接元数据或连接不能为空")
	}
	if p.isClosed() {
		_ = connMeta.Conn.Close()
		return fmt.Errorf("连接池已关闭")
	}

	if !p.isConnValid(connMeta.Conn) {
		_ = connMeta.Conn.Close()
		return fmt.Errorf("返回的连接已失效")
	}

	if statusCode == 0 {
		_ = connMeta.Conn.Close()
		return nil
	}

	p.UpdateIPStats(connMeta.TargetIP, statusCode)

	if statusCode == 200 {
		p.ipAccessControl.AddIP(connMeta.TargetIP, true)
		p.returnToPool(connMeta, p.healthyConns)
		// 不再立即释放IPv6地址，由定期清理机制负责
	} else if statusCode == 403 {
		p.ipAccessControl.AddIP(connMeta.TargetIP, false)
		_ = connMeta.Conn.Close()
		fmt.Printf("[连接池] IP加入黑名单 [%s]，连接已关闭\n", connMeta.TargetIP)
		// 403错误时标记地址为未使用，但不立即删除
		if connMeta.LocalIP != "" {
			localIPAddr := net.ParseIP(connMeta.LocalIP)
			if localIPAddr != nil && localIPAddr.To4() == nil && localIPAddr.To16() != nil {
				if p.localIPv6Pool != nil {
					p.localIPv6Pool.MarkIPUnused(localIPAddr)
				}
			}
		}
	} else {
		p.returnToPool(connMeta, p.unhealthyConns)
		// 其他错误状态码，标记为未使用，但不立即删除
		if connMeta.LocalIP != "" {
			localIPAddr := net.ParseIP(connMeta.LocalIP)
			if localIPAddr != nil && localIPAddr.To4() == nil && localIPAddr.To16() != nil {
				if p.localIPv6Pool != nil {
					p.localIPv6Pool.MarkIPUnused(localIPAddr)
				}
			}
		}
	}
	return nil
}

func (p *domainConnPool) returnToPool(connMeta *ConnMetadata, pool chan<- *ConnMetadata) {
	p.ipConnPoolsMutex.RLock()
	ipPool, exists := p.ipConnPools[connMeta.TargetIP]
	p.ipConnPoolsMutex.RUnlock()

	if exists {
		select {
		case ipPool <- connMeta:
		default:
			select {
			case pool <- connMeta:
			default:
				_ = connMeta.Conn.Close()
			}
		}
	} else {
		select {
		case pool <- connMeta:
		default:
			_ = connMeta.Conn.Close()
		}
	}
}

// Warmup pre-warms the connection pool
func (p *domainConnPool) Warmup() error {
	fmt.Printf("[连接池] 开始预热域名 [%s]，并发数: %d\n", p.domain, p.warmupConcurrency)
	p.refreshTargetIPList()

	p.ipListMutex.RLock()
	var jobs []ipWarmupJob
	for _, ip := range p.targetIPv6List {
		jobs = append(jobs, ipWarmupJob{ip: ip, isIPv6: true})
	}
	for _, ip := range p.targetIPv4List {
		jobs = append(jobs, ipWarmupJob{ip: ip, isIPv6: false})
	}
	p.ipListMutex.RUnlock()

	p.runWarmupJobs(jobs)

	fmt.Printf("[连接池] 域名 [%s] 预热完成\n", p.domain)
	p.printPoolStats()

	atomic.StoreInt32(&p.autoWarmupEnabled, 1)
	p.processPendingWarmups()
	return nil
}

func (p *domainConnPool) runWarmupJobs(jobs []ipWarmupJob) {
	if len(jobs) == 0 {
		return
	}
	semaphore := make(chan struct{}, p.warmupConcurrency)
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(job ipWarmupJob) {
			defer wg.Done()
			defer func() { <-semaphore }()
			p.warmupSingleIP(job.ip, job.isIPv6)
		}(job)
	}
	wg.Wait()
}

func (p *domainConnPool) warmupSingleIP(targetIP string, isIPv6 bool) {
	// 根据目标IP类型选择相应的本地IP
	localIP := p.getLocalIPForTarget(targetIP, isIPv6)
	connMeta, err := p.createConnection(localIP, targetIP, true)
	if err != nil {
		fmt.Printf("[预热] 连接创建失败 [%s]: %v\n", targetIP, err)
		// 连接失败，IPv6地址已在createConnection中释放
		return
	}

	if connMeta.Protocol == "http/1.1" {
		statusCode, bodyLen, err := p.healthCheckWithConn(connMeta, targetIP)
		if err != nil {
			_ = connMeta.Conn.Close()
			fmt.Printf("[预热] 健康检查失败 [%s]: %v\n", targetIP, err)
			// 健康检查失败，标记为未使用（不立即删除）
			if connMeta.LocalIP != "" {
				localIPAddr := net.ParseIP(connMeta.LocalIP)
				if localIPAddr != nil && localIPAddr.To4() == nil && localIPAddr.To16() != nil {
					if p.localIPv6Pool != nil {
						p.localIPv6Pool.MarkIPUnused(localIPAddr)
					}
				}
			}
			return
		}
		if statusCode != 200 || bodyLen != 13 {
			_ = connMeta.Conn.Close()
			fmt.Printf("[预热] 警告 [%s]: 状态码 %d, Body %d字节 -> 连接已关闭\n", targetIP, statusCode, bodyLen)
			if statusCode == 403 {
				p.ipAccessControl.AddIP(targetIP, false)
			}
			// 状态码不是200，标记为未使用（不立即删除）
			if connMeta.LocalIP != "" {
				localIPAddr := net.ParseIP(connMeta.LocalIP)
				if localIPAddr != nil && localIPAddr.To4() == nil && localIPAddr.To16() != nil {
					if p.localIPv6Pool != nil {
						p.localIPv6Pool.MarkIPUnused(localIPAddr)
					}
				}
			}
			return
		}
	}

	p.ipAccessControl.AddIP(targetIP, true)
	p.returnToPool(connMeta, p.healthyConns)
	fmt.Printf("[预热] 成功 [%s]: %s -> 连接已放入健康池\n", targetIP, connMeta.Protocol)
	// 不再立即释放IPv6地址，由定期清理机制负责
}

func (p *domainConnPool) healthCheckWithConn(connMeta *ConnMetadata, targetIP string) (int, int, error) {
	req := &UTlsRequest{
		Domain:      p.domain,
		Method:      p.warmupMethod,
		Path:        fmt.Sprintf("https://%s%s", p.domain, p.warmupPath),
		Headers:     p.warmupHeaders,
		Fingerprint: p.fingerprint,
	}

	if connMeta.Protocol == "h2" {
		resp, err := connMeta.HttpClient.Do(&http.Request{}) // Simplified
		if err != nil {
			return 0, 0, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, len(body), nil
	}

	if err := p.healthCheckClient.sendHTTPRequest(connMeta.Conn, req); err != nil {
		return 0, 0, err
	}
	statusCode, body, err := p.healthCheckClient.readHTTPResponse(connMeta.Conn)
	return statusCode, len(body), err
}

func (p *domainConnPool) isClosed() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.closed
}

func (p *domainConnPool) refreshTargetIPList() {
	if p.domainMonitor == nil {
		return
	}
	domainPool, ok := p.domainMonitor.GetDomainPool(p.domain)
	if !ok {
		return
	}

	collectIPs := func(records []IPRecord) []string {
		if len(records) == 0 {
			return nil
		}
		unique := make(map[string]struct{}, len(records))
		result := make([]string, 0, len(records))
		for _, record := range records {
			ip := strings.TrimSpace(record.IP)
			if ip == "" {
				continue
			}
			if _, exists := unique[ip]; exists {
				continue
			}
			unique[ip] = struct{}{}
			result = append(result, ip)
		}
		return result
	}

	newIPv4 := collectIPs(domainPool["ipv4"])
	newIPv6 := collectIPs(domainPool["ipv6"])

	p.ipListMutex.Lock()
	oldKnown := p.knownTargetIPs
	if oldKnown == nil {
		oldKnown = make(map[string]struct{})
	}

	newKnown := make(map[string]struct{}, len(newIPv4)+len(newIPv6))
	for _, ip := range newIPv6 {
		newKnown[ip] = struct{}{}
		if _, exists := oldKnown[ip]; !exists {
			p.pendingWarmups = append(p.pendingWarmups, ipWarmupJob{ip: ip, isIPv6: true})
		}
	}
	for _, ip := range newIPv4 {
		newKnown[ip] = struct{}{}
		if _, exists := oldKnown[ip]; !exists {
			p.pendingWarmups = append(p.pendingWarmups, ipWarmupJob{ip: ip, isIPv6: false})
		}
	}

	p.targetIPv4List = newIPv4
	p.targetIPv6List = newIPv6
	p.knownTargetIPs = newKnown
	
	// 更新IPv6地址池的目标IP数量（与目标IPv6地址数量保持一致）
	totalTargetIPs := len(newIPv6) + len(newIPv4)
	if p.localIPv6Pool != nil && totalTargetIPs > 0 {
		p.localIPv6Pool.SetTargetIPCount(totalTargetIPs)
	}
	
	p.ipListMutex.Unlock()
}
func (p *domainConnPool) startBackgroundTasks() {
	if p.ipRefreshInterval <= 0 {
		return
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.ipRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.refreshTargetIPList()
				if atomic.LoadInt32(&p.autoWarmupEnabled) == 1 {
					p.processPendingWarmups()
				}
			case <-p.stopChan:
				return
			}
		}
	}()
}
func (p *domainConnPool) getLocalIP() (string, bool) {
	if p.localIPv6Pool != nil {
		if ip := p.localIPv6Pool.GetIP(); ip != nil {
			if ip.To16() != nil && ip.To4() == nil {
				return ip.String(), true
			}
			// 隧道模式下 IPv6 池会返回 nil，继续尝试 IPv4
		}
	}

	if p.localIPv4Pool != nil {
		if ip := p.localIPv4Pool.GetIP(); ip != nil {
			return ip.String(), false
		}
	}

	return "", false
}

// getLocalIPForTarget 根据目标IP类型返回相应的本地IP
func (p *domainConnPool) getLocalIPForTarget(targetIP string, isTargetIPv6 bool) string {
	if isTargetIPv6 {
		// 目标是IPv6，优先使用IPv6本地地址
		if p.localIPv6Pool != nil {
			if ip := p.localIPv6Pool.GetIP(); ip != nil {
				if ip.To16() != nil && ip.To4() == nil {
					return ip.String()
				}
			}
		}
		// 如果没有IPv6本地地址，返回空字符串，让系统自动选择
		return ""
	} else {
		// 目标是IPv4，使用IPv4本地地址
		if p.localIPv4Pool != nil {
			if ip := p.localIPv4Pool.GetIP(); ip != nil {
				return ip.String()
			}
		}
		// 如果没有IPv4本地地址，返回空字符串，让系统自动选择
		return ""
	}
}
func (p *domainConnPool) createConnectionWithFallback(skipWhitelistCheck bool) (*ConnMetadata, string, error) {
	p.ipListMutex.RLock()
	currentIPv6 := append([]string(nil), p.targetIPv6List...)
	currentIPv4 := append([]string(nil), p.targetIPv4List...)
	p.ipListMutex.RUnlock()

	totalCandidates := len(currentIPv6) + len(currentIPv4)
	if totalCandidates == 0 {
		return nil, "", fmt.Errorf("域名 [%s] 尚无可用IP", p.domain)
	}

	filteredIPv6 := p.filterAllowedIPs(currentIPv6)
	filteredIPv4 := p.filterAllowedIPs(currentIPv4)
	if len(filteredIPv6) == 0 && len(filteredIPv4) == 0 {
		filteredIPv6 = currentIPv6
		filteredIPv4 = currentIPv4
	}

	candidates := make([]ipWarmupJob, 0, len(filteredIPv6)+len(filteredIPv4))
	for _, ip := range filteredIPv6 {
		candidates = append(candidates, ipWarmupJob{ip: ip, isIPv6: true})
	}
	for _, ip := range filteredIPv4 {
		candidates = append(candidates, ipWarmupJob{ip: ip, isIPv6: false})
	}

	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("域名 [%s] 无候选IP供创建连接", p.domain)
	}

	order := p.rand.Perm(len(candidates))
	var lastErr error

	for _, idx := range order {
		candidate := candidates[idx]
		// 根据目标IP类型选择相应的本地IP
		localIP := p.getLocalIPForTarget(candidate.ip, candidate.isIPv6)
		connMeta, err := p.createConnection(localIP, candidate.ip, skipWhitelistCheck)
		if err == nil {
			return connMeta, candidate.ip, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("未知错误")
	}
	return nil, "", fmt.Errorf("为域名 [%s] 创建连接失败: %w", p.domain, lastErr)
}
func (p *domainConnPool) filterAllowedIPs(ips []string) []string {
	if len(ips) == 0 {
		return nil
	}
	if p.ipAccessControl == nil {
		return append([]string(nil), ips...)
	}

	allowed := p.ipAccessControl.GetAllowedIPs()
	if len(allowed) == 0 {
		return append([]string(nil), ips...)
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, ip := range allowed {
		allowedSet[ip] = struct{}{}
	}

	filtered := make([]string, 0, len(ips))
	for _, ip := range ips {
		if _, ok := allowedSet[ip]; ok {
			filtered = append(filtered, ip)
		}
	}
	return filtered
}
func (p *domainConnPool) isConnValid(conn *utls.UConn) bool {
	if conn == nil {
		return false
	}
	state := conn.ConnectionState()
	return state.HandshakeComplete && conn.RemoteAddr() != nil
}
func (p *domainConnPool) UpdateIPStats(targetIP string, statusCode int) {
	if targetIP == "" {
		return
	}

	p.ipStatsMutex.Lock()
	stats, exists := p.ipStatsMap[targetIP]
	if !exists {
		stats = &ipStats{}
		p.ipStatsMap[targetIP] = stats
	}

	if statusCode >= 200 && statusCode < 300 {
		stats.SuccessCount++
	} else if statusCode >= 400 {
		stats.FailureCount++
	}
	p.ipStatsMutex.Unlock()
}
func (p *domainConnPool) printPoolStats() {
	healthy := len(p.healthyConns)
	unhealthy := len(p.unhealthyConns)

	p.ipStatsMutex.RLock()
	totalIPs := len(p.ipStatsMap)
	p.ipStatsMutex.RUnlock()

	fmt.Printf("[连接池] 状态: 健康连接=%d, 待恢复连接=%d, 已跟踪IP=%d\n", healthy, unhealthy, totalIPs)
}
func (p *domainConnPool) Close() error {
	p.mutex.Lock()
	if p.closed {
		p.mutex.Unlock()
		return nil
	}
	p.closed = true
	close(p.stopChan)
	p.mutex.Unlock()

	p.wg.Wait()

cleanupChannels:
	for {
		select {
		case connMeta := <-p.healthyConns:
			if connMeta != nil && connMeta.Conn != nil {
				_ = connMeta.Conn.Close()
			}
		default:
			break cleanupChannels
		}
	}

cleanupUnhealthy:
	for {
		select {
		case connMeta := <-p.unhealthyConns:
			if connMeta != nil && connMeta.Conn != nil {
				_ = connMeta.Conn.Close()
			}
		default:
			break cleanupUnhealthy
		}
	}

	p.ipConnPoolsMutex.Lock()
	for key, pool := range p.ipConnPools {
		close(pool)
		delete(p.ipConnPools, key)
	}
	p.ipConnPoolsMutex.Unlock()

	return nil
}
func (p *domainConnPool) processPendingWarmups() {
	p.ipListMutex.Lock()
	if len(p.pendingWarmups) == 0 {
		p.ipListMutex.Unlock()
		return
	}
	jobs := make([]ipWarmupJob, len(p.pendingWarmups))
	copy(jobs, p.pendingWarmups)
	p.pendingWarmups = nil
	p.ipListMutex.Unlock()

	p.runWarmupJobs(jobs)
}
