package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"utlsProxy/config"
	"utlsProxy/src"
)

// Crawler 爬虫结构体
type Crawler struct {
	pool          src.HotConnPool   // 热连接池
	client        *src.UTlsClient   // UTLS客户端（用于发送请求）
	config        *config.Config    // 配置信息
	domainMonitor src.DomainMonitor // 域名IP监控器
	stats         *CrawlerStats     // 统计信息
	stopChan      chan struct{}     // 停止信号
	wg            sync.WaitGroup    // 等待组
	concurrency   int               // 并发数
	dataDir       string            // 数据存储目录
	stopped       int32             // 停止标志（使用原子操作）
	fingerprint   src.Profile       // TLS指纹（用于重试时创建新连接）
}

// CrawlerStats 爬虫统计信息
type CrawlerStats struct {
	TotalRequests   int64 // 总请求数
	SuccessRequests int64 // 成功请求数
	FailedRequests  int64 // 失败请求数
	TotalBytes      int64 // 总字节数
	StartTime       time.Time
	mutex           sync.RWMutex
}

// NewCrawler 创建爬虫实例
func NewCrawler(cfg *config.Config) (*Crawler, error) {
	// 创建数据存储目录
	dataDir := "./crawler_data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 创建域名监控器
	domainMonitor, err := createDomainMonitor(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建域名监控器失败: %w", err)
	}

	// 启动域名监控
	domainMonitor.Start()

	// 等待域名监控器完成首次更新（确保有IP数据）
	log.Println("等待域名监控器完成首次IP更新...")

	// 立即检查IP数据（域名监控器启动时会立即执行一次更新）
	ipPool, found := domainMonitor.GetDomainPool(cfg.HotConnPool.Domain)
	ipv4Count := 0
	ipv6Count := 0
	if found {
		if ipv4List, ok := ipPool["ipv4"]; ok {
			ipv4Count = len(ipv4List)
		}
		if ipv6List, ok := ipPool["ipv6"]; ok {
			ipv6Count = len(ipv6List)
		}
	}

	// 如果已有数据，直接使用；否则等待首次更新完成
	if ipv4Count > 0 || ipv6Count > 0 {
		log.Printf("IP数据已就绪: IPv4=%d个, IPv6=%d个", ipv4Count, ipv6Count)
	} else {
		// 轮询检查IP数据，最多等待30秒
		log.Println("等待域名监控器完成首次IP更新...")
		maxWaitTime := 30 * time.Second
		checkInterval := 1 * time.Second
		waited := 0 * time.Second

		for waited < maxWaitTime {
			ipPool, found = domainMonitor.GetDomainPool(cfg.HotConnPool.Domain)
			if found {
				ipv4Count = 0
				ipv6Count = 0
				if ipv4List, ok := ipPool["ipv4"]; ok {
					ipv4Count = len(ipv4List)
				}
				if ipv6List, ok := ipPool["ipv6"]; ok {
					ipv6Count = len(ipv6List)
				}
				if ipv4Count > 0 || ipv6Count > 0 {
					log.Printf("IP数据已就绪: IPv4=%d个, IPv6=%d个", ipv4Count, ipv6Count)
					break
				}
			}
			time.Sleep(checkInterval)
			waited += checkInterval
			if int(waited.Seconds())%5 == 0 && int(waited.Seconds()) > 0 {
				log.Printf("等待IP数据... (已等待 %v)", waited)
			}
		}
	}

	// 最终检查并记录
	if ipv4Count == 0 && ipv6Count == 0 {
		log.Printf("警告: 域名监控器尚未获取到IP数据，但连接池会尝试从文件加载")
	}

	// 创建本地IP池（自动检测模式）
	// 如果配置中提供了IP地址，则使用配置的；否则自动检测系统中可用的IP地址
	log.Println("正在初始化本地IP池（自动检测模式）...")

	var localIPv4Pool src.IPPool
	var localIPv6Pool src.IPPool

	// 创建IPv4 IP池（如果配置为空，会自动检测）
	localIPv4Pool, err = src.NewLocalIPPool(
		cfg.HotConnPool.LocalIPv4Addresses,
		"", // IPv6子网留空，先创建IPv4池
	)
	if err != nil {
		log.Printf("警告: 创建IPv4 IP池失败: %v", err)
		localIPv4Pool = nil
	}

	// 创建IPv6 IP池（如果配置为空，会自动检测）
	localIPv6Pool, err = src.NewLocalIPPool(
		[]string{}, // IPv4地址留空，只关注IPv6
		cfg.HotConnPool.LocalIPv6SubnetCIDR,
	)
	if err != nil {
		log.Printf("警告: 创建IPv6 IP池失败: %v (将使用IPv4模式)", err)
		localIPv6Pool = nil
	}

	// 如果两个IP池都创建失败，至少创建一个空的IPv4池用于降级（会自动检测）
	if localIPv4Pool == nil && localIPv6Pool == nil {
		log.Println("警告: 所有本地IP池创建失败，尝试使用自动检测模式")
		// 创建一个空的IPv4池，会自动检测系统中可用的IP地址
		localIPv4Pool, err = src.NewLocalIPPool([]string{}, "")
		if err != nil {
			log.Printf("警告: 自动检测IP池也失败: %v，将使用系统默认路由", err)
			localIPv4Pool = nil
		}
	}

	// 获取TLS指纹
	fingerprint := getFingerprint(cfg)

	// 获取预热路径和请求头
	warmupPath := cfg.GetWarmupPath()
	warmupHeaders := cfg.GetWarmupHeaders()

	// 创建热连接池配置
	poolConfig := src.DomainConnPoolConfig{
		DomainMonitor:         domainMonitor,
		IPAccessControl:       src.NewWhiteBlackIPPool(),
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

	// 创建热连接池
	pool, err := src.NewDomainHotConnPool(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("创建热连接池失败: %w", err)
	}

	// 连接池创建后会调用refreshTargetIPList，但此时可能还没有IP数据
	// 等待一下，然后手动触发一次刷新
	time.Sleep(2 * time.Second)
	// 注意：这里无法直接调用refreshTargetIPList，因为它不是公开方法
	// 但连接池的后台任务会在IPRefreshInterval后自动刷新
	// 为了立即生效，我们等待域名监控器完成更新后再创建连接池

	// 创建UTLS客户端，并设置热连接池
	client := src.NewUTlsClient()
	client.DialTimeout = cfg.UTlsClient.GetDialTimeout()
	client.ReadTimeout = cfg.UTlsClient.GetReadTimeout()
	client.HotConnPool = pool // 设置热连接池，UTlsClient.Do会自动使用连接池

	crawler := &Crawler{
		pool:          pool,
		client:        client,
		config:        cfg,
		domainMonitor: domainMonitor,
		stats: &CrawlerStats{
			StartTime: time.Now(),
		},
		stopChan:    make(chan struct{}),
		concurrency: cfg.PoolConfig.Concurrency,
		dataDir:     dataDir,
		stopped:     0,
		fingerprint: fingerprint, // 保存指纹，用于重试时创建新连接
	}

	return crawler, nil
}

// createDomainMonitor 创建域名监控器
func createDomainMonitor(cfg *config.Config) (src.DomainMonitor, error) {
	// 读取DNS服务器配置
	var dnsServers []string
	dnsData, err := os.ReadFile(cfg.DNSDomain.DNSServerFilePath)
	if err != nil {
		log.Printf("无法读取DNS服务器文件，使用默认DNS服务器")
		dnsServers = cfg.DNSDomain.DefaultDNSServers
	} else {
		var dnsDB struct {
			Servers map[string]string `json:"servers"`
		}
		if err := json.Unmarshal(dnsData, &dnsDB); err != nil {
			log.Printf("解析DNS服务器文件失败，使用默认DNS服务器")
			dnsServers = cfg.DNSDomain.DefaultDNSServers
		} else {
			uniqueServers := make(map[string]bool)
			for _, ip := range dnsDB.Servers {
				if !uniqueServers[ip] {
					uniqueServers[ip] = true
					dnsServers = append(dnsServers, ip)
				}
			}
		}
	}

	// 创建监控器配置
	monitorConfig := src.MonitorConfig{
		Domains:        cfg.DNSDomain.HostName,
		DNSServers:     dnsServers,
		IPInfoToken:    cfg.IPInfo.Token,
		UpdateInterval: cfg.DNSDomain.GetUpdateInterval(),
		StorageDir:     cfg.DNSDomain.StorageDir,
		StorageFormat:  cfg.DNSDomain.StorageFormat,
	}

	return src.NewRemoteIPMonitor(monitorConfig)
}

// getFingerprint 获取TLS指纹
func getFingerprint(cfg *config.Config) src.Profile {
	if cfg.HotConnPool.FingerprintName != "" {
		library := src.NewLibrary()
		profile, err := library.ProfileByName(cfg.HotConnPool.FingerprintName)
		if err == nil {
			return *profile
		}
	}
	return src.GetRandomFingerprint()
}

// Start 启动爬虫
func (c *Crawler) Start() error {
	log.Println("=========================================")
	log.Println("启动高效爬虫系统")
	log.Println("=========================================")

	// 预热连接池
	log.Println("开始预热连接池...")
	warmupStart := time.Now()
	if err := c.pool.Warmup(); err != nil {
		log.Printf("预热连接池失败: %v", err)
	} else {
		log.Printf("连接池预热完成，耗时: %v", time.Since(warmupStart))
	}

	// 等待一段时间让预热完成
	log.Println("等待预热连接稳定...")
	time.Sleep(3 * time.Second)

	// 启动爬取任务
	c.wg.Add(1)
	go c.runCrawler()

	log.Println("爬虫系统已启动")
	return nil
}

// runCrawler 运行爬虫主循环
func (c *Crawler) runCrawler() {
	defer c.wg.Done()

	// 创建信号量控制并发
	semaphore := make(chan struct{}, c.concurrency)

	// 首先获取PlanetoidMetadata（数据库入口）
	log.Println("开始爬取PlanetoidMetadata...")
	metadata, err := c.fetchPlanetoidMetadata()
	if err != nil {
		log.Printf("获取PlanetoidMetadata失败: %v", err)
		return
	}

	log.Printf("成功获取PlanetoidMetadata，版本: %s", metadata.Version)

	// 根据metadata爬取数据
	// 这里可以根据实际需求实现具体的爬取逻辑
	// 例如：爬取BulkMetadata、NodeData等

	// 批量爬取BulkMetadata（1000条任务）
	c.crawlBulkMetadataBatch(metadata, semaphore)

	// 等待所有任务完成
	for i := 0; i < c.concurrency; i++ {
		semaphore <- struct{}{}
	}

	log.Println("爬虫任务完成")
}

// PlanetoidMetadata Planetoid元数据结构
type PlanetoidMetadata struct {
	Version string `json:"version"`
	// 其他字段根据实际响应添加
}

// fetchPlanetoidMetadata 获取PlanetoidMetadata
func (c *Crawler) fetchPlanetoidMetadata() (*PlanetoidMetadata, error) {
	path := fmt.Sprintf("https://%s%s", c.config.RockTreeDataConfig.HostName, c.config.RockTreeDataConfig.CheckStatusPath)

	// 解析请求头
	headers := make(map[string]string)
	for _, header := range c.config.RockTreeDataConfig.RocktreeRquestHeader {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			headers[key] = value
		}
	}

	// 使用UTlsClient.Do方法执行请求（会自动使用热连接池）
	req := &src.UTlsRequest{
		WorkID:      fmt.Sprintf("planetoid-%d", time.Now().UnixNano()),
		Domain:      c.config.RockTreeDataConfig.HostName,
		Method:      "GET",
		Path:        path,
		Headers:     headers,
		Body:        nil,
		DomainIP:    "",            // 让UTlsClient从连接池获取IP
		Fingerprint: c.fingerprint, // 使用爬虫的指纹（重试时会用到）
		StartTime:   time.Now(),
	}

	resp, err := c.client.Do(req)
	if err != nil {
		atomic.AddInt64(&c.stats.FailedRequests, 1)
		atomic.AddInt64(&c.stats.TotalRequests, 1)
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	if resp.StatusCode != 200 {
		atomic.AddInt64(&c.stats.FailedRequests, 1)
		atomic.AddInt64(&c.stats.TotalRequests, 1)
		return nil, fmt.Errorf("请求失败，状态码: %d", resp.StatusCode)
	}

	// 验证响应体长度（PlanetoidMetadata的标准响应长度是13字节）
	if len(resp.Body) != 13 {
		atomic.AddInt64(&c.stats.FailedRequests, 1)
		atomic.AddInt64(&c.stats.TotalRequests, 1)
		return nil, fmt.Errorf("响应体长度不正确，期望13字节，实际%d字节", len(resp.Body))
	}

	// PlanetoidMetadata返回的是13字节的二进制数据，不是JSON
	// 直接使用响应体，不进行JSON解析
	metadata := &PlanetoidMetadata{
		Version: fmt.Sprintf("%x", resp.Body), // 将二进制数据转换为十六进制字符串作为版本号
	}

	// 保存数据
	if err := c.saveData("PlanetoidMetadata.bin", resp.Body); err != nil {
		log.Printf("保存数据失败: %v", err)
	}

	atomic.AddInt64(&c.stats.SuccessRequests, 1)
	atomic.AddInt64(&c.stats.TotalRequests, 1)
	atomic.AddInt64(&c.stats.TotalBytes, int64(len(resp.Body)))

	log.Printf("成功获取PlanetoidMetadata，响应体长度: %d字节，内容: %x", len(resp.Body), resp.Body)

	return metadata, nil
}

// crawlBulkMetadata 爬取BulkMetadata（保留原方法，用于兼容）
func (c *Crawler) crawlBulkMetadata(metadata *PlanetoidMetadata, semaphore chan struct{}) {
	c.crawlBulkMetadataBatch(metadata, semaphore)
}

// crawlBulkMetadataBatch 批量爬取BulkMetadata（1000条任务，使用热连接池）
func (c *Crawler) crawlBulkMetadataBatch(metadata *PlanetoidMetadata, semaphore chan struct{}) {
	log.Println("开始批量爬取BulkMetadata（1000条任务，使用热连接池）...")

	// 解析请求头
	headers := make(map[string]string)
	for _, header := range c.config.RockTreeDataConfig.RocktreeRquestHeader {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			headers[key] = value
		}
	}

	// 创建1000个任务，每个任务使用相同的路径
	// 路径: /rt/earth/BulkMetadata/pb=!1m2!1s!2u1003
	bulkPath := "/rt/earth/BulkMetadata/pb=!1m2!1s!2u1003"
	totalTasks := 10000

	// 确保并发数不超过连接池大小，并且每个连接至少有一条任务
	// 如果连接池有N个连接，我们设置并发数为N，确保每个连接都有任务
	// 如果任务数少于连接数，则使用任务数作为并发数
	poolSize := c.config.HotConnPool.MaxConns
	if poolSize > totalTasks {
		poolSize = totalTasks
	}
	if poolSize < 1 {
		poolSize = c.concurrency // 降级到配置的并发数
	}

	log.Printf("批量爬取配置: 总任务数=%d, 连接池大小=%d, 并发数=%d", totalTasks, c.config.HotConnPool.MaxConns, poolSize)

	var wg sync.WaitGroup
	taskChan := make(chan int, totalTasks)

	// 将所有任务放入通道
	for i := 0; i < totalTasks; i++ {
		taskChan <- i
	}
	close(taskChan)

	// 启动worker goroutines，每个worker使用热连接池
	for i := 0; i < poolSize; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// 每个worker处理多个任务，确保每个连接都有任务
			taskCount := 0
			for taskID := range taskChan {
				taskCount++
				semaphore <- struct{}{} // 获取信号量

				// 构建完整URL
				path := fmt.Sprintf("https://%s%s", c.config.RockTreeDataConfig.HostName, bulkPath)

				// 使用UTlsClient.Do方法执行请求（会自动使用热连接池）
				req := &src.UTlsRequest{
					WorkID:      fmt.Sprintf("bulk-%d-%d-%d", workerID, taskID, time.Now().UnixNano()),
					Domain:      c.config.RockTreeDataConfig.HostName,
					Method:      "GET",
					Path:        path,
					Headers:     headers,
					Body:        nil,
					DomainIP:    "",            // 让UTlsClient从连接池获取IP
					Fingerprint: c.fingerprint, // 使用爬虫的指纹（重试时会用到）
					StartTime:   time.Now(),
				}

				resp, err := c.client.Do(req)
				<-semaphore // 释放信号量

				if err != nil {
					log.Printf("[Worker %d] 任务 %d 请求失败: %v", workerID, taskID, err)
					atomic.AddInt64(&c.stats.FailedRequests, 1)
					atomic.AddInt64(&c.stats.TotalRequests, 1)
					continue
				}

				if resp.StatusCode == 200 {
					// 保存数据
					filename := fmt.Sprintf("BulkMetadata_%d_%d.bin", workerID, taskID)
					if err := c.saveData(filename, resp.Body); err != nil {
						log.Printf("[Worker %d] 任务 %d 保存数据失败: %v", workerID, taskID, err)
					}
					atomic.AddInt64(&c.stats.SuccessRequests, 1)
					atomic.AddInt64(&c.stats.TotalBytes, int64(len(resp.Body)))
					log.Printf("[Worker %d] 任务 %d 成功，响应长度: %d字节", workerID, taskID, len(resp.Body))
				} else {
					log.Printf("[Worker %d] 任务 %d 失败，状态码: %d", workerID, taskID, resp.StatusCode)
					atomic.AddInt64(&c.stats.FailedRequests, 1)
				}

				atomic.AddInt64(&c.stats.TotalRequests, 1)
			}

			log.Printf("[Worker %d] 完成，处理了 %d 个任务", workerID, taskCount)
		}(i)
	}

	wg.Wait()
	log.Printf("批量爬取完成: 总任务=%d, 成功=%d, 失败=%d",
		atomic.LoadInt64(&c.stats.TotalRequests),
		atomic.LoadInt64(&c.stats.SuccessRequests),
		atomic.LoadInt64(&c.stats.FailedRequests))
}

// saveData 保存数据到文件
func (c *Crawler) saveData(filename string, data []byte) error {
	filePath := filepath.Join(c.dataDir, filename)
	return os.WriteFile(filePath, data, 0644)
}

// printStats 打印统计信息
func (c *Crawler) printStats() {
	c.stats.mutex.RLock()
	defer c.stats.mutex.RUnlock()

	duration := time.Since(c.stats.StartTime)
	total := atomic.LoadInt64(&c.stats.TotalRequests)
	success := atomic.LoadInt64(&c.stats.SuccessRequests)
	failed := atomic.LoadInt64(&c.stats.FailedRequests)
	bytes := atomic.LoadInt64(&c.stats.TotalBytes)

	log.Println("=========================================")
	log.Println("爬虫统计信息")
	log.Println("=========================================")
	log.Printf("运行时间: %v", duration)
	log.Printf("总请求数: %d", total)
	log.Printf("成功请求: %d", success)
	log.Printf("失败请求: %d", failed)
	if total > 0 {
		log.Printf("成功率: %.2f%%", float64(success)/float64(total)*100)
	} else {
		log.Printf("成功率: 0.00%%")
	}
	log.Printf("总数据量: %.2f MB", float64(bytes)/(1024*1024))
	if duration.Seconds() > 0 {
		log.Printf("平均速度: %.2f KB/s", float64(bytes)/(1024)/duration.Seconds())
	} else {
		log.Printf("平均速度: 0.00 KB/s")
	}
	log.Println("=========================================")
}

// Stop 停止爬虫
func (c *Crawler) Stop() {
	// 使用原子操作确保只执行一次
	if !atomic.CompareAndSwapInt32(&c.stopped, 0, 1) {
		return // 已经停止过了
	}

	log.Println("正在停止爬虫...")

	// 安全关闭 stopChan
	select {
	case <-c.stopChan:
		// 已经关闭
	default:
		close(c.stopChan)
	}

	c.wg.Wait()
	c.pool.Close()
	c.domainMonitor.Stop()
	c.printStats()
	log.Println("爬虫已停止")
}

func main() {
	// 加载配置
	cfg, err := config.LoadConfig("./config/config.toml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建爬虫
	crawler, err := NewCrawler(cfg)
	if err != nil {
		log.Fatalf("创建爬虫失败: %v", err)
	}
	defer crawler.Stop()

	// 启动爬虫
	if err := crawler.Start(); err != nil {
		log.Fatalf("启动爬虫失败: %v", err)
	}

	// 定期打印统计信息
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

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("收到退出信号，正在关闭...")
	crawler.Stop()
}
