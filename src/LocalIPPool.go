package src // Package src 定义src包

import ( // 导入所需的标准库
	"crypto/rand"     // 用于加密安全的随机数生成
	"fmt"             // 用于格式化输入输出
	"io"              // 用于基础IO接口
	"math/big"        // 用于大整数运算
	mrand "math/rand" // 用于伪随机数生成
	"net"             // 用于网络相关功能
	"os/exec"         // 用于执行系统命令
	"strings"         // 用于字符串操作
	"sync"            // 用于同步原语如互斥锁
	"time"            // 用于时间处理
)

// IPPool 定义了IP地址池的行为接口。
// 通过依赖此接口，业务逻辑可以与具体的IP池实现（如LocalIPPool）解耦，
// 从而提高代码的可测试性和可扩展性。
type IPPool interface { // 定义IPPool接口
	// GetIP 从池中获取一个可用的IP地址。
	GetIP() net.IP // 获取一个IP地址的方法
	// ReleaseIP 释放一个IPv6地址（删除并创建新的），仅对IPv6地址有效
	ReleaseIP(ip net.IP) // 释放IP地址的方法
	// MarkIPUnused 标记IPv6地址为未使用（不立即删除，等待定期清理）
	MarkIPUnused(ip net.IP) // 标记IP地址为未使用方法
	// SetTargetIPCount 设置目标IP数量（用于IPv6地址池动态调整）
	SetTargetIPCount(count int) // 设置目标IP数量方法
	// Closer io.Closer 接口的实现，允许使用 defer pool.Close() 的方式优雅关闭。
	io.Closer // 嵌入Closer接口，用于资源清理
}

// LocalIPPool 是一个智能IP地址池，实现了 IPPool 接口。
// 它能够自动适应当前运行环境，管理静态IPv4地址，并在检测到可用的IPv6子网时，
// 动态地生成海量的IPv6地址。
type LocalIPPool struct { // 定义LocalIPPool结构体，实现IPPool接口
	mu             sync.RWMutex // 读写互斥锁，保护结构体字段并发安全
	staticIPv4s    []net.IP     // 静态IPv4地址列表
	rand           *mrand.Rand  // 伪随机数生成器
	hasIPv6Support bool         // 标记是否支持IPv6动态生成

	// --- 以下字段仅在 hasIPv6Support 为 true 时被初始化和使用 ---

	// ipv6Subnet 存储了服务商提供的IPv6子网信息，例如 "2607:8700:5500:2943::/64"。
	ipv6Subnet *net.IPNet // IPv6子网信息
	// ipv6Queue 是一个带缓冲的通道，作为预生成IPv6地址的队列，供消费者快速获取。
	ipv6Queue chan net.IP // IPv6地址队列
	// stopChan 用于在关闭IP池时，向后台的生成器goroutine发送停止信号。
	stopChan chan struct{} // 停止信号通道
	// ipv6Interface 存储IPv6接口名称，用于绑定IPv6地址
	ipv6Interface string // IPv6接口名称
	// createdIPv6Addrs 存储已创建的IPv6地址，用于清理
	createdIPv6Addrs map[string]bool // 已创建的IPv6地址映射
	createdIPv6Mutex sync.RWMutex    // 保护已创建地址映射的互斥锁
	// usedIPv6Addrs 存储正在使用的IPv6地址
	usedIPv6Addrs map[string]bool // 正在使用的IPv6地址映射
	usedIPv6Mutex sync.RWMutex    // 保护正在使用地址映射的互斥锁
	// activeIPv6Addrs 存储当前活跃的IPv6地址（已创建且在系统上）
	activeIPv6Addrs map[string]bool // 活跃的IPv6地址映射
	activeIPv6Mutex sync.RWMutex    // 保护活跃地址映射的互斥锁
	// batchSize 批量创建/删除的地址数量
	batchSize int // 批量操作大小
	// minActiveAddrs 最小活跃地址数量
	minActiveAddrs int // 最小活跃地址数
	// maxActiveAddrs 最大活跃地址数量
	maxActiveAddrs int // 最大活跃地址数
	// lastCleanupTime 上次清理时间
	lastCleanupTime time.Time // 上次清理时间
	cleanupMutex    sync.Mutex // 保护清理时间的互斥锁
}

// NewLocalIPPool 创建并初始化一个智能IP池。
//
// 该函数的核心特性是环境自适应：它会自动检测提供的 ipv6SubnetCIDR 是否在
// 当前系统的网络接口上真实可用。如果可用，则启用IPv6动态生成模式；否则，
// 将自动降级为仅IPv4模式。
//
// 注意：返回类型为 IPPool 接口，这强制调用方依赖于抽象而非具体实现。
//
// 参数:
//   - staticIPv4s: 一个包含静态IPv4地址字符串的切片，例如 []string{"1.1.1.1", "8.8.8.8"}。
//   - ipv6SubnetCIDR: 一个IPv6子网的CIDR表示法字符串。例如 "2607:8700:5500:2943::/64"。
//     如果此参数为空字符串，或者系统环境中未配置该子网，则不会启用IPv6功能。
//
// 返回值:
//   - 一个实现了 IPPool 接口的实例。
//   - 如果没有可用的IP地址（既没有有效的IPv4，IPv6环境也不支持），则返回错误。
func NewLocalIPPool(staticIPv4s []string, ipv6SubnetCIDR string) (IPPool, error) {
	// 初始化基础结构，包括一个私有的随机数生成器以避免全局锁。
	pool := &LocalIPPool{ // 创建LocalIPPool实例
		rand:     mrand.New(mrand.NewSource(time.Now().UnixNano())), // 初始化随机数生成器
		stopChan: make(chan struct{}),                               // 创建停止信号通道
	}

	// 如果未提供静态IPv4地址，自动检测系统中可用的IPv4地址
	if len(staticIPv4s) == 0 {
		detectedIPv4s := detectAvailableIPv4Addresses()
		if len(detectedIPv4s) > 0 {
			fmt.Printf("[IP池] 自动检测到 %d 个IPv4地址: %v\n", len(detectedIPv4s), detectedIPv4s)
			staticIPv4s = detectedIPv4s
		} else {
			fmt.Println("[IP池] 警告: 未检测到可用的IPv4地址")
		}
	}

	// 解析并验证传入的静态IPv4地址。
	for _, s := range staticIPv4s { // 遍历静态IPv4地址列表
		ip := net.ParseIP(s)              // 解析IP地址字符串
		if ip != nil && ip.To4() != nil { // 如果解析成功且是IPv4地址
			pool.staticIPv4s = append(pool.staticIPv4s, ip) // 添加到静态IPv4地址列表
		}
	}

	// 如果未提供IPv6子网CIDR，自动检测系统中可用的IPv6子网
	if ipv6SubnetCIDR == "" {
		detectedSubnets := detectAvailableIPv6Subnets()
		if len(detectedSubnets) > 0 {
			// 优先使用第一个检测到的/64子网
			ipv6SubnetCIDR = detectedSubnets[0]
			fmt.Printf("[IP池] 自动检测到IPv6子网: %s\n", ipv6SubnetCIDR)
		} else {
			// 即使没有检测到公网IPv6子网，如果系统支持IPv6路由（如通过隧道），
			// 仍然可以创建IPv6池，但不绑定本地IP，让系统自动选择路由
			if hasIPv6RoutingSupport() {
				fmt.Println("[IP池] 未检测到公网IPv6子网，但系统支持IPv6路由（可能通过隧道），将创建IPv6池（不绑定本地IP）")
				// 创建一个虚拟的IPv6子网，用于标识IPv6支持
				// 实际使用时不会绑定本地IP，而是让系统自动选择路由
				ipv6SubnetCIDR = "2000::/3" // 使用全局单播地址范围作为标识
			} else {
				fmt.Println("[IP池] 未检测到可用的IPv6子网，将使用IPv4模式")
			}
		}
	}

	// 检查并尝试启用IPv6支持。
	if ipv6SubnetCIDR != "" { // 如果提供了IPv6子网CIDR
		_, subnet, err := net.ParseCIDR(ipv6SubnetCIDR) // 解析IPv6子网CIDR
		if err != nil {                                 // 如果解析失败
			return nil, fmt.Errorf("无效的IPv6子网CIDR: %w", err) // 返回错误
		}

		// 检查是否是虚拟子网标识（用于隧道模式）
		isVirtualSubnet := subnet.IP.To4() == nil && len(subnet.IP) >= 2 && subnet.IP[0] == 0x20 && subnet.IP[1] == 0x00

		// 核心逻辑：检查当前系统网络配置是否真的支持此IPv6子网。
		if isSubnetConfigured(subnet) || isVirtualSubnet { // 检查子网是否已配置，或者是虚拟子网（隧道模式）
			if isVirtualSubnet {
				fmt.Println("[IP池] 检测到IPv6路由支持（隧道模式），已启用IPv6模式（不绑定本地IP）。") // 输出日志
				// 对于隧道模式，不绑定本地IP，让系统自动选择路由
				// 创建一个特殊的IPv6池，GetIP返回nil表示不绑定本地IP
				pool.hasIPv6Support = true
				pool.ipv6Subnet = subnet
				// 不创建IPv6队列，GetIP时返回nil，表示不绑定本地IP
			} else {
				fmt.Println("[IP池] 检测到可用的IPv6子网，已启用IPv6动态生成模式。") // 输出日志
				pool.hasIPv6Support = true                       // 设置IPv6支持标志
				pool.ipv6Subnet = subnet                         // 设置IPv6子网
				pool.ipv6Queue = make(chan net.IP, 100)          // 预生成100个IPv6地址作为缓冲区。
				pool.createdIPv6Addrs = make(map[string]bool)    // 初始化已创建地址映射
				pool.usedIPv6Addrs = make(map[string]bool)       // 初始化正在使用地址映射
				pool.activeIPv6Addrs = make(map[string]bool)     // 初始化活跃地址映射
				pool.batchSize = 10                              // 默认批量操作大小：10个地址
				pool.minActiveAddrs = 0                           // 最小活跃地址数（动态设置）
				pool.maxActiveAddrs = 0                           // 最大活跃地址数（动态设置）
				// 检测IPv6接口名称
				pool.ipv6Interface = detectIPv6Interface(subnet)
				if pool.ipv6Interface == "" {
					fmt.Println("[IP池] 警告: 未找到IPv6接口，将尝试使用 ipv6net")
					pool.ipv6Interface = "ipv6net" // 默认接口名称
				} else {
					fmt.Printf("[IP池] 检测到IPv6接口: %s\n", pool.ipv6Interface)
				}
				// 启动时清理旧的IPv6地址
				pool.cleanupOldIPv6Addresses(subnet)
				go pool.producer()                               // 在后台启动IPv6地址生产者。
				go pool.manageIPv6Addresses()                    // 在后台启动IPv6地址管理器（热加载）
			}
		} else { // 如果子网未配置
			fmt.Printf("[IP池] 警告: 未在当前网络环境中检测到指定的IPv6子网 %s，已降级为仅IPv4模式。\n", ipv6SubnetCIDR) // 输出日志
		}
	}

	// 如果最终没有任何可用的IP地址，则初始化失败。
	if !pool.hasIPv6Support && len(pool.staticIPv4s) == 0 { // 如果既不支持IPv6又没有IPv4地址
		return nil, fmt.Errorf("IP池初始化失败：没有可用的IPv4地址，且IPv6环境不支持") // 返回错误
	}

	return pool, nil // 返回初始化成功的IP池
}

// GetIP 从池中获取一个可用的IP地址。
//
// 如果池已启用IPv6支持，它将优先返回一个动态生成的、全新的IPv6地址。
// 对于隧道模式（虚拟子网），返回nil表示不绑定本地IP，让系统自动选择路由。
// 这种模式下，调用会阻塞直到获取到新的IP。
//
// 如果池工作在仅IPv4模式，它将从预设的列表中随机返回一个IPv4地址。
func (p *LocalIPPool) GetIP() net.IP { // 实现GetIP方法
	p.mu.RLock()                // 加读锁
	hasIPv6 := p.hasIPv6Support // 获取IPv6支持标志
	ipv6Queue := p.ipv6Queue    // 获取IPv6队列引用
	p.mu.RUnlock()              // 解读锁

	if hasIPv6 { // 如果支持IPv6
		// 如果IPv6队列为nil，说明是隧道模式，返回nil表示不绑定本地IP
		if ipv6Queue == nil {
			return nil // 隧道模式：不绑定本地IP，让系统自动选择路由
		}
		// 从队列中获取一个预生成的IPv6地址。
		// 如果队列为空，此操作会阻塞，直到后台生产者放入新的地址。
		ip := <-ipv6Queue // 从IPv6队列获取地址
		
		// 确保IPv6地址在系统上已创建
		if ip != nil {
			p.ensureIPv6AddressCreated(ip)
			// 标记地址为正在使用
			p.usedIPv6Mutex.Lock()
			p.usedIPv6Addrs[ip.String()] = true
			p.usedIPv6Mutex.Unlock()
		}
		
		return ip
	}

	// 在仅IPv4模式下，从静态列表中随机选择一个。
	p.mu.RLock()                 // 加读锁
	defer p.mu.RUnlock()         // 延迟解锁
	if len(p.staticIPv4s) == 0 { // 如果静态IPv4地址列表为空
		return nil // 理论上在初始化时已避免此情况。
	}
	idx := p.rand.Intn(len(p.staticIPv4s)) // 生成随机索引
	return p.staticIPv4s[idx]              // 返回随机选择的IPv4地址
}

// Close 优雅地关闭IP池，停止后台的goroutine，并清理所有创建的IPv6地址。
// 这是对 io.Closer 接口的实现。
func (p *LocalIPPool) Close() error { // 实现Close方法
	p.mu.RLock()                   // 加读锁
	hasSupport := p.hasIPv6Support // 获取IPv6支持标志
	p.mu.RUnlock()                 // 解读锁

	if hasSupport { // 如果支持IPv6
		// 清理所有创建的IPv6地址
		p.cleanupCreatedIPv6Addresses()
		
		// 使用非阻塞的方式尝试关闭channel，防止重复关闭导致的panic。
		select {
		case <-p.stopChan: // 检查停止通道是否已关闭
			// channel已经关闭，什么也不做。
		default: // 默认情况
			close(p.stopChan) // 关闭停止通道
		}
	}
	return nil // 返回nil表示关闭成功
}

// producer 是一个后台运行的goroutine，它持续不断地生成新的随机IPv6地址，
// 并将它们放入ipv6Queue通道，直到收到停止信号。
// 为了避免队列积压过多地址，当队列已满或活跃地址足够时暂停生成。
func (p *LocalIPPool) producer() { // IPv6地址生产者方法
	for { // 无限循环
		select {
		case <-p.stopChan: // 如果收到停止信号
			return // 收到停止信号，优雅退出。
		default: // 默认情况
			// 检查队列是否已满
			if len(p.ipv6Queue) >= cap(p.ipv6Queue) {
				// 队列已满，等待一段时间再检查
				time.Sleep(1 * time.Second)
				continue
			}
			
			// 检查活跃地址数量，如果已经足够，减少生成频率
			p.activeIPv6Mutex.RLock()
			activeCount := len(p.activeIPv6Addrs)
			p.activeIPv6Mutex.RUnlock()
			
			p.mu.RLock()
			minActive := p.minActiveAddrs
			p.mu.RUnlock()
			
			// 如果活跃地址已经达到或超过目标值，降低生成频率
			if minActive > 0 && activeCount >= minActive {
				// 队列未满但活跃地址已足够，降低生成频率
				time.Sleep(5 * time.Second)
				continue
			}
			
			// 生成随机IPv6地址并放入队列
			ip := p.generateRandomIPInSubnet()
			if ip != nil {
				select {
				case p.ipv6Queue <- ip: // 尝试将地址放入队列
				case <-p.stopChan: // 如果放入过程中收到停止信号
					return // 收到停止信号，优雅退出。
				}
			}
		}
	}
}

// generateRandomIPInSubnet 在给定的IPv6子网内生成一个随机的IP地址。
// 对于 /64 子网，只使用前64位（子网前缀），主机部分使用简单的16进制表示。
func (p *LocalIPPool) generateRandomIPInSubnet() net.IP { // 生成子网内随机IPv6地址的方法
	// 复制前缀以避免修改原始数据
	prefix := make(net.IP, len(p.ipv6Subnet.IP)) // 创建前缀副本
	copy(prefix, p.ipv6Subnet.IP)                // 复制IPv6子网IP

	// 计算主机部分的位数
	ones, total := p.ipv6Subnet.Mask.Size() // 获取子网掩码大小
	hostBits := total - ones                // 计算主机位数

	// 对于 /64 子网，使用随机的16进制后缀（如 ::a1b2, ::c3d4, ::ef56 等）
	// 避免使用系统已存在的十进制后缀地址（如 ::1001 到 ::1100）
	if ones == 64 {
		// 生成一个随机的16位16进制数作为后缀
		// 避免使用 0x1001 到 0x1100 范围（4097 到 4352），这些是系统已配置的地址
		// 生成范围：0x0001-0x1000 (1-4096) 和 0x1101-0xFFFF (4353-65535)
		var suffix uint16
		for {
			// 生成 1 到 65535 之间的随机数
			suffix = uint16(p.rand.Intn(0xFFFF) + 1)
			// 跳过系统已使用的范围 0x1001-0x1100 (4097-4352)
			if suffix < 0x1001 || suffix > 0x1100 {
				break
			}
		}
		
		// 将后缀填充到IPv6地址的后64位（最后16位）
		// IPv6地址是16字节，前8字节是前缀，后8字节是主机部分
		// 我们只使用最后2字节（16位）作为随机的16进制后缀
		prefix[14] = byte(suffix >> 8)  // 高字节（16进制高位）
		prefix[15] = byte(suffix & 0xFF) // 低字节（16进制低位）
		// 前面的字节保持为0（即 :: 的表示）
		for i := 8; i < 14; i++ {
			prefix[i] = 0
		}
	} else {
		// 对于非 /64 子网，使用原来的随机生成方式
		// 使用加密安全的随机数生成器生成一个覆盖主机位的大整数。
		randInt, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), uint(hostBits))) // 生成随机大整数
		if err != nil {                                                                        // 如果生成失败
			// 在极罕见的情况下，如果读取系统随机源失败，则回退到伪随机。
			// 这种情况在正常系统中几乎不会发生。
			randInt = big.NewInt(p.rand.Int63()) // 使用伪随机数生成器
		}
		randBytes := randInt.Bytes() // 获取随机数的字节表示

		// 将生成的随机字节填充到IP地址的主机部分。
		// 从后向前填充，以正确处理不同长度的随机数。
		for i := 0; i < len(randBytes); i++ { // 遍历随机字节数组
			prefix[total/8-1-i] |= randBytes[len(randBytes)-1-i] // 将随机字节填充到前缀中
		}
	}

	return prefix // 返回生成的IPv6地址
}

// isPrivateIPv4 检查IPv4地址是否为私有地址（RFC 1918）
func isPrivateIPv4(ip net.IP) bool {
	if ip.To4() == nil {
		return false
	}
	// 10.0.0.0/8
	if ip[0] == 10 {
		return true
	}
	// 172.16.0.0/12
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	// 192.168.0.0/16
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	return false
}

// isPrivateIPv6 检查IPv6地址是否为私有地址
func isPrivateIPv6(ip net.IP) bool {
	if ip.To4() != nil {
		return false
	}
	// fc00::/7 (ULA - Unique Local Addresses)
	if len(ip) >= 2 && ip[0] == 0xfc {
		return true
	}
	if len(ip) >= 2 && ip[0] == 0xfd {
		return true
	}
	// fe80::/10 (Link-local addresses)
	if len(ip) >= 2 && ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}
	return false
}

// detectAvailableIPv4Addresses 自动检测系统中可用的公网IPv4地址
// 返回所有非回环、已启用接口的公网IPv4地址列表（排除私有地址）
func detectAvailableIPv4Addresses() []string {
	var ipv4List []string
	interfaces, err := net.Interfaces()
	if err != nil {
		return ipv4List
	}

	for _, iface := range interfaces {
		// 忽略状态为Down的接口或回环接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				// 只处理IPv4地址
				if ipv4 := ipnet.IP.To4(); ipv4 != nil {
					// 排除私有地址（内网地址）
					if !isPrivateIPv4(ipv4) {
						ipv4List = append(ipv4List, ipv4.String())
					}
				}
			}
		}
	}
	return ipv4List
}

// detectAvailableIPv6Subnets 自动检测系统中可用的公网IPv6子网
// 返回所有非回环、已启用接口的公网IPv6子网CIDR列表（优先返回/64子网，排除私有地址）
func detectAvailableIPv6Subnets() []string {
	var subnets []string
	seenSubnets := make(map[string]bool) // 用于去重

	interfaces, err := net.Interfaces()
	if err != nil {
		return subnets
	}

	for _, iface := range interfaces {
		// 忽略状态为Down的接口或回环接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				// 只处理IPv6地址
				if ipnet.IP.To4() == nil {
					// 排除私有地址（内网地址）
					if isPrivateIPv6(ipnet.IP) {
						continue
					}

					ones, bits := ipnet.Mask.Size()

					// 如果已经是/64子网，直接使用
					if ones == 64 && bits == 128 {
						subnetCIDR := fmt.Sprintf("%s/64", ipnet.IP.String())
						if !seenSubnets[subnetCIDR] {
							subnets = append(subnets, subnetCIDR)
							seenSubnets[subnetCIDR] = true
						}
					} else if ones >= 64 {
						// 如果子网掩码大于等于64位，提取前64位作为子网前缀
						// 例如：2607:f8b0:4002:c09::5d/128 -> 2607:f8b0:4002:c09::/64
						ip := make(net.IP, 16)
						copy(ip, ipnet.IP)
						// 将后64位（后8字节）清零，得到/64子网前缀
						for i := 8; i < 16; i++ {
							ip[i] = 0
						}
						subnetCIDR := fmt.Sprintf("%s/64", ip.String())
						if !seenSubnets[subnetCIDR] {
							subnets = append(subnets, subnetCIDR)
							seenSubnets[subnetCIDR] = true
						}
					}
				}
			}
		}
	}
	return subnets
}

// hasIPv6RoutingSupport 检查系统是否支持IPv6路由（可能通过隧道）
func hasIPv6RoutingSupport() bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}

	for _, iface := range interfaces {
		// 检查是否有IPv6隧道接口（如sit0, tun0, ip6tnl0等）
		ifaceName := iface.Name
		if strings.HasPrefix(ifaceName, "sit") ||
			strings.HasPrefix(ifaceName, "tun") ||
			strings.HasPrefix(ifaceName, "ip6tnl") ||
			strings.HasPrefix(ifaceName, "6to4") ||
			strings.HasPrefix(ifaceName, "teredo") {
			// 检查接口是否启用
			if iface.Flags&net.FlagUp != 0 {
				return true
			}
		}

		// 检查是否有IPv6路由（通过检查接口是否有IPv6地址，即使是私有地址）
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				// 如果接口有IPv6地址（包括私有地址），说明系统支持IPv6
				if ipnet.IP.To4() == nil {
					return true
				}
			}
		}
	}
	return false
}

// isSubnetConfigured 遍历当前系统的所有网络接口及其地址，
// 检查是否有任何一个已配置的IP地址属于给定的目标子网。
func isSubnetConfigured(targetSubnet *net.IPNet) bool { // 检查子网是否已配置的方法
	interfaces, err := net.Interfaces() // 获取网络接口列表
	if err != nil {                     // 如果获取失败
		return false // 返回false
	}

	for _, iface := range interfaces { // 遍历网络接口
		// 忽略状态为Down的接口或回环接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 { // 如果接口down或为回环接口
			continue // 继续下一个接口
		}

		addrs, err := iface.Addrs() // 获取接口地址列表
		if err != nil {             // 如果获取失败
			continue // 继续下一个接口
		}

		for _, addr := range addrs { // 遍历地址列表
			// 类型断言，只处理IP网络地址
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() == nil { // 如果是IPv6网络地址
				// 检查接口上配置的IP地址是否位于我们目标的大子网内。
				// 例如，检查 2607:..::2/128 是否在 2607:..::/64 内。
				if targetSubnet.Contains(ipnet.IP) { // 如果包含目标IP
					return true // 返回true
				}
			}
		}
	}
	return false // 返回false
}

// detectIPv6Interface 检测包含指定IPv6子网的接口名称
func detectIPv6Interface(subnet *net.IPNet) string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		// 忽略状态为Down的接口或回环接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() == nil {
				// 检查接口上配置的IP地址是否位于目标子网内
				if subnet.Contains(ipnet.IP) {
					return iface.Name
				}
			}
		}
	}
	return ""
}

// ensureIPv6AddressCreated 确保IPv6地址在系统上已创建
func (p *LocalIPPool) ensureIPv6AddressCreated(ip net.IP) {
	if ip == nil {
		return
	}

	ipStr := ip.String()
	
	// 检查地址是否已创建
	p.createdIPv6Mutex.RLock()
	alreadyCreated := p.createdIPv6Addrs[ipStr]
	p.createdIPv6Mutex.RUnlock()

	if alreadyCreated {
		return // 地址已创建，跳过
	}

	// 检查地址是否已在系统上存在
	if p.isIPv6AddressExists(ipStr) {
		// 地址已存在，记录但不创建
		p.createdIPv6Mutex.Lock()
		p.createdIPv6Addrs[ipStr] = true
		p.createdIPv6Mutex.Unlock()
		p.activeIPv6Mutex.Lock()
		p.activeIPv6Addrs[ipStr] = true
		p.activeIPv6Mutex.Unlock()
		return
	}

	// 创建IPv6地址
	p.mu.RLock()
	interfaceName := p.ipv6Interface
	p.mu.RUnlock()

	if interfaceName == "" {
		interfaceName = "ipv6net" // 默认接口名称
	}

	// 使用 ip addr add 命令创建地址
	cmd := exec.Command("ip", "addr", "add", ipStr+"/128", "dev", interfaceName)
	if err := cmd.Run(); err != nil {
		// 创建失败，记录错误但不阻塞
		fmt.Printf("[IP池] 警告: 创建IPv6地址 %s 失败: %v\n", ipStr, err)
		return
	}

	// 记录已创建的地址
	p.createdIPv6Mutex.Lock()
	p.createdIPv6Addrs[ipStr] = true
	p.createdIPv6Mutex.Unlock()

	// 记录为活跃地址
	p.activeIPv6Mutex.Lock()
	p.activeIPv6Addrs[ipStr] = true
	p.activeIPv6Mutex.Unlock()

	fmt.Printf("[IP池] 已创建IPv6地址: %s/%s\n", ipStr, interfaceName)
}

// isIPv6AddressExists 检查IPv6地址是否已在系统上存在
func (p *LocalIPPool) isIPv6AddressExists(ipStr string) bool {
	p.mu.RLock()
	interfaceName := p.ipv6Interface
	p.mu.RUnlock()

	if interfaceName == "" {
		interfaceName = "ipv6net"
	}

	// 使用 ip addr show 命令检查地址是否存在
	cmd := exec.Command("ip", "-6", "addr", "show", "dev", interfaceName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.Contains(string(output), ipStr)
}

// cleanupCreatedIPv6Addresses 清理所有创建的IPv6地址
func (p *LocalIPPool) cleanupCreatedIPv6Addresses() {
	p.createdIPv6Mutex.Lock()
	defer p.createdIPv6Mutex.Unlock()

	if len(p.createdIPv6Addrs) == 0 {
		return
	}

	p.mu.RLock()
	interfaceName := p.ipv6Interface
	p.mu.RUnlock()

	if interfaceName == "" {
		interfaceName = "ipv6net"
	}

	cleaned := 0
	for ipStr := range p.createdIPv6Addrs {
		// 使用 ip addr del 命令删除地址
		cmd := exec.Command("ip", "addr", "del", ipStr+"/128", "dev", interfaceName)
		if err := cmd.Run(); err != nil {
			// 删除失败，记录但不阻塞
			fmt.Printf("[IP池] 警告: 删除IPv6地址 %s 失败: %v\n", ipStr, err)
		} else {
			cleaned++
		}
	}

	if cleaned > 0 {
		fmt.Printf("[IP池] 已清理 %d 个IPv6地址\n", cleaned)
	}

	// 清空映射
	p.createdIPv6Addrs = make(map[string]bool)
}

// ReleaseIP 释放一个IPv6地址（删除并创建新的），仅对IPv6地址有效
func (p *LocalIPPool) ReleaseIP(ip net.IP) {
	if ip == nil {
		return
	}

	// 只处理IPv6地址
	if ip.To4() != nil {
		return // IPv4地址不需要释放
	}

	ipStr := ip.String()

	// 检查是否正在使用
	p.usedIPv6Mutex.Lock()
	isUsed := p.usedIPv6Addrs[ipStr]
	delete(p.usedIPv6Addrs, ipStr)
	p.usedIPv6Mutex.Unlock()

	if !isUsed {
		return // 地址未在使用中，无需释放
	}

	// 删除IPv6地址
	p.mu.RLock()
	interfaceName := p.ipv6Interface
	p.mu.RUnlock()

	if interfaceName == "" {
		interfaceName = "ipv6net"
	}

	// 使用 ip addr del 命令删除地址
	cmd := exec.Command("ip", "addr", "del", ipStr+"/128", "dev", interfaceName)
	if err := cmd.Run(); err != nil {
		fmt.Printf("[IP池] 警告: 删除IPv6地址 %s 失败: %v\n", ipStr, err)
	} else {
		fmt.Printf("[IP池] 已释放IPv6地址: %s/%s\n", ipStr, interfaceName)
	}

	// 从已创建地址映射中移除
	p.createdIPv6Mutex.Lock()
	delete(p.createdIPv6Addrs, ipStr)
	p.createdIPv6Mutex.Unlock()

	// 从活跃地址映射中移除
	p.activeIPv6Mutex.Lock()
	delete(p.activeIPv6Addrs, ipStr)
	p.activeIPv6Mutex.Unlock()
}

// MarkIPUnused 标记IPv6地址为未使用（不立即删除，等待定期清理）
func (p *LocalIPPool) MarkIPUnused(ip net.IP) {
	if ip == nil {
		return
	}

	// 只处理IPv6地址
	if ip.To4() != nil {
		return // IPv4地址不需要标记
	}

	ipStr := ip.String()

	// 从正在使用的地址映射中移除，但不删除地址
	p.usedIPv6Mutex.Lock()
	delete(p.usedIPv6Addrs, ipStr)
	p.usedIPv6Mutex.Unlock()
}

// cleanupOldIPv6Addresses 清理子网下的所有旧IPv6地址（启动时调用）
func (p *LocalIPPool) cleanupOldIPv6Addresses(subnet *net.IPNet) {
	p.mu.RLock()
	interfaceName := p.ipv6Interface
	p.mu.RUnlock()

	if interfaceName == "" {
		interfaceName = "ipv6net"
	}

	// 获取接口上所有的IPv6地址
	cmd := exec.Command("ip", "-6", "addr", "show", "dev", interfaceName)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("[IP池] 警告: 无法获取接口 %s 的IPv6地址列表: %v\n", interfaceName, err)
		return
	}

	// 解析输出，找到所有属于子网的IPv6地址
	lines := strings.Split(string(output), "\n")
	cleaned := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "inet6 ") {
			continue
		}

		// 解析地址，格式如: inet6 2607:8700:5500:2943::2ca9/128 scope global
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		addrStr := strings.Split(parts[1], "/")[0] // 提取IP地址部分
		ip := net.ParseIP(addrStr)
		if ip == nil {
			continue
		}

		// 检查地址是否属于子网
		if subnet.Contains(ip) {
			// 删除地址
			delCmd := exec.Command("ip", "addr", "del", ip.String()+"/128", "dev", interfaceName)
			if err := delCmd.Run(); err != nil {
				// 删除失败，记录但不阻塞
				fmt.Printf("[IP池] 警告: 清理旧IPv6地址 %s 失败: %v\n", ip.String(), err)
			} else {
				cleaned++
			}
		}
	}

	if cleaned > 0 {
		fmt.Printf("[IP池] 启动时已清理 %d 个旧IPv6地址\n", cleaned)
	}
}

// manageIPv6Addresses 后台管理IPv6地址的热加载（动态创建和删除）
func (p *LocalIPPool) manageIPv6Addresses() {
	adjustTicker := time.NewTicker(30 * time.Second)  // 每30秒检查一次地址池大小
	cleanupTicker := time.NewTicker(20 * time.Minute)  // 每20分钟清理一次未使用的地址
	defer adjustTicker.Stop()
	defer cleanupTicker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-adjustTicker.C:
			p.adjustIPv6AddressPool() // 调整地址池大小（只创建，不删除）
		case <-cleanupTicker.C:
			p.cleanupUnusedIPv6Addresses() // 每20分钟清理一次未使用的地址
		}
	}
}

// adjustIPv6AddressPool 调整IPv6地址池大小（热加载）
func (p *LocalIPPool) adjustIPv6AddressPool() {
	p.mu.RLock()
	hasSupport := p.hasIPv6Support
	subnet := p.ipv6Subnet
	interfaceName := p.ipv6Interface
	batchSize := p.batchSize
	minActive := p.minActiveAddrs
	maxActive := p.maxActiveAddrs
	p.mu.RUnlock()

	if !hasSupport || subnet == nil {
		return
	}

	if interfaceName == "" {
		interfaceName = "ipv6net"
	}

	// 统计当前活跃地址数量
	p.activeIPv6Mutex.RLock()
	activeCount := len(p.activeIPv6Addrs)
	p.activeIPv6Mutex.RUnlock()

	// 如果目标IP数量未设置，跳过调整
	if minActive == 0 || maxActive == 0 {
		return
	}

	// 只负责创建地址，不在这里删除（删除由cleanupUnusedIPv6Addresses负责）
	// 如果活跃地址太少，批量创建新地址
	// 但是要避免频繁创建：如果活跃地址已经接近目标值，就不要继续创建
	if activeCount < minActive {
		needed := minActive - activeCount
		// 如果差距很小（小于batchSize），说明已经在接近目标值，不需要批量创建
		// 让GetIP()自然创建即可
		if needed >= batchSize {
			p.batchCreateIPv6Addresses(batchSize, subnet, interfaceName)
		}
		// 如果差距小于batchSize，说明已经接近目标值，不需要批量创建
		// 这样可以避免频繁创建，让GetIP()按需创建即可
	}
}

// batchCreateIPv6Addresses 批量创建IPv6地址
func (p *LocalIPPool) batchCreateIPv6Addresses(count int, subnet *net.IPNet, interfaceName string) {
	created := 0
	for i := 0; i < count; i++ {
		ip := p.generateRandomIPInSubnet()
		if ip == nil {
			continue
		}

		ipStr := ip.String()

		// 检查地址是否已存在
		if p.isIPv6AddressExists(ipStr) {
			// 地址已存在，记录但不创建
			p.createdIPv6Mutex.Lock()
			p.createdIPv6Addrs[ipStr] = true
			p.createdIPv6Mutex.Unlock()
			p.activeIPv6Mutex.Lock()
			p.activeIPv6Addrs[ipStr] = true
			p.activeIPv6Mutex.Unlock()
			continue
		}

		// 创建地址
		cmd := exec.Command("ip", "addr", "add", ipStr+"/128", "dev", interfaceName)
		if err := cmd.Run(); err != nil {
			fmt.Printf("[IP池] 警告: 批量创建IPv6地址 %s 失败: %v\n", ipStr, err)
			continue
		}

		// 记录地址
		p.createdIPv6Mutex.Lock()
		p.createdIPv6Addrs[ipStr] = true
		p.createdIPv6Mutex.Unlock()
		p.activeIPv6Mutex.Lock()
		p.activeIPv6Addrs[ipStr] = true
		p.activeIPv6Mutex.Unlock()

		created++
	}

	if created > 0 {
		fmt.Printf("[IP池] 热加载: 批量创建了 %d 个IPv6地址\n", created)
	}
}

// batchDeleteIPv6Addresses 批量删除IPv6地址（删除指定的地址列表）
func (p *LocalIPPool) batchDeleteIPv6Addresses(count int, interfaceName string, addrsToDelete []string) {
	deleted := 0
	for _, ipStr := range addrsToDelete {
		// 删除地址
		cmd := exec.Command("ip", "addr", "del", ipStr+"/128", "dev", interfaceName)
		if err := cmd.Run(); err != nil {
			fmt.Printf("[IP池] 警告: 批量删除IPv6地址 %s 失败: %v\n", ipStr, err)
			continue
		}

		// 从映射中移除
		p.createdIPv6Mutex.Lock()
		delete(p.createdIPv6Addrs, ipStr)
		p.createdIPv6Mutex.Unlock()
		p.activeIPv6Mutex.Lock()
		delete(p.activeIPv6Addrs, ipStr)
		p.activeIPv6Mutex.Unlock()

		deleted++
	}

	if deleted > 0 {
		fmt.Printf("[IP池] 热加载: 批量删除了 %d 个IPv6地址\n", deleted)
	}
}

// SetTargetIPCount 设置目标IP数量（用于IPv6地址池动态调整）
func (p *LocalIPPool) SetTargetIPCount(count int) {
	if count <= 0 {
		return
	}

	p.mu.Lock()
	p.minActiveAddrs = count  // 最小活跃地址数 = 目标IP数量
	p.maxActiveAddrs = count * 2 // 最大活跃地址数 = 目标IP数量的2倍（留有余量）
	p.mu.Unlock()

	// 如果支持IPv6，立即触发一次调整
	if p.hasIPv6Support && p.ipv6Subnet != nil {
		p.adjustIPv6AddressPool()
		// 如果当前活跃地址不足，立即批量创建
		p.activeIPv6Mutex.RLock()
		activeCount := len(p.activeIPv6Addrs)
		p.activeIPv6Mutex.RUnlock()

		if activeCount < count {
			needed := count - activeCount
			if needed > p.batchSize {
				needed = p.batchSize
			}
			p.mu.RLock()
			subnet := p.ipv6Subnet
			interfaceName := p.ipv6Interface
			p.mu.RUnlock()
			if interfaceName == "" {
				interfaceName = "ipv6net"
			}
			p.batchCreateIPv6Addresses(needed, subnet, interfaceName)
		}
	}

	fmt.Printf("[IP池] 已设置目标IP数量: %d，最小活跃地址: %d，最大活跃地址: %d\n", count, count, count*2)
}

// cleanupUnusedIPv6Addresses 每20分钟清理一次未使用的IPv6地址
func (p *LocalIPPool) cleanupUnusedIPv6Addresses() {
	p.cleanupMutex.Lock()
	now := time.Now()
	// 如果距离上次清理不足20分钟，跳过
	if !p.lastCleanupTime.IsZero() && now.Sub(p.lastCleanupTime) < 20*time.Minute {
		p.cleanupMutex.Unlock()
		return
	}
	p.lastCleanupTime = now
	p.cleanupMutex.Unlock()

	p.mu.RLock()
	hasSupport := p.hasIPv6Support
	interfaceName := p.ipv6Interface
	maxActive := p.maxActiveAddrs
	p.mu.RUnlock()

	if !hasSupport {
		return
	}

	if interfaceName == "" {
		interfaceName = "ipv6net"
	}

	// 统计当前活跃地址数量和找出未使用的地址
	p.activeIPv6Mutex.RLock()
	p.usedIPv6Mutex.RLock()

	activeCount := len(p.activeIPv6Addrs)
	
	// 找出未使用的地址
	unusedAddrs := make([]string, 0)
	for addr := range p.activeIPv6Addrs {
		if !p.usedIPv6Addrs[addr] {
			unusedAddrs = append(unusedAddrs, addr)
		}
	}

	p.usedIPv6Mutex.RUnlock()
	p.activeIPv6Mutex.RUnlock()

	// 如果活跃地址超过最大值，删除多余的未使用地址
	if maxActive > 0 && activeCount > maxActive && len(unusedAddrs) > 0 {
		excess := activeCount - maxActive
		if excess > len(unusedAddrs) {
			excess = len(unusedAddrs)
		}
		// 只删除多余的未使用地址
		p.batchDeleteIPv6Addresses(excess, interfaceName, unusedAddrs[:excess])
		fmt.Printf("[IP池] 定期清理: 删除了 %d 个未使用的IPv6地址（20分钟周期）\n", excess)
	} else if len(unusedAddrs) > 0 {
		// 即使没有超过最大值，也清理一些长期未使用的地址（保留足够的缓冲）
		// 保留至少 minActive 个地址，删除多余的未使用地址
		p.mu.RLock()
		minActive := p.minActiveAddrs
		p.mu.RUnlock()
		
		if minActive > 0 && activeCount > minActive {
			// 删除超出最小值的未使用地址的一半（避免一次性删除太多）
			toDelete := (activeCount - minActive) / 2
			if toDelete > len(unusedAddrs) {
				toDelete = len(unusedAddrs)
			}
			if toDelete > 0 {
				p.batchDeleteIPv6Addresses(toDelete, interfaceName, unusedAddrs[:toDelete])
				fmt.Printf("[IP池] 定期清理: 删除了 %d 个未使用的IPv6地址（20分钟周期，保持最小活跃数）\n", toDelete)
			}
		}
	}
}
