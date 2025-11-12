package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"utlsProxy/config"
	"utlsProxy/src"
)

// Crawler 爬虫结构体
type Crawler struct {
	pool            src.HotConnPool
	client          *src.UTlsClient
	config          *config.Config
	domainMonitor   src.DomainMonitor
	ipAccessControl src.IPAccessController
	stats           *CrawlerStats
	stopChan        chan struct{}
	wg              sync.WaitGroup
	concurrency     int
	grpcSemaphore   chan struct{} // gRPC请求并发控制信号量
	dataDir         string
	stopped         int32
	fingerprint     src.Profile
	slowIPTracker   *SlowIPTracker
	requestHeaders  map[string]string
	ipSelector      uint64
	grpcServer      *grpc.Server
	grpcListener    net.Listener
}

const maxTaskDuration = 15 * time.Second // 增加超时时间到15秒，以应对慢速IP

// CrawlerStats 爬虫统计信息
type CrawlerStats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalBytes      int64
	TotalDuration   int64
	StartedTasks    int64
	CompletedTasks  int64
	CompletedMicros int64
	// gRPC请求统计
	GRPCRequests      int64 // gRPC请求总数
	GRPCSuccess       int64 // gRPC成功请求数
	GRPCFailed        int64 // gRPC失败请求数
	GRPCRequestBytes  int64 // gRPC请求总字节数
	GRPCResponseBytes int64 // gRPC响应总字节数
	GRPCDuration      int64 // gRPC请求总耗时（微秒）
	StartTime         time.Time
}

// SlowIPTracker 用于跟踪响应缓慢的IP
type SlowIPTracker struct {
	threshold time.Duration
	counts    sync.Map
}

// NewSlowIPTracker 创建慢速IP跟踪器
func NewSlowIPTracker(threshold time.Duration) *SlowIPTracker {
	return &SlowIPTracker{
		threshold: threshold,
	}
}

// Record 如果响应耗时超过阈值则记录并返回累计次数
func (t *SlowIPTracker) Record(ip string, duration time.Duration) int64 {
	if ip == "" || duration < t.threshold {
		return 0
	}
	ptrIface, _ := t.counts.LoadOrStore(ip, new(int64))
	counterPtr := ptrIface.(*int64)
	return atomic.AddInt64(counterPtr, 1)
}

// Snapshot 返回当前慢速IP统计
func (t *SlowIPTracker) Snapshot() map[string]int64 {
	result := make(map[string]int64)
	t.counts.Range(func(key, value any) bool {
		if countPtr, ok := value.(*int64); ok {
			result[key.(string)] = atomic.LoadInt64(countPtr)
		}
		return true
	})
	return result
}

// NewCrawler 创建爬虫实例
func NewCrawler(cfg *config.Config) (*Crawler, error) {
	dataDir := "./crawler_data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	domainMonitor, err := createDomainMonitor(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建域名监控器失败: %w", err)
	}
	domainMonitor.Start()

	log.Printf("等待域名监控器为域名 [%s] 完成首次IP更新...", cfg.HotConnPool.Domain)
	if !waitForIPs(domainMonitor, cfg.HotConnPool.Domain, 30*time.Second) {
		return nil, fmt.Errorf("在30秒内未能从域名监控器获取到域名 [%s] 的任何IP地址", cfg.HotConnPool.Domain)
	}

	log.Println("正在初始化本地IP池（自动检测模式）...")
	localIPv4Pool, err := src.NewLocalIPPool(cfg.HotConnPool.LocalIPv4Addresses, "")
	if err != nil {
		log.Printf("警告: 创建IPv4 IP池失败: %v", err)
	}
	localIPv6Pool, err := src.NewLocalIPPool([]string{}, cfg.HotConnPool.LocalIPv6SubnetCIDR)
	if err != nil {
		log.Printf("警告: 创建IPv6 IP池失败: %v", err)
	}
	if localIPv4Pool == nil && localIPv6Pool == nil {
		localIPv4Pool, _ = src.NewLocalIPPool([]string{}, "")
	}

	fingerprint := getFingerprint(cfg)
	warmupPath := cfg.GetWarmupPath()
	warmupHeaders := cfg.GetWarmupHeaders()
	requestHeaders := parseHeaderList(cfg.RockTreeDataConfig.RocktreeRquestHeader)
	ipAccessControl := src.NewWhiteBlackIPPool()

	poolConfig := src.DomainConnPoolConfig{
		DomainMonitor:         domainMonitor,
		IPAccessControl:       ipAccessControl,
		LocalIPv4Pool:         localIPv4Pool,
		LocalIPv6Pool:         localIPv6Pool,
		Fingerprint:           fingerprint,
		Domain:                cfg.HotConnPool.Domain,
		Port:                  cfg.HotConnPool.Port,
		MaxConns:              cfg.HotConnPool.MaxConns,
		IdleTimeout:           cfg.HotConnPool.GetIdleTimeout(),
		WarmupPath:            warmupPath,
		WarmupMethod:          cfg.HotConnPool.WarmupMethod,
		WarmupHeaders:         warmupHeaders,
		WarmupConcurrency:     cfg.HotConnPool.WarmupConcurrency,
		BlacklistTestInterval: cfg.HotConnPool.GetBlacklistTestInterval(),
		IPRefreshInterval:     cfg.HotConnPool.GetIPRefreshInterval(),
		DialTimeout:           cfg.UTlsClient.GetDialTimeout(),
	}

	pool, err := src.NewDomainHotConnPool(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("创建热连接池失败: %w", err)
	}

	client := src.NewUTlsClient()
	client.DialTimeout = cfg.UTlsClient.GetDialTimeout()
	client.ReadTimeout = cfg.UTlsClient.GetReadTimeout()
	client.HotConnPool = pool

	// 初始化gRPC并发控制信号量，限制最大并发数为配置的并发数
	grpcConcurrency := cfg.PoolConfig.Concurrency
	if grpcConcurrency <= 0 {
		grpcConcurrency = 500 // 默认500并发
	}
	
	crawler := &Crawler{
		pool:            pool,
		client:          client,
		config:          cfg,
		domainMonitor:   domainMonitor,
		ipAccessControl: ipAccessControl,
		stats: &CrawlerStats{
			StartTime: time.Now(),
		},
		stopChan:       make(chan struct{}),
		concurrency:    cfg.PoolConfig.Concurrency,
		grpcSemaphore:  make(chan struct{}, grpcConcurrency), // 创建信号量，限制gRPC并发数
		dataDir:        dataDir,
		stopped:        0,
		fingerprint:    fingerprint,
		slowIPTracker:  NewSlowIPTracker(4 * time.Second),
		requestHeaders: requestHeaders,
	}
	
	log.Printf("[并发控制] gRPC服务器最大并发数设置为: %d", grpcConcurrency)

	return crawler, nil
}

// waitForIPs waits for the DomainMonitor to fetch IPs for a given domain.
func waitForIPs(monitor src.DomainMonitor, domain string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ipPool, found := monitor.GetDomainPool(domain)
		if found {
			ipv4Count := 0
			ipv6Count := 0
			if ipv4List, ok := ipPool["ipv4"]; ok {
				ipv4Count = len(ipv4List)
			}
			if ipv6List, ok := ipPool["ipv6"]; ok {
				ipv6Count = len(ipv6List)
			}
			if ipv4Count > 0 || ipv6Count > 0 {
				log.Printf("IP数据已就绪: 域名 [%s] -> IPv4=%d个, IPv6=%d个", domain, ipv4Count, ipv6Count)
				return true
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false
}

func createDomainMonitor(cfg *config.Config) (src.DomainMonitor, error) {
	var dnsServers []string
	dnsData, err := os.ReadFile(cfg.DNSDomain.DNSServerFilePath)
	if err != nil {
		dnsServers = cfg.DNSDomain.DefaultDNSServers
	} else {
		var dnsDB struct {
			Servers map[string]string `json:"servers"`
		}
		if err := json.Unmarshal(dnsData, &dnsDB); err != nil {
			dnsServers = cfg.DNSDomain.DefaultDNSServers
		} else {
			for _, ip := range dnsDB.Servers {
				dnsServers = append(dnsServers, ip)
			}
		}
	}
	monitorConfig := src.MonitorConfig{
		Domains:        cfg.DNSDomain.HostName,
		DNSServers:     dnsServers,
		UpdateInterval: cfg.DNSDomain.GetUpdateInterval(),
		StorageDir:     cfg.DNSDomain.StorageDir,
		StorageFormat:  cfg.DNSDomain.StorageFormat,
	}
	return src.NewRemoteIPMonitor(monitorConfig)
}

func getFingerprint(cfg *config.Config) src.Profile {
	return src.GetRandomFingerprint()
}

func parseHeaderList(list []string) map[string]string {
	headers := make(map[string]string)
	for _, header := range list {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" && value != "" {
				headers[key] = value
			}
		}
	}
	return headers
}

func (c *Crawler) Start() error {
	log.Println("=========================================")
	log.Println("启动高效爬虫系统")
	log.Println("=========================================")

	log.Println("开始预热连接池...")
	warmupStart := time.Now()
	if err := c.pool.Warmup(); err != nil {
		log.Printf("预热连接池失败: %v", err)
	} else {
		log.Printf("连接池预热完成，耗时: %v", time.Since(warmupStart))
	}

	log.Println("等待预热连接稳定...")
	time.Sleep(3 * time.Second)

	if err := c.startGRPCServer(); err != nil {
		return err
	}

	log.Println("爬虫系统已启动并等待任务")
	return nil
}

func (c *Crawler) runCrawler() {
	defer c.wg.Done()

	log.Println("开始爬取PlanetoidMetadata...")
	metadata, err := c.fetchPlanetoidMetadata()
	if err != nil {
		log.Printf("获取PlanetoidMetadata失败: %v", err)
		return
	}
	log.Printf("成功获取PlanetoidMetadata，版本: %s", metadata.Version)

	c.crawlBulkMetadataBatch(metadata, nil)

	log.Println("爬虫任务完成")
}

type PlanetoidMetadata struct {
	Version string `json:"version"`
}

func (c *Crawler) fetchPlanetoidMetadata() (*PlanetoidMetadata, error) {
	path := fmt.Sprintf("https://%s%s", c.config.RockTreeDataConfig.HostName, c.config.RockTreeDataConfig.CheckStatusPath)
	headers := make(map[string]string)
	for _, header := range c.config.RockTreeDataConfig.RocktreeRquestHeader {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	req := &src.UTlsRequest{
		WorkID:      fmt.Sprintf("planetoid-%d", time.Now().UnixNano()),
		Domain:      c.config.RockTreeDataConfig.HostName,
		Method:      "GET",
		Path:        path,
		Headers:     headers,
		Fingerprint: c.fingerprint,
		StartTime:   time.Now(),
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 || len(resp.Body) != 13 {
		return nil, fmt.Errorf("请求失败或响应体不正确")
	}
	metadata := &PlanetoidMetadata{Version: fmt.Sprintf("%x", resp.Body)}
	_ = c.saveData("PlanetoidMetadata.bin", resp.Body)
	log.Printf("成功获取PlanetoidMetadata，响应体长度: %d字节，内容: %x", len(resp.Body), resp.Body)
	return metadata, nil
}

func (c *Crawler) crawlBulkMetadataBatch(metadata *PlanetoidMetadata, semaphore chan struct{}) {
	log.Println("开始批量爬取BulkMetadata（10000条任务，使用热连接池）...")

	bulkPath := "/rt/earth/BulkMetadata/pb=!1m2!1s!2u1003"
	totalTasks := 500
	allowedIPs := c.ipAccessControl.GetAllowedIPs()
	if len(allowedIPs) == 0 {
		log.Println("警告: 白名单为空，无法执行爬取任务")
		return
	}
	poolSize := len(allowedIPs)

	log.Printf("批量爬取配置: 总任务数=%d, 白名单IP数量=%d, Worker数量=%d", totalTasks, len(allowedIPs), poolSize)

	var wg sync.WaitGroup
	taskChan := make(chan int, totalTasks)
	for i := 0; i < totalTasks; i++ {
		taskChan <- i
	}
	close(taskChan)

	processTask := func(workerID int, taskID int, workerLocalIP *string) {
		taskStart := time.Now()
		c.recordTaskStart()
		defer func() {
			c.recordTaskCompletion(time.Since(taskStart))
		}()

		attempt := 0
		for {
			if atomic.LoadInt32(&c.stopped) == 1 {
				return
			}

			attempt++
			if c.executeBulkTask(workerID, taskID, attempt, workerLocalIP, bulkPath) {
				if attempt > 1 {
					log.Printf("[Worker %d] 任务 %d 在第 %d 次尝试后成功完成", workerID, taskID, attempt)
				}
				return
			}

			time.Sleep(c.backoffDuration(attempt))
		}
	}

	for workerID := 0; workerID < poolSize; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			var workerLocalIP string
			taskCount := 0
			for taskID := range taskChan {
				taskCount++
				processTask(id, taskID, &workerLocalIP)
			}
			if taskCount > 0 {
				log.Printf("[Worker %d] [%s] 完成，共处理了 %d 个任务", id, workerLocalIP, taskCount)
			}
		}(workerID)
	}

	wg.Wait()
	c.printStats()
}

func (c *Crawler) executeBulkTask(workerID, taskID, attempt int, workerLocalIP *string, bulkPath string) bool {
	allowedIPs := c.ipAccessControl.GetAllowedIPs()
	if len(allowedIPs) == 0 {
		log.Printf("[Worker %d] 任务 %d 尝试 %d 次时白名单为空，等待可用IP...", workerID, taskID, attempt)
		return false
	}

	targetIP := allowedIPs[(taskID+attempt-1)%len(allowedIPs)]
	workID := fmt.Sprintf("bulk-%d-%d-%d", workerID, taskID, attempt)

	resp, localIP, err, duration := c.performRequestAttempt(workerID, taskID, attempt, targetIP, bulkPath, workID, maxTaskDuration)
	if err != nil {
		log.Printf("[Worker %d] 任务 %d 请求失败（第 %d 次，目标IP: %s，耗时: %v）: %v", workerID, taskID, attempt, targetIP, duration, err)
		return false
	}

	if duration > maxTaskDuration {
		log.Printf("[Worker %d] 任务 %d 超时（第 %d 次，目标IP: %s，耗时: %v）", workerID, taskID, attempt, targetIP, duration)
		return false
	}

	if resp.StatusCode == 200 {
		if workerLocalIP != nil && *workerLocalIP == "" && localIP != "" {
			*workerLocalIP = localIP
		}
		log.Printf("[Worker %d] 任务 %d 成功（第 %d 次，目标IP: %s，耗时: %v，长度: %d 字节）", workerID, taskID, attempt, targetIP, duration, len(resp.Body))
		return true
	}

	log.Printf("[Worker %d] 任务 %d 返回状态码 %d（第 %d 次，目标IP: %s，耗时: %v）", workerID, taskID, resp.StatusCode, attempt, targetIP, duration)
	return false
}

func (c *Crawler) backoffDuration(attempt int) time.Duration {
	if attempt <= 0 {
		return 200 * time.Millisecond
	}
	maxBackoff := 5 * time.Second
	base := 200 * time.Millisecond
	if attempt > 10 {
		attempt = 10
	}
	backoff := base * time.Duration(1<<uint(attempt-1))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

func (c *Crawler) recordSlowIP(ip string, duration time.Duration) {
	if c.slowIPTracker == nil || ip == "" {
		return
	}
	count := c.slowIPTracker.Record(ip, duration)
	if count > 0 && (count == 1 || count%5 == 0) {
		log.Printf("[慢速IP] 目标IP: %s, 最近耗时: %v, 累计次数: %d", ip, duration, count)
	}
}

func (c *Crawler) recordTaskStart() {
	atomic.AddInt64(&c.stats.StartedTasks, 1)
}

func (c *Crawler) recordTaskCompletion(duration time.Duration) {
	atomic.AddInt64(&c.stats.CompletedTasks, 1)
	atomic.AddInt64(&c.stats.CompletedMicros, duration.Microseconds())
}

func (c *Crawler) saveData(filename string, data []byte) error {
	filePath := filepath.Join(c.dataDir, filename)
	return os.WriteFile(filePath, data, 0644)
}

func (c *Crawler) printStats() {
	stats := c.stats

	total := atomic.LoadInt64(&stats.TotalRequests)
	success := atomic.LoadInt64(&stats.SuccessRequests)
	failed := atomic.LoadInt64(&stats.FailedRequests)
	bytes := atomic.LoadInt64(&stats.TotalBytes)
	totalMicros := atomic.LoadInt64(&stats.TotalDuration)

	started := atomic.LoadInt64(&stats.StartedTasks)
	completed := atomic.LoadInt64(&stats.CompletedTasks)
	completedMicros := atomic.LoadInt64(&stats.CompletedMicros)

	avgReqDuration := time.Duration(0)
	if total > 0 {
		avgReqDuration = time.Duration(totalMicros/total) * time.Microsecond
	}

	avgTaskDuration := time.Duration(0)
	if completed > 0 {
		avgTaskDuration = time.Duration(completedMicros/completed) * time.Microsecond
	}

	elapsed := time.Since(stats.StartTime)

	log.Printf("[统计] 运行时长=%v, 请求总数=%d (成功=%d, 失败=%d), 平均请求耗时=%v, 累计字节=%d",
		elapsed, total, success, failed, avgReqDuration, bytes)

	if started > 0 {
		log.Printf("[统计] 任务派发=%d, 已完成=%d, 平均任务耗时=%v, 未完成=%d",
			started, completed, avgTaskDuration, started-completed)
	}

	// gRPC请求统计
	grpcTotal := atomic.LoadInt64(&stats.GRPCRequests)
	grpcSuccess := atomic.LoadInt64(&stats.GRPCSuccess)
	grpcFailed := atomic.LoadInt64(&stats.GRPCFailed)
	grpcReqBytes := atomic.LoadInt64(&stats.GRPCRequestBytes)
	grpcRespBytes := atomic.LoadInt64(&stats.GRPCResponseBytes)
	grpcTotalMicros := atomic.LoadInt64(&stats.GRPCDuration)
	
	if grpcTotal > 0 {
		avgGRPCDuration := time.Duration(0)
		if grpcTotal > 0 {
			avgGRPCDuration = time.Duration(grpcTotalMicros/grpcTotal) * time.Microsecond
		}
		log.Printf("[统计] gRPC请求总数=%d (成功=%d, 失败=%d), 平均耗时=%v, 请求流量=%d字节, 响应流量=%d字节, 总流量=%d字节",
			grpcTotal, grpcSuccess, grpcFailed, avgGRPCDuration, grpcReqBytes, grpcRespBytes, grpcReqBytes+grpcRespBytes)
	}
}

func (c *Crawler) Stop() {
	if !atomic.CompareAndSwapInt32(&c.stopped, 0, 1) {
		return
	}
	log.Println("正在停止爬虫...")
	close(c.stopChan)

	if c.grpcServer != nil {
		c.grpcServer.GracefulStop()
		c.grpcServer = nil
	}

	c.wg.Wait()

	if c.grpcListener != nil {
		_ = c.grpcListener.Close()
		c.grpcListener = nil
	}

	c.pool.Close()
	c.domainMonitor.Stop()
	c.printStats()
	log.Println("爬虫已停止")
}

func main() {
	cfg, err := config.LoadConfig("./config/config.toml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	crawler, err := NewCrawler(cfg)
	if err != nil {
		log.Fatalf("创建爬虫失败: %v", err)
	}
	defer crawler.Stop()

	if err := crawler.Start(); err != nil {
		log.Fatalf("启动爬虫失败: %v", err)
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				crawler.printStats()
			case <-crawler.stopChan:
				return
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("收到退出信号，正在关闭...")
	crawler.Stop()
}
