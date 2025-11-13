package config // 定义config包

import ( // 导入所需的标准库和第三方库
	"fmt"     // 用于格式化输入输出
	"os"      // 用于操作系统功能
	"strings" // 用于字符串操作
	"time"    // 用于时间处理

	"github.com/BurntSushi/toml" // 用于TOML格式解析
)

// Config 定义完整的配置结构体
type Config struct {
	ServerConfig           ServerConfig           `toml:"ServerConfig"`           // 服务器配置
	DNSDomain              DNSDomainConfig        `toml:"DNSDomain"`              // DNS域名配置
	PoolConfig             PoolConfig             `toml:"PoolConfig"`             // 连接池配置
	IPInfo                 IPInfoConfig           `toml:"IPInfo"`                 // IP信息配置
	UTlsClient             UTlsClientConfig       `toml:"UTlsClient"`             // UTLS客户端配置
	HotConnPool            HotConnPoolConfig      `toml:"HotConnPool"`            // 热连接池配置
	RockTreeDataConfig     RockTreeDataConfig     `toml:"RockTreeDataConfig"`     // RockTree数据配置
	EarthImageryDataConfig EarthImageryDataConfig `toml:"EarthImageryDataConfig"` // Earth影像数据配置
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Version                  string `toml:"Version"`                  // 版本号
	ServerPort               int    `toml:"ServerPort"`               // gRPC 服务器端口
	EnableQUIC               bool   `toml:"EnableQUIC"`               // 是否启用 QUIC 传输
	QUICPort                 int    `toml:"QUICPort"`                 // QUIC 监听端口
	QUICCertFile             string `toml:"QUICCertFile"`             // QUIC TLS 证书路径
	QUICKeyFile              string `toml:"QUICKeyFile"`              // QUIC TLS 私钥路径
	QUICCAFile               string `toml:"QUICCAFile"`               // QUIC 根证书（可选，用于客户端校验）
	QUICALPN                 string `toml:"QUICALPN"`                 // ALPN 标识符
	QUICMaxIdleTimeoutSecond int    `toml:"QUICMaxIdleTimeoutSecond"` // 会话最大空闲超时（秒）
}

// DNSDomainConfig DNS域名配置
type DNSDomainConfig struct {
	HostName                   []string `toml:"HostName"`                   // 主机名列表
	StorageDir                 string   `toml:"StorageDir"`                 // 存储目录
	StorageFormat              string   `toml:"StorageFormat"`              // 存储格式
	UpdateIntervalMinutes      int      `toml:"UpdateIntervalMinutes"`      // 更新间隔（分钟）
	DNSServerFilePath          string   `toml:"DNSServerFilePath"`          // DNS服务器配置文件路径
	DefaultDNSServers          []string `toml:"DefaultDNSServers"`          // 默认DNS服务器列表
	DNSQueryTimeoutSeconds     int      `toml:"DNSQueryTimeoutSeconds"`     // DNS查询超时时间（秒）
	DNSMaxWorkers              int      `toml:"DNSMaxWorkers"`              // DNS并发查询工作线程数
	HTTPClientTimeoutSeconds   int      `toml:"HTTPClientTimeoutSeconds"`   // HTTP客户端超时时间（秒）
	HTTPMaxIdleConns           int      `toml:"HTTPMaxIdleConns"`           // HTTP最大空闲连接数
	HTTPMaxIdleConnsPerHost    int      `toml:"HTTPMaxIdleConnsPerHost"`    // 每个主机最大空闲连接数
	HTTPIdleConnTimeoutSeconds int      `toml:"HTTPIdleConnTimeoutSeconds"` // HTTP空闲连接超时时间（秒）
}

// PoolConfig 连接池配置
type PoolConfig struct {
	ProxyAddress                  string `toml:"ProxyAddress"`                  // 代理地址
	Concurrency                   int    `toml:"Concurrency"`                   // 并发数
	RehabilitationIntervalMinutes int    `toml:"RehabilitationIntervalMinutes"` // 恢复间隔（分钟）
	IdleTimeoutMinutes            int    `toml:"IdleTimeoutMinutes"`            // 空闲超时（分钟）
}

// IPInfoConfig IP信息配置
type IPInfoConfig struct {
	Token string `toml:"Token"` // IP信息查询令牌
}

// UTlsClientConfig UTLS客户端配置
type UTlsClientConfig struct {
	ReadTimeoutSeconds int `toml:"ReadTimeoutSeconds"` // 读取超时时间（秒）
	DialTimeoutSeconds int `toml:"DialTimeoutSeconds"` // 连接超时时间（秒）
	MaxRetries         int `toml:"MaxRetries"`         // 最大重试次数
}

// HotConnPoolConfig 热连接池配置
type HotConnPoolConfig struct {
	// 本地IP池配置
	LocalIPv4Addresses  []string `toml:"LocalIPv4Addresses"`  // 本地IPv4地址列表（备用）
	LocalIPv6SubnetCIDR string   `toml:"LocalIPv6SubnetCIDR"` // 本地IPv6子网CIDR（优先）
	IPv6QueueSize       int      `toml:"IPv6QueueSize"`       // IPv6地址队列缓冲区大小

	// 连接池基础配置
	Domain             string `toml:"Domain"`             // 目标域名
	Port               string `toml:"Port"`               // 目标端口
	MaxConns           int    `toml:"MaxConns"`           // 最大连接数
	IdleTimeoutMinutes int    `toml:"IdleTimeoutMinutes"` // 连接空闲超时时间（分钟）

	// 预热配置
	WarmupPath        string `toml:"WarmupPath"`        // 预热测试路径（可选，为空则使用RockTreeDataConfig.CheckStatusPath）
	WarmupMethod      string `toml:"WarmupMethod"`      // 预热请求方法
	WarmupConcurrency int    `toml:"WarmupConcurrency"` // 预热并发数

	// 定时任务配置
	BlacklistTestIntervalMinutes int `toml:"BlacklistTestIntervalMinutes"` // 黑名单IP测试间隔（分钟）
	IPRefreshIntervalMinutes     int `toml:"IPRefreshIntervalMinutes"`     // IP列表刷新间隔（分钟）

	// TLS指纹配置
	FingerprintName string `toml:"FingerprintName"` // TLS指纹名称
}

// RockTreeDataConfig RockTree数据配置
type RockTreeDataConfig struct {
	HostName             string   `toml:"HostName"`             // 主机名
	CheckStatusPath      string   `toml:"CheckStatusPath"`      // 检查状态路径
	BulkMetadataPath     string   `toml:"BulkMetadataPath"`     // 批量元数据路径
	NodeDataPath         string   `toml:"NodeDataPath"`         // 节点数据路径
	ImageryDataPath      string   `toml:"ImageryDataPath"`      // 影像数据路径
	RocktreeRquestHeader []string `toml:"RocktreeRquestHeader"` // 请求头列表
}

// EarthImageryDataConfig Earth影像数据配置
type EarthImageryDataConfig struct {
	HostName        string   `toml:"HostName"`        // 主机名
	CheckStatusPath string   `toml:"CheckStatusPath"` // 检查状态路径
	DbrootPath      string   `toml:"dbrootPath"`      // dbroot路径
	Q2Path          string   `toml:"q2path"`          // q2路径
	ImageryPath     string   `toml:"imageryPath"`     // 影像路径
	RequestHeader   []string `toml:"requestHeader"`   // 请求头列表
}

// LoadConfig 从指定路径加载配置文件
// 参数：configPath - 配置文件路径
// 返回值：配置结构体指针和错误信息
func LoadConfig(configPath string) (*Config, error) {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}

	// 读取配置文件内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析TOML配置
	var config Config
	if _, err := toml.Decode(string(data), &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 验证并设置默认值
	if err := config.validateAndSetDefaults(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	return &config, nil
}

// validateAndSetDefaults 验证配置并设置默认值
func (c *Config) validateAndSetDefaults() error {
	// 热连接池配置默认值
	if c.HotConnPool.Port == "" {
		c.HotConnPool.Port = "443"
	}
	if c.HotConnPool.MaxConns == 0 {
		c.HotConnPool.MaxConns = 100
	}
	if c.HotConnPool.IdleTimeoutMinutes == 0 {
		c.HotConnPool.IdleTimeoutMinutes = 5
	}
	if c.HotConnPool.WarmupMethod == "" {
		c.HotConnPool.WarmupMethod = "GET"
	}
	if c.HotConnPool.WarmupConcurrency == 0 {
		c.HotConnPool.WarmupConcurrency = 10
	}
	if c.HotConnPool.BlacklistTestIntervalMinutes == 0 {
		c.HotConnPool.BlacklistTestIntervalMinutes = 5
	}
	if c.HotConnPool.IPRefreshIntervalMinutes == 0 {
		c.HotConnPool.IPRefreshIntervalMinutes = 10
	}

	// DNS域名配置默认值
	if c.DNSDomain.StorageFormat == "" {
		c.DNSDomain.StorageFormat = "json"
	}
	if c.DNSDomain.StorageDir == "" {
		c.DNSDomain.StorageDir = "./domain_ips"
	}
	if c.DNSDomain.UpdateIntervalMinutes == 0 {
		c.DNSDomain.UpdateIntervalMinutes = 10
	}
	if c.DNSDomain.DNSServerFilePath == "" {
		c.DNSDomain.DNSServerFilePath = "./src/DNSServerNames.json"
	}
	if len(c.DNSDomain.DefaultDNSServers) == 0 {
		c.DNSDomain.DefaultDNSServers = []string{"8.8.8.8", "1.1.1.1"}
	}
	if c.DNSDomain.DNSQueryTimeoutSeconds == 0 {
		c.DNSDomain.DNSQueryTimeoutSeconds = 5
	}
	if c.DNSDomain.DNSMaxWorkers == 0 {
		c.DNSDomain.DNSMaxWorkers = 50
	}
	if c.DNSDomain.HTTPClientTimeoutSeconds == 0 {
		c.DNSDomain.HTTPClientTimeoutSeconds = 10
	}
	if c.DNSDomain.HTTPMaxIdleConns == 0 {
		c.DNSDomain.HTTPMaxIdleConns = 100
	}
	if c.DNSDomain.HTTPMaxIdleConnsPerHost == 0 {
		c.DNSDomain.HTTPMaxIdleConnsPerHost = 10
	}
	if c.DNSDomain.HTTPIdleConnTimeoutSeconds == 0 {
		c.DNSDomain.HTTPIdleConnTimeoutSeconds = 90
	}

	// UTLS客户端配置默认值
	if c.UTlsClient.ReadTimeoutSeconds == 0 {
		c.UTlsClient.ReadTimeoutSeconds = 30
	}
	if c.UTlsClient.DialTimeoutSeconds == 0 {
		c.UTlsClient.DialTimeoutSeconds = 10
	}

	// 热连接池IPv6队列大小默认值
	if c.HotConnPool.IPv6QueueSize == 0 {
		c.HotConnPool.IPv6QueueSize = 100
	}

	// QUIC 配置默认值
	if c.ServerConfig.QUICALPN == "" {
		c.ServerConfig.QUICALPN = "utls-proxy-quic"
	}
	if c.ServerConfig.QUICMaxIdleTimeoutSecond == 0 {
		c.ServerConfig.QUICMaxIdleTimeoutSecond = 30
	}
	if c.ServerConfig.EnableQUIC && c.ServerConfig.QUICPort == 0 {
		c.ServerConfig.QUICPort = 9092
	}

	return nil
}

// GetIdleTimeout 获取连接空闲超时时间
func (c *HotConnPoolConfig) GetIdleTimeout() time.Duration {
	return time.Duration(c.IdleTimeoutMinutes) * time.Minute
}

// GetBlacklistTestInterval 获取黑名单测试间隔
func (c *HotConnPoolConfig) GetBlacklistTestInterval() time.Duration {
	return time.Duration(c.BlacklistTestIntervalMinutes) * time.Minute
}

// GetIPRefreshInterval 获取IP刷新间隔
func (c *HotConnPoolConfig) GetIPRefreshInterval() time.Duration {
	return time.Duration(c.IPRefreshIntervalMinutes) * time.Minute
}

// GetWarmupPath 获取预热路径
// 如果 HotConnPool.WarmupPath 为空，则使用 RockTreeDataConfig.CheckStatusPath
func (c *Config) GetWarmupPath() string {
	if c.HotConnPool.WarmupPath != "" {
		return c.HotConnPool.WarmupPath
	}
	// 优先使用 RockTreeDataConfig.CheckStatusPath
	if c.RockTreeDataConfig.CheckStatusPath != "" {
		return c.RockTreeDataConfig.CheckStatusPath
	}
	// 如果都没有，使用 EarthImageryDataConfig.CheckStatusPath 作为备用
	if c.EarthImageryDataConfig.CheckStatusPath != "" {
		return c.EarthImageryDataConfig.CheckStatusPath
	}
	// 默认值
	return "/rt/earth/PlanetoidMetadata"
}

// GetWarmupHeaders 获取预热请求头
// 优先使用 RockTreeDataConfig.RocktreeRquestHeader，如果为空则使用 EarthImageryDataConfig.RequestHeader
// 返回值：map[string]string 格式的请求头
func (c *Config) GetWarmupHeaders() map[string]string {
	var headerList []string

	// 优先使用 RockTreeDataConfig.RocktreeRquestHeader
	if len(c.RockTreeDataConfig.RocktreeRquestHeader) > 0 {
		headerList = c.RockTreeDataConfig.RocktreeRquestHeader
	} else if len(c.EarthImageryDataConfig.RequestHeader) > 0 {
		// 备用：使用 EarthImageryDataConfig.RequestHeader
		headerList = c.EarthImageryDataConfig.RequestHeader
	} else {
		// 默认请求头
		return map[string]string{
			"Accept":          "*/*",
			"Accept-Encoding": "gzip, deflate, br, zstd",
		}
	}

	// 解析请求头字符串数组为 map
	headers := make(map[string]string)
	for _, headerStr := range headerList {
		// 解析格式："Key: Value" 或 "Key:Value"
		parts := strings.SplitN(headerStr, ":", 2)
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

// GetRehabilitationInterval 获取恢复间隔
func (c *PoolConfig) GetRehabilitationInterval() time.Duration {
	return time.Duration(c.RehabilitationIntervalMinutes) * time.Minute
}

// GetIdleTimeout 获取空闲超时时间
func (c *PoolConfig) GetIdleTimeout() time.Duration {
	return time.Duration(c.IdleTimeoutMinutes) * time.Minute
}

// GetUpdateInterval 获取更新间隔
func (c *DNSDomainConfig) GetUpdateInterval() time.Duration {
	return time.Duration(c.UpdateIntervalMinutes) * time.Minute
}

// GetDNSQueryTimeout 获取DNS查询超时时间
func (c *DNSDomainConfig) GetDNSQueryTimeout() time.Duration {
	return time.Duration(c.DNSQueryTimeoutSeconds) * time.Second
}

// GetHTTPClientTimeout 获取HTTP客户端超时时间
func (c *DNSDomainConfig) GetHTTPClientTimeout() time.Duration {
	return time.Duration(c.HTTPClientTimeoutSeconds) * time.Second
}

// GetHTTPIdleConnTimeout 获取HTTP空闲连接超时时间
func (c *DNSDomainConfig) GetHTTPIdleConnTimeout() time.Duration {
	return time.Duration(c.HTTPIdleConnTimeoutSeconds) * time.Second
}

// GetReadTimeout 获取读取超时时间
func (c *UTlsClientConfig) GetReadTimeout() time.Duration {
	return time.Duration(c.ReadTimeoutSeconds) * time.Second
}

// GetDialTimeout 获取连接超时时间
func (c *UTlsClientConfig) GetDialTimeout() time.Duration {
	return time.Duration(c.DialTimeoutSeconds) * time.Second
}

// GetQUICMaxIdleTimeout 获取 QUIC 会话最大空闲超时时间
func (c *ServerConfig) GetQUICMaxIdleTimeout() time.Duration {
	if c.QUICMaxIdleTimeoutSecond <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.QUICMaxIdleTimeoutSecond) * time.Second
}
