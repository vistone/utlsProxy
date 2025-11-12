package src // Package src 定义src包

import "sync" // 导入同步原语如互斥锁

// IPAccessController 定义了IP访问控制的行为接口。
// 通过该接口，可以将IP访问控制的具体实现与业务逻辑解耦。
type IPAccessController interface { // 定义IP访问控制器接口
	// AddIP 将一个IP地址添加到指定的名单中。
	// 如果 isWhite 为 true，IP被添加到白名单；否则，添加到黑名单。
	AddIP(ip string, isWhite bool) // 添加IP地址方法

	// RemoveIP 从指定的名单中删除一个IP地址。
	// 如果 isWhite 为 true，则从白名单删除；否则，从黑名单删除。
	RemoveIP(ip string, isWhite bool) // 删除IP地址方法

	// IsIPAllowed 检查一个IP地址是否被允许访问。
	IsIPAllowed(ip string) bool // 检查IP地址是否被允许访问方法

	// GetAllowedIPs 返回当前白名单中所有IP地址的快照。
	GetAllowedIPs() []string // 获取允许的IP地址列表方法

	// GetBlockedIPs 返回当前黑名单中所有IP地址的快照。
	GetBlockedIPs() []string // 获取阻止的IP地址列表方法
}

// IPSet 用于高效地存储和查询IP地址集合。
type IPSet map[string]bool // 定义IP集合类型，映射IP地址到布尔值

// WhiteBlackIPPool 是 IPAccessController 接口的一个具体实现。
// 它在内存中使用两个集合来维护IP黑白名单，并保证并发安全。
type WhiteBlackIPPool struct { // 定义黑白名单IP池结构体
	whiteList IPSet        // 白名单集合
	blackList IPSet        // 黑名单集合
	mutex     sync.RWMutex // 读写互斥锁，保护集合并发安全
}

// NewWhiteBlackIPPool 创建并返回一个基于内存的IP访问控制器实例。
// 返回值：IP访问控制器接口实例
func NewWhiteBlackIPPool() IPAccessController {
	return &WhiteBlackIPPool{ // 创建并返回黑白名单IP池实例
		whiteList: make(IPSet), // 初始化白名单集合
		blackList: make(IPSet), // 初始化黑名单集合
	}
}

// AddIP 将一个IP地址添加到指定的名单中。
// 如果 isWhite 为 true，IP被添加到白名单；否则，添加到黑名单。
// 参数：
// ip - 要添加的IP地址
// isWhite - 是否添加到白名单的标志
func (pool *WhiteBlackIPPool) AddIP(ip string, isWhite bool) {
	pool.mutex.Lock()         // 加写锁
	defer pool.mutex.Unlock() // 延迟解锁
	if isWhite {              // 如果添加到白名单
		pool.whiteList[ip] = true // 将IP添加到白名单
	} else { // 如果添加到黑名单
		pool.blackList[ip] = true // 将IP添加到黑名单
	}
}

// RemoveIP 从指定的名单中删除一个IP地址。
// 如果 isWhite 为 true，则从白名单删除；否则，从黑名单删除。
// 参数：
// ip - 要删除的IP地址
// isWhite - 是否从白名单删除的标志
func (pool *WhiteBlackIPPool) RemoveIP(ip string, isWhite bool) {
	pool.mutex.Lock()         // 加写锁
	defer pool.mutex.Unlock() // 延迟解锁
	if isWhite {              // 如果从白名单删除
		delete(pool.whiteList, ip) // 从白名单删除IP
	} else { // 如果从黑名单删除
		delete(pool.blackList, ip) // 从黑名单删除IP
	}
}

// IsIPAllowed 检查一个IP地址是否被允许访问。
// 访问策略遵循"黑名单优先"和"默认拒绝"原则：
// 1. 如果IP存在于黑名单，则明确拒绝（返回 false）。
// 2. 如果IP存在于白名单，则明确允许（返回 true）。
// 3. 如果IP不在任何名单中，则默认拒绝（返回 false）。
// 参数：
// ip - 要检查的IP地址
// 返回值：IP地址是否被允许访问的布尔值
func (pool *WhiteBlackIPPool) IsIPAllowed(ip string) bool {
	pool.mutex.RLock()         // 加读锁
	defer pool.mutex.RUnlock() // 延迟解锁

	// 黑名单具有最高优先级
	if _, blackExists := pool.blackList[ip]; blackExists { // 检查IP是否在黑名单中
		return false // 如果在黑名单中，返回false
	}

	// 其次检查白名单
	if _, whiteExists := pool.whiteList[ip]; whiteExists { // 检查IP是否在白名单中
		return true // 如果在白名单中，返回true
	}

	// 默认拒绝
	return false // 默认返回false
}

// GetAllowedIPs 返回当前白名单中所有IP地址的快照。
// 返回值：白名单中所有IP地址的切片
func (pool *WhiteBlackIPPool) GetAllowedIPs() []string {
	pool.mutex.RLock()         // 加读锁
	defer pool.mutex.RUnlock() // 延迟解锁

	allowedIPs := make([]string, 0, len(pool.whiteList)) // 创建允许的IP地址切片
	for ip := range pool.whiteList {                     // 遍历白名单
		allowedIPs = append(allowedIPs, ip) // 添加到允许的IP地址切片
	}
	return allowedIPs // 返回允许的IP地址切片
}

// GetBlockedIPs 返回当前黑名单中所有IP地址的快照。
// 返回值：黑名单中所有IP地址的切片
func (pool *WhiteBlackIPPool) GetBlockedIPs() []string {
	pool.mutex.RLock()         // 加读锁
	defer pool.mutex.RUnlock() // 延迟解锁

	blockedIPs := make([]string, 0, len(pool.blackList)) // 创建阻止的IP地址切片
	for ip := range pool.blackList {                     // 遍历黑名单
		blockedIPs = append(blockedIPs, ip) // 添加到阻止的IP地址切片
	}
	return blockedIPs // 返回阻止的IP地址切片
}
