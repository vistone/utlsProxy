package src // Package src 定义src包

import ( // 导入所需的标准库和第三方库
	"fmt"       // 用于格式化输入输出
	"math/rand" // 用于随机数生成
	"strings"   // 用于字符串操作
	"sync"      // 用于同步原语如互斥锁
	"time"      // 用于时间处理

	utls "github.com/refraction-networking/utls" // 导入utls库用于TLS指纹伪装
)

// Profile 定义了浏览器指纹配置文件结构体
type Profile struct { // 定义配置文件结构体
	Name        string             // 配置文件名称
	HelloID     utls.ClientHelloID // TLS握手标识
	UserAgent   string             // 用户代理字符串
	Description string             // 描述信息
	Platform    string             // 平台信息
	Browser     string             // 浏览器信息
	Version     string             // 版本信息
}

// Library 定义了指纹库结构体
type Library struct { // 定义指纹库结构体
	profiles []Profile  // 配置文件列表
	rand     *rand.Rand // 随机数生成器
	mu       sync.Mutex // 互斥锁，保护随机数生成器并发安全
}

// 定义所有支持的语言列表
var allLanguages = []string{ // 所有支持的语言代码列表
	"en-US", // 美国英语
	"en-GB", // 英国英语
	"es-ES", // 西班牙语（西班牙）
	"es-MX", // 西班牙语（墨西哥）
	"fr-FR", // 法语（法国）
	"fr-CA", // 法语（加拿大）
	"de-DE", // 德语（德国）
	"ru-RU", // 俄语（俄罗斯）
	"zh-CN", // 中文（简体）
	"zh-TW", // 中文（繁体）
	"zh-HK", // 中文（香港）
	"ja-JP", // 日语（日本）
	"ko-KR", // 韩语（韩国）
	"pt-BR", // 葡萄牙语（巴西）
	"pt-PT", // 葡萄牙语（葡萄牙）
	"it-IT", // 意大利语（意大利）
	"nl-NL", // 荷兰语（荷兰）
	"sv-SE", // 瑞典语（瑞典）
	"no-NO", // 挪威语（挪威）
	"da-DK", // 丹麦语（丹麦）
	"fi-FI", // 芬兰语（芬兰）
	"pl-PL", // 波兰语（波兰）
	"tr-TR", // 土耳其语（土耳其）
	"cs-CZ", // 捷克语（捷克）
	"sk-SK", // 斯洛伐克语（斯洛伐克）
	"hu-HU", // 匈牙利语（匈牙利）
	"ro-RO", // 罗马尼亚语（罗马尼亚）
	"bg-BG", // 保加利亚语（保加利亚）
	"uk-UA", // 乌克兰语（乌克兰）
	"el-GR", // 希腊语（希腊）
	"he-IL", // 希伯来语（以色列）
	"ar-SA", // 阿拉伯语（沙特阿拉伯）
	"ar-EG", // 阿拉伯语（埃及）
	"fa-IR", // 波斯语（伊朗）
	"hi-IN", // 印地语（印度）
	"bn-IN", // 孟加拉语（印度）
	"ur-PK", // 乌尔都语（巴基斯坦）
	"th-TH", // 泰语（泰国）
	"vi-VN", // 越南语（越南）
	"ms-MY", // 马来语（马来西亚）
	"id-ID", // 印度尼西亚语（印度尼西亚）
	"et-EE", // 爱沙尼亚语（爱沙尼亚）
	"lv-LV", // 拉脱维亚语（拉脱维亚）
	"lt-LT", // 立陶宛语（立陶宛）
	"hr-HR", // 克罗地亚语（克罗地亚）
	"sl-SI", // 斯洛文尼亚语（斯洛文尼亚）
	"sr-RS", // 塞尔维亚语（塞尔维亚）
	"is-IS", // 冰岛语（冰岛）
	"ga-IE", // 爱尔兰语（爱尔兰）
	"af-ZA", // 南非荷兰语（南非）
	"sw-KE", // 斯瓦希里语（肯尼亚）
	"ta-IN", // 泰米尔语（印度）
	"ta-LK", // 泰米尔语（斯里兰卡）
	"mr-IN", // 马拉地语（印度）
	"kn-IN", // 卡纳达语（印度）
	"ml-IN", // 马拉雅拉姆语（印度）
	"te-IN", // 泰卢固语（印度）
}

// Global fingerprint library instance
// 全局指纹库实例
var fpLibrary = NewLibrary() // 创建全局指纹库实例

// NewLibrary 创建并初始化一个新的指纹库实例
// 返回值：初始化后的指纹库指针
func NewLibrary() *Library {
	lib := &Library{ // 创建指纹库实例
		rand: rand.New(rand.NewSource(time.Now().UnixNano())), // 初始化随机数生成器
	}
	lib.initProfiles() // 初始化配置文件
	return lib         // 返回指纹库实例
}

// GetRandomFingerprint 提供一种简单的方法来获取随机指纹配置文件。
// 返回值：随机选择的配置文件
func GetRandomFingerprint() Profile {
	return fpLibrary.RandomProfile() // 从全局指纹库获取随机配置文件
}

// initProfiles 初始化所有支持的浏览器指纹配置文件
func (lib *Library) initProfiles() {
	lib.profiles = []Profile{ // 初始化配置文件列表
		{ // Chrome 133 - Windows配置
			Name:        "Chrome 133 - Windows",                                                                                            // 配置名称
			HelloID:     utls.HelloChrome_133,                                                                                              // TLS握手标识
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36", // 用户代理
			Description: "Chrome 133 on Windows 10/11",                                                                                     // 描述信息
			Platform:    "Windows",                                                                                                         // 平台信息
			Browser:     "Chrome",                                                                                                          // 浏览器信息
			Version:     "133",                                                                                                             // 版本信息
		},
		{ // Chrome 133 - macOS配置
			Name:        "Chrome 133 - macOS",
			HelloID:     utls.HelloChrome_133,
			UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
			Description: "Chrome 133 on macOS",
			Platform:    "macOS",
			Browser:     "Chrome",
			Version:     "133",
		},
		{ // Chrome 131 - Windows配置
			Name:        "Chrome 131 - Windows",
			HelloID:     utls.HelloChrome_131,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			Description: "Chrome 131 on Windows 10/11",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "131",
		},
		{ // Chrome 131 - macOS配置
			Name:        "Chrome 131 - macOS",
			HelloID:     utls.HelloChrome_131,
			UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			Description: "Chrome 131 on macOS",
			Platform:    "macOS",
			Browser:     "Chrome",
			Version:     "131",
		},
		{ // Chrome 120 - Windows配置
			Name:        "Chrome 120 - Windows",
			HelloID:     utls.HelloChrome_120,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			Description: "Chrome 120 on Windows 10/11",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "120",
		},
		{ // Chrome 120 - Linux配置
			Name:        "Chrome 120 - Linux",
			HelloID:     utls.HelloChrome_120,
			UserAgent:   "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			Description: "Chrome 120 on Linux",
			Platform:    "Linux",
			Browser:     "Chrome",
			Version:     "120",
		},
		{ // Chrome 115 PQ - Windows配置
			Name:        "Chrome 115 PQ - Windows",
			HelloID:     utls.HelloChrome_115_PQ,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36",
			Description: "Chrome 115 with Post-Quantum on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "115-PQ",
		},
		{ // Chrome 114 - Windows配置
			Name:        "Chrome 114 - Windows",
			HelloID:     utls.HelloChrome_114_Padding_PSK_Shuf,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36",
			Description: "Chrome 114 with advanced features on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "114",
		},
		{ // Chrome 112 - Windows配置
			Name:        "Chrome 112 - Windows",
			HelloID:     utls.HelloChrome_112_PSK_Shuf,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/112.0.0.0 Safari/537.36",
			Description: "Chrome 112 with PSK shuffle on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "112",
		},
		{ // Chrome 106 Shuffle - Windows配置
			Name:        "Chrome 106 Shuffle - Windows",
			HelloID:     utls.HelloChrome_106_Shuffle,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36",
			Description: "Chrome 106 with shuffle on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "106",
		},
		{ // Chrome 102 - Windows配置
			Name:        "Chrome 102 - Windows",
			HelloID:     utls.HelloChrome_102,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.0.0 Safari/537.36",
			Description: "Chrome 102 on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "102",
		},
		{ // Chrome 100 - Windows配置
			Name:        "Chrome 100 - Windows",
			HelloID:     utls.HelloChrome_100,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.0.0 Safari/537.36",
			Description: "Chrome 100 on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "100",
		},
		{ // Chrome 96 - Windows配置
			Name:        "Chrome 96 - Windows",
			HelloID:     utls.HelloChrome_96,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.0.0 Safari/537.36",
			Description: "Chrome 96 on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "96",
		},
		{ // Chrome 87 - Windows配置
			Name:        "Chrome 87 - Windows",
			HelloID:     utls.HelloChrome_87,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.0.0 Safari/537.36",
			Description: "Chrome 87 on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "87",
		},
		{ // Chrome 83 - Windows配置
			Name:        "Chrome 83 - Windows",
			HelloID:     utls.HelloChrome_83,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/83.0.0.0 Safari/537.36",
			Description: "Chrome 83 on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "83",
		},
		{ // Chrome Auto - Windows配置
			Name:        "Chrome Auto - Windows",
			HelloID:     utls.HelloChrome_Auto,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			Description: "Chrome latest on Windows",
			Platform:    "Windows",
			Browser:     "Chrome",
			Version:     "auto",
		},
		{ // Firefox 120 - Windows配置
			Name:        "Firefox 120 - Windows",
			HelloID:     utls.HelloFirefox_120,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
			Description: "Firefox 120 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "120",
		},
		{ // Firefox 120 - macOS配置
			Name:        "Firefox 120 - macOS",
			HelloID:     utls.HelloFirefox_120,
			UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:120.0) Gecko/20100101 Firefox/120.0",
			Description: "Firefox 120 on macOS",
			Platform:    "macOS",
			Browser:     "Firefox",
			Version:     "120",
		},
		{ // Firefox 105 - Windows配置
			Name:        "Firefox 105 - Windows",
			HelloID:     utls.HelloFirefox_105,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:105.0) Gecko/20100101 Firefox/105.0",
			Description: "Firefox 105 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "105",
		},
		{ // Firefox 102 - Windows配置
			Name:        "Firefox 102 - Windows",
			HelloID:     utls.HelloFirefox_102,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:102.0) Gecko/20100101 Firefox/102.0",
			Description: "Firefox 102 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "102",
		},
		{ // Firefox 99 - Windows配置
			Name:        "Firefox 99 - Windows",
			HelloID:     utls.HelloFirefox_99,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:99.0) Gecko/20100101 Firefox/99.0",
			Description: "Firefox 99 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "99",
		},
		{ // Firefox 65 - Windows配置
			Name:        "Firefox 65 - Windows",
			HelloID:     utls.HelloFirefox_65,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:65.0) Gecko/20100101 Firefox/65.0",
			Description: "Firefox 65 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "65",
		},
		{ // Firefox 63 - Windows配置
			Name:        "Firefox 63 - Windows",
			HelloID:     utls.HelloFirefox_63,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:63.0) Gecko/20100101 Firefox/63.0",
			Description: "Firefox 63 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "63",
		},
		{ // Firefox 56 - Windows配置
			Name:        "Firefox 56 - Windows",
			HelloID:     utls.HelloFirefox_56,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:56.0) Gecko/20100101 Firefox/56.0",
			Description: "Firefox 56 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "56",
		},
		{ // Firefox 55 - Windows配置
			Name:        "Firefox 55 - Windows",
			HelloID:     utls.HelloFirefox_55,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:55.0) Gecko/20100101 Firefox/55.0",
			Description: "Firefox 55 on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "55",
		},
		{ // Firefox Auto - Windows配置
			Name:        "Firefox Auto - Windows",
			HelloID:     utls.HelloFirefox_Auto,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
			Description: "Firefox latest on Windows",
			Platform:    "Windows",
			Browser:     "Firefox",
			Version:     "auto",
		},
		{ // Edge 106 - Windows配置
			Name:        "Edge 106 - Windows",
			HelloID:     utls.HelloEdge_106,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36 Edg/106.0.0.0",
			Description: "Edge 106 on Windows",
			Platform:    "Windows",
			Browser:     "Edge",
			Version:     "106",
		},
		{ // Edge 85 - Windows配置
			Name:        "Edge 85 - Windows",
			HelloID:     utls.HelloEdge_85,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/85.0.0.0 Safari/537.36 Edg/85.0.0.0",
			Description: "Edge 85 on Windows",
			Platform:    "Windows",
			Browser:     "Edge",
			Version:     "85",
		},
		{ // Edge Auto - Windows配置
			Name:        "Edge Auto - Windows",
			HelloID:     utls.HelloEdge_Auto,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			Description: "Edge latest on Windows",
			Platform:    "Windows",
			Browser:     "Edge",
			Version:     "auto",
		},
		{ // Safari 17 - macOS配置
			Name:        "Safari 17 - macOS",
			HelloID:     utls.HelloSafari_Auto,
			UserAgent:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
			Description: "Safari on macOS",
			Platform:    "macOS",
			Browser:     "Safari",
			Version:     "17",
		},
		{ // iOS Safari 14 - iPhone配置
			Name:        "iOS Safari 14 - iPhone",
			HelloID:     utls.HelloIOS_14,
			UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 14_7_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.2 Mobile/15E148 Safari/604.1",
			Description: "iOS Safari 14 on iPhone",
			Platform:    "iOS",
			Browser:     "Safari",
			Version:     "14",
		},
		{ // iOS Safari 13 - iPhone配置
			Name:        "iOS Safari 13 - iPhone",
			HelloID:     utls.HelloIOS_13,
			UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 13_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.1.2 Mobile/15E148 Safari/604.1",
			Description: "iOS Safari 13 on iPhone",
			Platform:    "iOS",
			Browser:     "Safari",
			Version:     "13",
		},
		{ // iOS Safari 12 - iPhone配置
			Name:        "iOS Safari 12 - iPhone",
			HelloID:     utls.HelloIOS_12_1,
			UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 12_5_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/12.1.2 Mobile/15E148 Safari/604.1",
			Description: "iOS Safari 12 on iPhone",
			Platform:    "iOS",
			Browser:     "Safari",
			Version:     "12",
		},
		{ // Randomized - Chrome Like配置
			Name:        "Randomized - Chrome Like",
			HelloID:     utls.HelloRandomized,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			Description: "Randomized fingerprint with Chrome-like UA",
			Platform:    "Random",
			Browser:     "Random",
			Version:     "random",
		},
		{ // Randomized ALPN - Chrome Like配置
			Name:        "Randomized ALPN - Chrome Like",
			HelloID:     utls.HelloRandomizedALPN,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			Description: "Randomized fingerprint with ALPN",
			Platform:    "Random",
			Browser:     "Random",
			Version:     "random",
		},
		{ // Randomized No ALPN - Firefox Like配置
			Name:        "Randomized No ALPN - Firefox Like",
			HelloID:     utls.HelloRandomizedNoALPN,
			UserAgent:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
			Description: "Randomized fingerprint without ALPN",
			Platform:    "Random",
			Browser:     "Random",
			Version:     "random",
		},
	}
}

// All 返回所有配置文件列表
// 返回值：所有配置文件的切片
func (lib *Library) All() []Profile {
	return lib.profiles // 返回所有配置文件
}

// randomIndex 生成一个安全的随机索引
// 参数：n - 范围上限
// 返回值：随机索引值
func (lib *Library) randomIndex(n int) int {
	lib.mu.Lock()           // 加锁保护随机数生成器
	defer lib.mu.Unlock()   // 延迟解锁
	return lib.rand.Intn(n) // 生成随机索引
}

// getRealBrowserProfiles 获取所有真实浏览器的指纹配置（排除Randomized类型）
// 返回值：真实浏览器指纹配置列表
func (lib *Library) getRealBrowserProfiles() []Profile {
	var realProfiles []Profile
	for _, profile := range lib.profiles {
		// 排除Randomized类型的指纹（Browser为"Random"或Platform为"Random"）
		if profile.Browser != "Random" && profile.Platform != "Random" {
			realProfiles = append(realProfiles, profile)
		}
	}
	return realProfiles
}

// RandomProfile 随机返回一个配置文件（只返回真实浏览器的指纹）
// 返回值：随机选择的配置文件
func (lib *Library) RandomProfile() Profile {
	// 只使用真实浏览器的指纹
	realProfiles := lib.getRealBrowserProfiles()
	if len(realProfiles) == 0 { // 如果真实浏览器指纹列表为空
		// 返回默认的Chrome配置
		return Profile{HelloID: utls.HelloChrome_Auto,
			UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
			Name:      "Default Chrome",
			Browser:   "Chrome",
			Platform:  "Windows"}
	}
	return realProfiles[lib.randomIndex(len(realProfiles))] // 从真实浏览器指纹中随机选择
}

// ProfileByName 根据名称查找配置文件
// 参数：name - 配置文件名称
// 返回值：配置文件指针和错误信息
func (lib *Library) ProfileByName(name string) (*Profile, error) {
	for _, profile := range lib.profiles { // 遍历配置文件列表
		if profile.Name == name { // 如果找到匹配的配置文件
			return &profile, nil // 返回配置文件指针
		}
	}
	return nil, fmt.Errorf("fingerprint not found: %s", name) // 返回错误信息
}

// ProfilesByBrowser 根据浏览器类型筛选配置文件
// 参数：browser - 浏览器类型
// 返回值：匹配的配置文件列表
func (lib *Library) ProfilesByBrowser(browser string) []Profile {
	var result []Profile                   // 初始化结果列表
	for _, profile := range lib.profiles { // 遍历配置文件列表
		if profile.Browser == browser { // 如果浏览器类型匹配
			result = append(result, profile) // 添加到结果列表
		}
	}
	return result // 返回结果列表
}

// ProfilesByPlatform 根据平台类型筛选配置文件
// 参数：platform - 平台类型
// 返回值：匹配的配置文件列表
func (lib *Library) ProfilesByPlatform(platform string) []Profile {
	var result []Profile                   // 初始化结果列表
	for _, profile := range lib.profiles { // 遍历配置文件列表
		if profile.Platform == platform { // 如果平台类型匹配
			result = append(result, profile) // 添加到结果列表
		}
	}
	return result // 返回结果列表
}

// RecommendedProfiles 返回推荐的配置文件列表（只返回真实浏览器的指纹）
// 返回值：推荐的配置文件列表
func (lib *Library) RecommendedProfiles() []Profile {
	// 只使用真实浏览器的指纹
	realProfiles := lib.getRealBrowserProfiles()
	var recommended []Profile              // 初始化推荐列表
	for _, profile := range realProfiles { // 遍历真实浏览器指纹列表
		if profile.Version == "133" || profile.Version == "131" || // 如果是推荐版本
			profile.Version == "120" || profile.Version == "auto" {
			recommended = append(recommended, profile) // 添加到推荐列表
		}
	}
	return recommended // 返回推荐列表
}

// RandomProfileByBrowser 根据浏览器类型随机返回一个配置文件
// 参数：browser - 浏览器类型
// 返回值：配置文件指针和错误信息
func (lib *Library) RandomProfileByBrowser(browser string) (*Profile, error) {
	profiles := lib.ProfilesByBrowser(browser) // 获取指定浏览器的配置文件列表
	if len(profiles) == 0 {                    // 如果列表为空
		return nil, fmt.Errorf("browser not found: %s", browser) // 返回错误信息
	}
	profile := profiles[lib.randomIndex(len(profiles))] // 随机选择一个配置文件
	return &profile, nil                                // 返回配置文件指针
}

// RandomProfileByPlatform 根据平台类型随机返回一个配置文件
// 参数：platform - 平台类型
// 返回值：配置文件指针和错误信息
func (lib *Library) RandomProfileByPlatform(platform string) (*Profile, error) {
	profiles := lib.ProfilesByPlatform(platform) // 获取指定平台的配置文件列表
	if len(profiles) == 0 {                      // 如果列表为空
		return nil, fmt.Errorf("platform not found: %s", platform) // 返回错误信息
	}
	profile := profiles[lib.randomIndex(len(profiles))] // 随机选择一个配置文件
	return &profile, nil                                // 返回配置文件指针
}

// SafeProfiles 返回安全的配置文件列表（只返回真实浏览器的指纹）
// 返回值：安全的配置文件列表
func (lib *Library) SafeProfiles() []Profile {
	// 只使用真实浏览器的指纹
	realProfiles := lib.getRealBrowserProfiles()
	var safeProfiles []Profile             // 初始化安全配置文件列表
	for _, profile := range realProfiles { // 遍历真实浏览器指纹列表
		if profile.Browser == "Firefox" || // 如果是Firefox浏览器
			profile.Version == "133" || // 或者是版本133
			profile.Version == "131" { // 或者是版本131
			safeProfiles = append(safeProfiles, profile) // 添加到安全配置文件列表
		}
	}
	return safeProfiles // 返回安全配置文件列表
}

// RandomRecommendedProfile 随机返回一个推荐的配置文件
// 返回值：随机选择的推荐配置文件
func (lib *Library) RandomRecommendedProfile() Profile {
	recommended := lib.RecommendedProfiles() // 获取推荐配置文件列表
	if len(recommended) == 0 {               // 如果列表为空
		return lib.RandomProfile() // 返回随机配置文件
	}
	return recommended[lib.randomIndex(len(recommended))] // 返回随机选择的推荐配置文件
}

// RandomAcceptLanguage 随机生成Accept-Language头部值
// 返回值：随机生成的Accept-Language字符串
func (lib *Library) RandomAcceptLanguage() string {
	minLangs := 2     // 最小语言数量
	maxLangs := 5     // 最大语言数量
	count := minLangs // 初始化语言数量

	// 由于maxLangs > minLangs始终为true，我们直接计算随机增量
	count += lib.randomIndex(maxLangs - minLangs + 1) // 随机增加语言数量

	if count > len(allLanguages) { // 如果数量超过支持的语言总数
		count = len(allLanguages) // 调整为支持的语言总数
	}

	drawn := make(map[int]struct{}, count) // 创建已选择语言映射
	selections := make([]string, 0, count) // 创建选择列表
	for len(selections) < count {          // 循环直到达到指定数量
		idx := lib.randomIndex(len(allLanguages)) // 生成随机索引
		if _, exists := drawn[idx]; exists {      // 如果已经选择过
			continue // 继续下一次循环
		}
		drawn[idx] = struct{}{}                            // 标记为已选择
		selections = append(selections, allLanguages[idx]) // 添加到选择列表
	}

	var builder strings.Builder       // 创建字符串构建器
	for i, lang := range selections { // 遍历选择的语言
		if i > 0 { // 如果不是第一个
			builder.WriteString(",") // 添加逗号分隔符
		}
		builder.WriteString(lang) // 添加语言代码
		if i > 0 {                // 如果不是第一个
			q := 1.0 - float64(i)*0.1 // 计算权重
			if q < 0.1 {              // 如果权重小于0.1
				q = 0.1 // 设置最小权重
			}
			builder.WriteString(";q=")                  // 添加权重标识
			builder.WriteString(fmt.Sprintf("%.1f", q)) // 添加权重值
		}
	}
	return builder.String() // 返回构建的字符串
}
