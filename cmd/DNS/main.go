package main // 主程序包

import ( // 导入所需包
	"encoding/json" // 用于JSON编码解码
	"log"           // 用于日志记录
	"os"            // 用于操作系统功能
	"os/signal"     // 用于处理系统信号
	"syscall"       // 用于系统调用
	"time"          // 用于时间处理
	"utlsProxy/src" // 导入自定义的src包
)

// DNSDatabaseConfig 定义一个结构体来匹配 DNSServerNames.json 文件的真实格式
// Servers字段是一个字符串到字符串的映射，表示DNS服务器配置
type DNSDatabaseConfig struct {
	Servers map[string]string `json:"servers"` // JSON标签，用于解析servers字段
}

// main 函数是程序的入口点
func main() {
	// 1. 从文件加载DNS服务器数据库
	dnsData, err := os.ReadFile("./src/DNSServerNames.json") // 读取DNS服务器配置文件
	if err != nil {                                          // 如果读取失败
		log.Fatalf("无法读取 DNSServerNames.json: %v", err) // 记录致命错误并退出
	}

	// 使用新的结构体来解析JSON对象
	var dnsDB DNSDatabaseConfig                             // 声明DNS数据库配置变量
	if err := json.Unmarshal(dnsData, &dnsDB); err != nil { // 解析JSON数据
		log.Fatalf("解析 DNSServerNames.json 失败: %v", err) // 记录致命错误并退出
	}

	// 2. 从解析后的数据中提取并去重所有DNS服务器IP
	uniqueServers := make(map[string]bool) // 创建去重映射
	var dnsServers []string                // 声明DNS服务器列表
	for _, ip := range dnsDB.Servers {     // 遍历所有DNS服务器
		if !uniqueServers[ip] { // 如果该IP尚未添加
			uniqueServers[ip] = true            // 标记为已添加
			dnsServers = append(dnsServers, ip) // 添加到服务器列表
		}
	}
	log.Printf("成功从数据库加载并去重后得到 %d 个DNS服务器。\n", len(dnsServers)) // 记录日志

	// 3. 创建配置，并注入处理后的DNS服务器列表
	config := src.MonitorConfig{ // 创建监视器配置
		Domains:        []string{"kh.google.com", "earth.google.com", "khmdb.google.com"}, // 监控的域名列表
		DNSServers:     dnsServers,                                                        // 使用从数据库提取并去重后的服务器列表
		IPInfoToken:    "f6babc99a5ec26",                                                  // IP信息查询令牌
		UpdateInterval: 10 * time.Minute,                                                  // 更新间隔为2分钟
		StorageDir:     "./domain_ips",                                                    // 存储目录
		StorageFormat:  "json",                                                            // 存储格式为JSON
	}

	// 4. 初始化并启动监视器
	monitor, err := src.NewRemoteIPMonitor(config) // 创建新的远程IP监视器
	if err != nil {                                // 如果创建失败
		log.Fatalf("无法创建监视器: %v", err) // 记录致命错误并退出
	}
	monitor.Start()      // 启动监视器
	defer monitor.Stop() // 程序结束时停止监视器

	// 5. 优雅地处理程序退出
	quit := make(chan os.Signal, 1)                      // 创建信号通道
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM) // 注册中断和终止信号
	<-quit                                               // 等待接收信号
	log.Println("收到退出信号，准备关闭...")                        // 记录日志
}
