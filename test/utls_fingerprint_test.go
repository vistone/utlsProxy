package test

import (
	"strings"
	"testing"
	"utlsProxy/src"
)

// TestNewLibrary 测试创建指纹库实例
func TestNewLibrary(t *testing.T) {
	library := src.NewLibrary()
	if library == nil {
		t.Error("NewLibrary应该返回一个有效的指纹库实例")
	}
}

// TestGetRandomFingerprint 测试获取随机指纹
func TestGetRandomFingerprint(t *testing.T) {
	profile := src.GetRandomFingerprint()
	if profile.Name == "" {
		t.Error("随机指纹应该有名称")
	}
	if profile.UserAgent == "" {
		t.Error("随机指纹应该有UserAgent")
	}
	if profile.Browser == "" {
		t.Error("随机指纹应该有浏览器信息")
	}
}

// TestLibraryAll 测试获取所有配置文件
func TestLibraryAll(t *testing.T) {
	library := src.NewLibrary()
	profiles := library.All()
	if len(profiles) == 0 {
		t.Error("应该至少有一个配置文件")
	}
}

// TestLibraryRandomProfile 测试随机获取配置文件
func TestLibraryRandomProfile(t *testing.T) {
	library := src.NewLibrary()
	profile := library.RandomProfile()
	if profile.Name == "" {
		t.Error("随机配置文件应该有名称")
	}
	if profile.UserAgent == "" {
		t.Error("随机配置文件应该有UserAgent")
	}
}

// TestLibraryProfileByName 测试根据名称获取配置文件
func TestLibraryProfileByName(t *testing.T) {
	library := src.NewLibrary()

	// 测试存在的配置文件
	profile, err := library.ProfileByName("Chrome 133 - Windows")
	if err != nil {
		t.Errorf("应该找到配置文件，但得到错误: %v", err)
	}
	if profile == nil {
		t.Error("应该返回有效的配置文件")
	}
	if profile.Name != "Chrome 133 - Windows" {
		t.Errorf("配置文件名称不匹配，期望: Chrome 133 - Windows, 实际: %s", profile.Name)
	}

	// 测试不存在的配置文件
	profile, err = library.ProfileByName("Non-existent Browser")
	if err == nil {
		t.Error("应该返回错误，因为配置文件不存在")
	}
	if profile != nil {
		t.Error("对于不存在的配置文件，应该返回nil")
	}
}

// TestLibraryProfilesByBrowser 测试根据浏览器筛选配置文件
func TestLibraryProfilesByBrowser(t *testing.T) {
	library := src.NewLibrary()

	// 测试Chrome浏览器
	chromeProfiles := library.ProfilesByBrowser("Chrome")
	if len(chromeProfiles) == 0 {
		t.Error("应该找到Chrome浏览器的配置文件")
	}

	// 验证所有返回的配置文件都是Chrome浏览器
	for _, profile := range chromeProfiles {
		if profile.Browser != "Chrome" {
			t.Errorf("找到非Chrome浏览器的配置文件: %s", profile.Browser)
		}
	}

	// 测试不存在的浏览器
	unknownProfiles := library.ProfilesByBrowser("UnknownBrowser")
	if len(unknownProfiles) != 0 {
		t.Error("对于未知浏览器，应该返回空列表")
	}
}

// TestLibraryProfilesByPlatform 测试根据平台筛选配置文件
func TestLibraryProfilesByPlatform(t *testing.T) {
	library := src.NewLibrary()

	// 测试Windows平台
	windowsProfiles := library.ProfilesByPlatform("Windows")
	if len(windowsProfiles) == 0 {
		t.Error("应该找到Windows平台的配置文件")
	}

	// 验证所有返回的配置文件都是Windows平台
	for _, profile := range windowsProfiles {
		if profile.Platform != "Windows" {
			t.Errorf("找到非Windows平台的配置文件: %s", profile.Platform)
		}
	}

	// 测试不存在的平台
	unknownProfiles := library.ProfilesByPlatform("UnknownPlatform")
	if len(unknownProfiles) != 0 {
		t.Error("对于未知平台，应该返回空列表")
	}
}

// TestLibraryRecommendedProfiles 测试获取推荐配置文件
func TestLibraryRecommendedProfiles(t *testing.T) {
	library := src.NewLibrary()
	recommended := library.RecommendedProfiles()
	if len(recommended) == 0 {
		t.Error("应该至少有一个推荐配置文件")
	}
}

// TestLibraryRandomProfileByBrowser 测试根据浏览器随机获取配置文件
func TestLibraryRandomProfileByBrowser(t *testing.T) {
	library := src.NewLibrary()

	// 测试存在的浏览器
	profile, err := library.RandomProfileByBrowser("Chrome")
	if err != nil {
		t.Errorf("应该找到Chrome浏览器的配置文件，但得到错误: %v", err)
	}
	if profile == nil {
		t.Error("应该返回有效的配置文件")
	}
	if profile.Browser != "Chrome" {
		t.Errorf("配置文件浏览器不匹配，期望: Chrome, 实际: %s", profile.Browser)
	}

	// 测试不存在的浏览器
	profile, err = library.RandomProfileByBrowser("UnknownBrowser")
	if err == nil {
		t.Error("应该返回错误，因为浏览器不存在")
	}
	if profile != nil {
		t.Error("对于不存在的浏览器，应该返回nil")
	}
}

// TestLibraryRandomProfileByPlatform 测试根据平台随机获取配置文件
func TestLibraryRandomProfileByPlatform(t *testing.T) {
	library := src.NewLibrary()

	// 测试存在的平台
	profile, err := library.RandomProfileByPlatform("Windows")
	if err != nil {
		t.Errorf("应该找到Windows平台的配置文件，但得到错误: %v", err)
	}
	if profile == nil {
		t.Error("应该返回有效的配置文件")
	}
	if profile.Platform != "Windows" {
		t.Errorf("配置文件平台不匹配，期望: Windows, 实际: %s", profile.Platform)
	}

	// 测试不存在的平台
	profile, err = library.RandomProfileByPlatform("UnknownPlatform")
	if err == nil {
		t.Error("应该返回错误，因为平台不存在")
	}
	if profile != nil {
		t.Error("对于不存在的平台，应该返回nil")
	}
}

// TestLibrarySafeProfiles 测试获取安全配置文件
func TestLibrarySafeProfiles(t *testing.T) {
	library := src.NewLibrary()
	safeProfiles := library.SafeProfiles()
	if len(safeProfiles) == 0 {
		t.Error("应该至少有一个安全配置文件")
	}
}

// TestLibraryRandomRecommendedProfile 测试随机获取推荐配置文件
func TestLibraryRandomRecommendedProfile(t *testing.T) {
	library := src.NewLibrary()
	profile := library.RandomRecommendedProfile()
	if profile.Name == "" {
		t.Error("随机推荐配置文件应该有名称")
	}
	if profile.UserAgent == "" {
		t.Error("随机推荐配置文件应该有UserAgent")
	}
}

// TestLibraryRandomAcceptLanguage 测试随机生成Accept-Language头部
func TestLibraryRandomAcceptLanguage(t *testing.T) {
	library := src.NewLibrary()

	// 多次测试随机生成的Accept-Language
	for i := 0; i < 5; i++ {
		acceptLang := library.RandomAcceptLanguage()
		if acceptLang == "" {
			t.Error("应该生成非空的Accept-Language字符串")
		}

		// 检查基本格式
		if !strings.Contains(acceptLang, "-") {
			t.Errorf("Accept-Language应该包含语言代码: %s", acceptLang)
		}

		// 检查是否包含合法的语言代码
		foundValidLang := false
		validLangs := []string{"en-US", "en-GB", "zh-CN", "zh-TW", "ja-JP", "ko-KR"}
		for _, lang := range validLangs {
			if strings.Contains(acceptLang, lang) {
				foundValidLang = true
				break
			}
		}
		if !foundValidLang {
			t.Logf("警告: 未识别的语言代码格式: %s", acceptLang)
		}
	}
}

// TestAllLanguagesList 测试所有语言列表
func TestAllLanguagesList(t *testing.T) {
	// 检查是否定义了语言列表
	// 注意：由于allLanguages是私有变量，我们无法直接访问它
	// 但我们可以通过RandomAcceptLanguage间接测试它
	library := src.NewLibrary()
	acceptLang := library.RandomAcceptLanguage()
	if acceptLang == "" {
		t.Error("应该能够生成Accept-Language头部")
	}
}
