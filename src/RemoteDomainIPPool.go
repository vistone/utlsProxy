package src // Package src 定义src包

import ( // 导入所需的标准库和第三方库
	"bytes"         // 用于操作字节缓冲区
	"encoding/json" // 用于JSON编码解码
	"fmt"           // 用于格式化输入输出
	"io"            // 用于基础IO操作
	"net"           // 用于网络相关功能
	"net/http"      // 用于HTTP客户端功能
	"os"            // 用于操作系统功能
	"path/filepath" // 用于文件路径操作
	"strings"       // 用于字符串操作
	"sync"          // 用于同步原语如互斥锁
	"time"          // 用于时间处理

	"github.com/BurntSushi/toml" // 用于TOML格式解析
	"github.com/miekg/dns"       // 用于DNS查询功能
	"gopkg.in/yaml.v3"           // 用于YAML格式解析
)

// --- 1. 接口定义 ---

// DomainMonitor 定义了域名IP监控组件的行为契约。
// 它不仅能被启动和停止，还能安全地查询其内部缓存的最新数据。
type DomainMonitor interface { // 定义DomainMonitor接口
	// Start 启动监视器。它会立即执行一次，然后按指定间隔重复执行。
	Start() // 启动监视器方法
	// Stop 优雅地停止监视器。
	Stop() // 停止监视器方法
	// GetDomainPool 获取指定域名的最新IP池数据。
	// 返回的数据是深拷贝，可以安全地被调用方修改。
	// 如果找不到该域名的数据，返回的 bool 值为 false。
	GetDomainPool(domain string) (map[string][]IPRecord, bool) // 获取域名IP池方法
}

// --- 2. 数据结构定义 ---

// IPRecord 存储一个IP地址及其从外部API获取的详细信息。
type IPRecord struct { // 定义IP记录结构体
	IP     string          `json:"ip"`      // IP地址
	IPInfo *IPInfoResponse `json:"ip_info"` // IP信息详情
}

// IPInfoAS 映射了 ipinfo.io API 的ASN信息。
type IPInfoAS struct { // 定义IP信息AS结构体
	ASN    string `json:"asn"`    // ASN编号
	Name   string `json:"name"`   // 名称
	Domain string `json:"domain"` // 域名
	Type   string `json:"type"`   // 类型
}

// IPInfoGeo 映射了 ipinfo.io API 的地理位置信息。
type IPInfoGeo struct { // 定义IP地理位置信息结构体
	City          string  `json:"city"`           // 城市
	Region        string  `json:"region"`         // 区域
	RegionCode    string  `json:"region_code"`    // 区域代码
	Country       string  `json:"country"`        // 国家
	CountryCode   string  `json:"country_code"`   // 国家代码
	Continent     string  `json:"continent"`      // 大洲
	ContinentCode string  `json:"continent_code"` // 大洲代码
	Latitude      float64 `json:"latitude"`       // 纬度
	Longitude     float64 `json:"longitude"`      // 经度
	Timezone      string  `json:"timezone"`       // 时区
	PostalCode    string  `json:"postal_code"`    // 邮政编码
}

// IPInfoResponse 映射了 ipinfo.io API 的完整响应。
type IPInfoResponse struct { // 定义IP信息响应结构体
	IP          string     `json:"ip"`                     // IP地址
	Hostname    string     `json:"hostname,omitempty"`     // 主机名
	City        string     `json:"city,omitempty"`         // 城市
	Region      string     `json:"region,omitempty"`       // 区域
	Country     string     `json:"country,omitempty"`      // 国家
	CountryCode string     `json:"country_code,omitempty"` // 国家代码
	Loc         string     `json:"loc,omitempty"`          // 位置信息
	Org         string     `json:"org,omitempty"`          // 组织信息
	Postal      string     `json:"postal,omitempty"`       // 邮政编码
	Timezone    string     `json:"timezone,omitempty"`     // 时区
	Anycast     bool       `json:"anycast,omitempty"`      // Anycast标记
	Geo         *IPInfoGeo `json:"geo,omitempty"`          // 地理位置信息
	AS          *IPInfoAS  `json:"as,omitempty"`           // AS信息
	IsAnonymous bool       `json:"is_anonymous,omitempty"` // 是否匿名
	IsAnycast   bool       `json:"is_anycast,omitempty"`   // 是否Anycast
	IsHosting   bool       `json:"is_hosting,omitempty"`   // 是否托管
	IsMobile    bool       `json:"is_mobile,omitempty"`    // 是否移动设备
	IsSatellite bool       `json:"is_satellite,omitempty"` // 是否卫星连接
}

// --- 3. 核心监控组件 ---

// MonitorConfig 用于配置 RemoteIPMonitor 的所有参数。
type MonitorConfig struct { // 定义监视器配置结构体
	Domains        []string      // 要监视的域名列表
	DNSServers     []string      // 用于解析的DNS服务器列表
	IPInfoToken    string        // ipinfo.io 的 API Token
	UpdateInterval time.Duration // 更新间隔
	StorageDir     string        // 存储结果文件的目录
	StorageFormat  string        // 存储格式 (json, yaml, toml)
}

// remoteIPMonitor 是 DomainMonitor 接口的一个具体实现。
type remoteIPMonitor struct { // 定义远程IP监视器结构体
	config     MonitorConfig                    // 监视器配置
	ticker     *time.Ticker                     // 定时器
	stopChan   chan struct{}                    // 停止信号通道
	httpClient *http.Client                     // HTTP客户端
	mu         sync.RWMutex                     // 读写互斥锁
	latestData map[string]map[string][]IPRecord // 最新数据缓存
}

// 确保 remoteIPMonitor 实现了 DomainMonitor 接口 (编译时检查)
var _ DomainMonitor = (*remoteIPMonitor)(nil) // 编译时接口实现检查

// NewRemoteIPMonitor 创建并验证一个新的监视器实例。
// 参数：config - 监视器配置
// 返回值：DomainMonitor接口实例和错误信息
func NewRemoteIPMonitor(config MonitorConfig) (DomainMonitor, error) {
	if config.UpdateInterval < 1*time.Minute { // 如果更新间隔小于1分钟
		fmt.Println("警告: 更新间隔小于1分钟，可能会导致API调用过于频繁。") // 输出警告信息
	}
	if len(config.Domains) == 0 { // 如果域名列表为空
		return nil, fmt.Errorf("域名列表不能为空") // 返回错误
	}
	if len(config.DNSServers) == 0 { // 如果DNS服务器列表为空
		fmt.Println("警告: 未提供DNS服务器列表，将使用默认服务器 [8.8.8.8, 1.1.1.1]。") // 输出警告信息
		config.DNSServers = []string{"8.8.8.8", "1.1.1.1"}          // 使用默认DNS服务器
	}
	if config.StorageDir == "" { // 如果存储目录为空
		config.StorageDir = "." // 默认为当前目录
	}

	// 创建一个可复用的、并发安全的HTTP客户端
	httpClient := &http.Client{ // 创建HTTP客户端
		Timeout: 10 * time.Second, // 设置超时时间
		Transport: &http.Transport{ // 设置传输层配置
			MaxIdleConns:        100,              // 最大空闲连接数
			MaxIdleConnsPerHost: 10,               // 每个主机最大空闲连接数
			IdleConnTimeout:     90 * time.Second, // 空闲连接超时时间
		},
	}

	return &remoteIPMonitor{ // 返回远程IP监视器实例
		config:     config,                                 // 设置配置
		stopChan:   make(chan struct{}),                    // 创建停止信号通道
		httpClient: httpClient,                             // 设置HTTP客户端
		latestData: make(map[string]map[string][]IPRecord), // 初始化最新数据缓存
	}, nil
}

// Start 实现了 DomainMonitor 接口。
func (m *remoteIPMonitor) Start() { // 实现Start方法
	fmt.Println("域名IP监视器已启动...")                       // 输出启动日志
	m.ticker = time.NewTicker(m.config.UpdateInterval) // 创建定时器
	go m.run()                                         // 启动后台运行goroutine
}

// Stop 实现了 DomainMonitor 接口。
func (m *remoteIPMonitor) Stop() { // 实现Stop方法
	fmt.Println("正在停止域名IP监视器...") // 输出停止日志
	if m.ticker != nil {          // 如果定时器不为空
		m.ticker.Stop() // 停止定时器
	}
	close(m.stopChan)          // 关闭停止信号通道
	fmt.Println("域名IP监视器已停止。") // 输出停止完成日志
}

// GetDomainPool 实现了 DomainMonitor 接口。
// 参数：domain - 域名
// 返回值：域名IP池数据和是否存在标记
func (m *remoteIPMonitor) GetDomainPool(domain string) (map[string][]IPRecord, bool) {
	m.mu.RLock()                        // 加读锁
	defer m.mu.RUnlock()                // 延迟解锁
	pool, found := m.latestData[domain] // 获取域名数据
	if !found {                         // 如果未找到
		return nil, false // 返回nil和false
	}
	// 返回深拷贝以保证线程安全
	copiedPool := make(map[string][]IPRecord, len(pool)) // 创建拷贝池
	for key, records := range pool {                     // 遍历数据
		copiedRecords := make([]IPRecord, len(records)) // 创建记录拷贝
		copy(copiedRecords, records)                    // 拷贝记录
		copiedPool[key] = copiedRecords                 // 设置拷贝池数据
	}
	return copiedPool, true // 返回拷贝池和true
}

// run 是在后台goroutine中运行的主循环。
func (m *remoteIPMonitor) run() { // 运行方法
	m.updateAllDomains() // 更新所有域名
	for {                // 无限循环
		select {
		case <-m.ticker.C: // 定时器触发
			m.updateAllDomains() // 更新所有域名
		case <-m.stopChan: // 收到停止信号
			return // 退出循环
		}
	}
}

// updateAllDomains 为配置中的每个域名启动一个隔离的、并行的更新流程。
func (m *remoteIPMonitor) updateAllDomains() { // 更新所有域名方法
	fmt.Printf("[%s] 开始按域名隔离的累加式增量更新...\n", time.Now().Format(time.Kitchen)) // 输出更新开始日志

	var wg sync.WaitGroup                     // 声明等待组
	for _, domain := range m.config.Domains { // 遍历域名列表
		wg.Add(1)           // 增加等待计数
		go func(d string) { // 启动goroutine处理单个域名
			defer wg.Done()          // 延迟减少等待计数
			m.processSingleDomain(d) // 处理单个域名
		}(domain) // 传递域名参数
	}
	wg.Wait()                                                       // 等待所有域名处理完成
	fmt.Printf("[%s] 所有域名处理完成。\n", time.Now().Format(time.Kitchen)) // 输出处理完成日志
}

// processSingleDomain 负责处理单个域名的完整更新逻辑。
// 参数：domain - 要处理的域名
func (m *remoteIPMonitor) processSingleDomain(domain string) { // 处理单个域名方法
	// 1. 构建此域名的专属文件路径
	fileName := strings.ReplaceAll(domain, ".", "_") + "." + m.config.StorageFormat // 构造文件名
	filePath := filepath.Join(m.config.StorageDir, fileName)                        // 构造文件路径

	// 2. 加载此域名的历史数据
	domainPool := m.loadDomainData(filePath) // 加载域名数据
	domainKnownIPs := make(map[string]bool)  // 创建已知IP映射
	for _, rec := range domainPool["ipv4"] { // 遍历IPv4记录
		domainKnownIPs[rec.IP] = true // 标记为已知IP
	}
	for _, rec := range domainPool["ipv6"] { // 遍历IPv6记录
		domainKnownIPs[rec.IP] = true // 标记为已知IP
	}
	fmt.Printf("域名 [%s]: 从 %s 加载了 %d 个已知IP。\n", domain, filePath, len(domainKnownIPs)) // 输出加载日志

	// 3. 并发解析此域名的当前IP
	currentIPv4s, currentIPv6s, _ := m.resolveDomainConcurrently(domain) // 并发解析域名IP
	currentIPs := append(currentIPv4s, currentIPv6s...)                  // 合并IPv4和IPv6地址

	// 4. 找出只属于该域名自己的新IP
	var newIPsForThisDomain []string // 声明新IP列表
	for _, ip := range currentIPs {  // 遍历当前IP
		if !domainKnownIPs[ip] { // 如果是未知IP
			newIPsForThisDomain = append(newIPsForThisDomain, ip) // 添加到新IP列表
		}
	}
	fmt.Printf("域名 [%s]: 发现 %d 个新IP需要查询信息。\n", domain, len(newIPsForThisDomain)) // 输出发现新IP日志

	// 5. 只为这些新IP查询信息
	if len(newIPsForThisDomain) > 0 { // 如果有新IP
		var wgIPInfo sync.WaitGroup              // 声明IP信息等待组
		var muDomainPool sync.Mutex              // 使用一个专用于此goroutine的锁来保护domainPool的并发追加
		for _, ip := range newIPsForThisDomain { // 遍历新IP
			wgIPInfo.Add(1)         // 增加等待计数
			go func(ipStr string) { // 启动goroutine查询IP信息
				defer wgIPInfo.Done()             // 延迟减少等待计数
				info, err := m.fetchIPInfo(ipStr) // 获取IP信息
				if err == nil {                   // 如果获取成功
					record := IPRecord{IP: ipStr, IPInfo: info}   // 创建IP记录
					isIPv4 := net.ParseIP(record.IP).To4() != nil // 判断是否为IPv4

					muDomainPool.Lock() // 加锁
					if isIPv4 {         // 如果是IPv4
						domainPool["ipv4"] = append(domainPool["ipv4"], record) // 添加到IPv4记录
					} else { // 如果是IPv6
						domainPool["ipv6"] = append(domainPool["ipv6"], record) // 添加到IPv6记录
					}
					muDomainPool.Unlock() // 解锁
				}
			}(ip) // 传递IP参数
		}
		wgIPInfo.Wait() // 等待所有IP信息查询完成
	}

	// 6. 更新内存缓存和文件
	m.mu.Lock()                       // 加锁
	m.latestData[domain] = domainPool // 更新内存缓存
	m.mu.Unlock()                     // 解锁

	if err := m.saveDomainData(filePath, domainPool); err != nil { // 保存域名数据
		fmt.Printf("错误: 域名 [%s] 无法将结果保存到文件 %s: %v\n", domain, filePath, err) // 输出错误日志
	} else { // 如果保存成功
		fmt.Printf("域名 [%s] 更新完成，结果已保存到 %s\n", domain, filePath) // 输出成功日志
	}
}

// resolveDomainConcurrently 使用并发工作池查询所有DNS服务器，以获取最多样化的IP列表。
// 参数：domain - 要解析的域名
// 返回值：IPv4地址列表、IPv6地址列表和错误信息
func (m *remoteIPMonitor) resolveDomainConcurrently(domain string) ([]string, []string, error) {
	var ipv4Map, ipv6Map sync.Map // 声明IPv4和IPv6同步映射
	var wg sync.WaitGroup         // 声明等待组
	maxWorkers := 50              // 并发DNS查询工作线程数

	serverChan := make(chan string, len(m.config.DNSServers)) // 创建服务器通道
	for _, server := range m.config.DNSServers {              // 遍历DNS服务器列表
		serverChan <- server // 发送到通道
	}
	close(serverChan) // 关闭通道

	if len(m.config.DNSServers) < maxWorkers { // 如果服务器数量小于最大工作线程数
		maxWorkers = len(m.config.DNSServers) // 调整工作线程数
	}

	for i := 0; i < maxWorkers; i++ { // 启动工作线程
		wg.Add(1)   // 增加等待计数
		go func() { // 启动goroutine
			defer wg.Done()                  // 延迟减少等待计数
			client := new(dns.Client)        // 创建DNS客户端
			client.Timeout = 5 * time.Second // 设置超时时间
			for server := range serverChan { // 从通道读取服务器
				addr := server                    // 获取服务器地址
				if !strings.Contains(addr, ":") { // 如果地址不包含端口
					addr = net.JoinHostPort(addr, "53") // 添加默认端口53
				}

				// 查询A记录
				msgA := new(dns.Msg)                                         // 创建DNS消息
				msgA.SetQuestion(dns.Fqdn(domain), dns.TypeA)                // 设置查询A记录
				rA, _, err := client.Exchange(msgA, addr)                    // 执行DNS查询
				if err == nil && rA != nil && rA.Rcode == dns.RcodeSuccess { // 如果查询成功
					for _, ans := range rA.Answer { // 遍历答案
						if a, ok := ans.(*dns.A); ok { // 如果是A记录
							ipv4Map.Store(a.A.String(), true) // 存储IPv4地址
						}
					}
				}

				// 查询AAAA记录
				msgAAAA := new(dns.Msg)                                            // 创建DNS消息
				msgAAAA.SetQuestion(dns.Fqdn(domain), dns.TypeAAAA)                // 设置查询AAAA记录
				rAAAA, _, err := client.Exchange(msgAAAA, addr)                    // 执行DNS查询
				if err == nil && rAAAA != nil && rAAAA.Rcode == dns.RcodeSuccess { // 如果查询成功
					for _, ans := range rAAAA.Answer { // 遍历答案
						if aaaa, ok := ans.(*dns.AAAA); ok { // 如果是AAAA记录
							ipv6Map.Store(aaaa.AAAA.String(), true) // 存储IPv6地址
						}
					}
				}
			}
		}()
	}
	wg.Wait() // 等待所有工作线程完成

	var ipv4s, ipv6s []string                         // 声明IPv4和IPv6地址列表
	ipv4Map.Range(func(key, value interface{}) bool { // 遍历IPv4映射
		ipv4s = append(ipv4s, key.(string)) // 添加到IPv4列表
		return true                         // 继续遍历
	})
	ipv6Map.Range(func(key, value interface{}) bool { // 遍历IPv6映射
		ipv6s = append(ipv6s, key.(string)) // 添加到IPv6列表
		return true                         // 继续遍历
	})

	return ipv4s, ipv6s, nil // 返回IPv4列表、IPv6列表和nil
}

// loadDomainData 从指定路径加载单个域名的数据。
// 参数：filePath - 文件路径
// 返回值：域名数据映射
func (m *remoteIPMonitor) loadDomainData(filePath string) map[string][]IPRecord {
	data := map[string][]IPRecord{"ipv4": {}, "ipv6": {}} // 初始化数据映射
	fileData, err := os.ReadFile(filePath)                // 读取文件数据
	if err != nil {                                       // 如果读取失败
		return data // 文件不存在或读取失败，返回一个空的池
	}

	switch m.config.StorageFormat { // 根据存储格式解析数据
	case "json": // JSON格式
		_ = json.Unmarshal(fileData, &data) // 解析JSON数据
	case "yaml": // YAML格式
		_ = yaml.Unmarshal(fileData, &data) // 解析YAML数据
	case "toml": // TOML格式
		_ = toml.Unmarshal(fileData, &data) // 解析TOML数据
	}
	return data // 返回数据
}

// fetchIPInfo 使用共享的HTTP客户端获取单个IP的信息。
// 参数：ip - 要查询的IP地址
// 返回值：IP信息响应和错误信息
func (m *remoteIPMonitor) fetchIPInfo(ip string) (*IPInfoResponse, error) {
	url := fmt.Sprintf("https://ipinfo.io/%s/json?token=%s", ip, m.config.IPInfoToken) // 构造URL
	resp, err := m.httpClient.Get(url)                                                 // 发起HTTP GET请求
	if err != nil {                                                                    // 如果请求失败
		return nil, err // 返回错误
	}
	defer func() { _ = resp.Body.Close() }() // 延迟关闭响应体
	body, err := io.ReadAll(resp.Body)       // 读取响应体
	if err != nil {                          // 如果读取失败
		return nil, err // 返回错误
	}
	var info IPInfoResponse                             // 声明IP信息响应变量
	if err := json.Unmarshal(body, &info); err != nil { // 解析JSON数据
		return nil, err // 返回错误
	}
	return &info, nil // 返回IP信息响应
}

// uniqueStrings 辅助函数，用于字符串切片去重。
// 参数：slice - 字符串切片
// 返回值：去重后的字符串切片
func uniqueStrings(slice []string) []string {
	keys := make(map[string]bool) // 创建去重映射
	var list []string             // 声明结果列表
	for _, entry := range slice { // 遍历切片
		if _, value := keys[entry]; !value { // 如果未出现过
			keys[entry] = true         // 标记为已出现
			list = append(list, entry) // 添加到结果列表
		}
	}
	return list // 返回去重后的列表
}

// saveDomainData 将单个域名的数据保存到指定文件，并在写入前自动创建目录。
// 参数：filePath - 文件路径，data - 要保存的数据
// 返回值：错误信息
func (m *remoteIPMonitor) saveDomainData(filePath string, data map[string][]IPRecord) error {
	// 获取文件所在的目录
	dir := filepath.Dir(filePath) // 获取目录路径
	// 使用 MkdirAll 安全地创建目录，如果目录已存在则什么也不做
	if err := os.MkdirAll(dir, 0755); err != nil { // 创建目录
		return fmt.Errorf("无法创建存储目录 %s: %w", dir, err) // 返回错误
	}

	var out []byte                  // 声明输出字节切片
	var err error                   // 声明错误变量
	switch m.config.StorageFormat { // 根据存储格式序列化数据
	case "json": // JSON格式
		out, err = json.MarshalIndent(data, "", "  ") // 序列化为JSON格式
	case "yaml": // YAML格式
		out, err = yaml.Marshal(data) // 序列化为YAML格式
	case "toml": // TOML格式
		buf := new(bytes.Buffer)                // 创建缓冲区
		err = toml.NewEncoder(buf).Encode(data) // 编码为TOML格式
		out = buf.Bytes()                       // 获取字节数据
	default: // 不支持的格式
		return fmt.Errorf("不支持的存储格式: %s", m.config.StorageFormat) // 返回错误
	}
	if err != nil { // 如果序列化失败
		return err // 返回错误
	}
	return os.WriteFile(filePath, out, 0644) // 写入文件
}
