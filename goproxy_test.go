package goproxy

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// Test data từ user
var testProxies = []string{
	"tmproxy|464cd73372785079a71e34f29b90f035|370",                                                    // TMProxy key hợp lệ
	"tmproxy|464cd73372785079a71e34f29b90f033|370",                                                    // TMProxy key die
	"kiotproxy|K0703e37ad8b74981b2a2bf42484df44e|130",                                                 // KiotProxy key hợp lệ
	"kiotproxy|Kaea3b95e4fde4bada2d4ee6caaf4215a|130",                                                 // KiotProxy key hết hạn
	"sticky|us.arxlabs.io:3010:eqcs86352-region-Rand-sid-{random}-t-60:drlkvfbp",                      // Sticky non-unique (default)
	"sticky|unlimit5.cliproxy.io:4397:2r8CKYpzMXx7-sid-${random}-t-3:FpF2RDvu3p|true|190",             // Sticky unique với min_time
	"static|192.168.1.1:8080:user:pass",                                                               // Static proxy
	"mobilehop|192.168.1.2:8080:user:pass|https://example.com/change",                                 // MobileHop proxy
}

func TestMain(m *testing.M) {
	// Xóa database cũ trước khi test
	os.Remove("proxy.db")
	code := m.Run()
	// Dọn dẹp sau test
	os.Remove("proxy.db")
	os.Exit(code)
}

func TestLoadProxiesFromList(t *testing.T) {
	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Test load proxies
	err = pm.SetConfig(Config{
		MaxUsed:             3,
		ChangeProxyWaitTime: 0,
		ProxyStrings:        testProxies,
		ClearAllProxy:       true,
	})

	if err != nil {
		t.Logf("SetConfig returned error (expected for some invalid keys): %v", err)
	}

	// Kiểm tra error proxies
	errorProxies, err := pm.GetErrorProxies()
	if err != nil {
		t.Fatalf("GetErrorProxies failed: %v", err)
	}

	t.Logf("=== Error Proxies (%d) ===", len(errorProxies))
	for _, ep := range errorProxies {
		t.Logf("ID: %d, Type: %s, ApiKey: %s, Error: %s", ep.ID, ep.Type, ep.ApiKey, ep.Error)
	}
}

func TestGetAvailableProxy_Static(t *testing.T) {
	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Load chỉ static proxy
	err = pm.SetConfig(Config{
		MaxUsed:             2,
		ChangeProxyWaitTime: 0,
		ProxyStrings:        []string{"static|192.168.1.1:8080:user:pass"},
		ClearAllProxy:       true,
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Lấy proxy lần 1
	id1, proxy1, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed: %v", err)
	}
	t.Logf("Static proxy 1: id=%d, proxy=%s", id1, proxy1)
	pm.ReleaseProxy(id1)

	// Lấy proxy lần 2
	id2, proxy2, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed: %v", err)
	}
	t.Logf("Static proxy 2: id=%d, proxy=%s", id2, proxy2)
	pm.ReleaseProxy(id2)

	// Lần 3 phải fail vì maxUsed=2
	_, _, err = pm.GetAvailableProxy(1)
	if err == nil {
		t.Errorf("Expected error when maxUsed exceeded, but got none")
	} else {
		t.Logf("Correctly rejected: %v", err)
	}
}

func TestGetAvailableProxy_StickyNonUnique(t *testing.T) {
	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Load sticky non-unique
	err = pm.SetConfig(Config{
		MaxUsed:             3,
		ChangeProxyWaitTime: 0,
		ProxyStrings:        []string{"sticky|us.arxlabs.io:3010:user-{random}:pass"},
		ClearAllProxy:       true,
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Lấy proxy nhiều lần - mỗi lần phải có random khác nhau
	var proxies []string
	for i := 0; i < 5; i++ {
		id, proxy, err := pm.GetAvailableProxy(1)
		if err != nil {
			t.Fatalf("GetAvailableProxy failed: %v", err)
		}
		proxies = append(proxies, proxy)
		t.Logf("Sticky non-unique %d: id=%d, proxy=%s", i+1, id, proxy)
		// Không cần ReleaseProxy vì là non-unique
	}

	// Kiểm tra các proxy có random khác nhau
	uniqueProxies := make(map[string]bool)
	for _, p := range proxies {
		uniqueProxies[p] = true
	}
	if len(uniqueProxies) != len(proxies) {
		t.Logf("Warning: Some proxies have same random value (unlikely but possible)")
	}
}

func TestGetAvailableProxy_StickyUnique(t *testing.T) {
	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Load sticky unique với min_time=2s
	err = pm.SetConfig(Config{
		MaxUsed:             2,
		ChangeProxyWaitTime: 0,
		ProxyStrings:        []string{"sticky|test.com:3010:user-${random}:pass|true|2"},
		ClearAllProxy:       true,
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Lấy proxy lần 1
	id1, proxy1, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed: %v", err)
	}
	t.Logf("Sticky unique 1: id=%d, proxy=%s", id1, proxy1)

	// Không release - thử lấy lại phải fail vì running=true
	_, _, err = pm.GetAvailableProxy(1)
	if err == nil {
		t.Errorf("Expected error when proxy is running, but got none")
	} else {
		t.Logf("Correctly rejected when running: %v", err)
	}

	// Release và lấy lại
	pm.ReleaseProxy(id1)
	id2, proxy2, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed after release: %v", err)
	}
	t.Logf("Sticky unique 2: id=%d, proxy=%s", id2, proxy2)
	pm.ReleaseProxy(id2)

	// Lần 3 phải fail vì maxUsed=2
	_, _, err = pm.GetAvailableProxy(1)
	if err == nil {
		t.Errorf("Expected error when maxUsed exceeded, but got none")
	} else {
		t.Logf("Correctly rejected when maxUsed exceeded: %v", err)
	}

	// Đợi min_time rồi thử lại
	t.Logf("Waiting 2s for min_time...")
	time.Sleep(2 * time.Second)

	id3, proxy3, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed after min_time: %v", err)
	}
	t.Logf("Sticky unique 3 (after min_time): id=%d, proxy=%s", id3, proxy3)
	pm.ReleaseProxy(id3)
}

func TestProcessStickyProxyStr(t *testing.T) {
	testCases := []struct {
		input    string
		hasRandom bool
	}{
		{"test.com:8080:user-{random}:pass", true},
		{"test.com:8080:user-${random}:pass", true},
		{"test.com:8080:user:pass", false},
	}

	for _, tc := range testCases {
		result := processStickyProxyStr(tc.input)
		if tc.hasRandom {
			if result == tc.input {
				t.Errorf("Expected random to be replaced in %s, but got same string", tc.input)
			}
			t.Logf("Input: %s -> Output: %s", tc.input, result)
		} else {
			if result != tc.input {
				t.Errorf("Expected no change for %s, but got %s", tc.input, result)
			}
		}
	}
}

func TestGetErrorProxies(t *testing.T) {
	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Load proxies với key die
	err = pm.SetConfig(Config{
		MaxUsed:             3,
		ChangeProxyWaitTime: 0,
		ProxyStrings: []string{
			"tmproxy|invalid_key_12345|370",
			"kiotproxy|invalid_key_67890|130",
		},
		ClearAllProxy: true,
	})

	// Có thể có lỗi khi load
	if err != nil {
		t.Logf("SetConfig error (expected): %v", err)
	}

	// Kiểm tra error proxies
	errorProxies, err := pm.GetErrorProxies()
	if err != nil {
		t.Fatalf("GetErrorProxies failed: %v", err)
	}

	t.Logf("=== Error Proxies (%d) ===", len(errorProxies))
	for _, ep := range errorProxies {
		t.Logf("ID: %d, Type: %s, Error: %s", ep.ID, ep.Type, ep.Error)
	}
}

func TestClearProxyError(t *testing.T) {
	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Lấy error proxies
	errorProxies, _ := pm.GetErrorProxies()
	if len(errorProxies) > 0 {
		// Clear error đầu tiên
		err := pm.ClearProxyError(errorProxies[0].ID)
		if err != nil {
			t.Fatalf("ClearProxyError failed: %v", err)
		}
		t.Logf("Cleared error for proxy ID: %d", errorProxies[0].ID)

		// Kiểm tra lại
		errorProxies2, _ := pm.GetErrorProxies()
		if len(errorProxies2) >= len(errorProxies) {
			t.Errorf("Expected fewer error proxies after clear")
		}
	}
}

// Test với real API (cần uncomment để chạy)
func TestRealTMProxy(t *testing.T) {
	t.Skip("Skipping real API test - uncomment to run")

	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	err = pm.SetConfig(Config{
		MaxUsed:             3,
		ChangeProxyWaitTime: 0,
		ProxyStrings:        []string{"tmproxy|464cd73372785079a71e34f29b90f035|370"},
		ClearAllProxy:       true,
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	id, proxy, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed: %v", err)
	}
	t.Logf("TMProxy: id=%d, proxy=%s", id, proxy)
	pm.ReleaseProxy(id)

	// Check errors
	errorProxies, _ := pm.GetErrorProxies()
	for _, ep := range errorProxies {
		t.Logf("Error: %s - %s", ep.Type, ep.Error)
	}
}

func TestRealKiotProxy(t *testing.T) {
	t.Skip("Skipping real API test - uncomment to run")

	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	err = pm.SetConfig(Config{
		MaxUsed:             3,
		ChangeProxyWaitTime: 0,
		ProxyStrings:        []string{"kiotproxy|K0703e37ad8b74981b2a2bf42484df44e|130"},
		ClearAllProxy:       true,
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	id, proxy, err := pm.GetAvailableProxy(1)
	if err != nil {
		t.Fatalf("GetAvailableProxy failed: %v", err)
	}
	t.Logf("KiotProxy: id=%d, proxy=%s", id, proxy)
	pm.ReleaseProxy(id)

	// Check errors
	errorProxies, _ := pm.GetErrorProxies()
	for _, ep := range errorProxies {
		t.Logf("Error: %s - %s", ep.Type, ep.Error)
	}
}

// Test tổng hợp với tất cả proxy types
func TestAllProxyTypes(t *testing.T) {
	t.Skip("Skipping real API test - uncomment to run")

	pm, err := GetInstance()
	if err != nil {
		t.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	err = pm.SetConfig(Config{
		MaxUsed:             3,
		ChangeProxyWaitTime: 2 * time.Second,
		ProxyStrings:        testProxies,
		ClearAllProxy:       true,
	})
	if err != nil {
		t.Logf("SetConfig warning: %v", err)
	}

	// Thử lấy proxy 10 lần
	for i := 0; i < 10; i++ {
		id, proxy, err := pm.GetAvailableProxy(1)
		if err != nil {
			t.Logf("GetAvailableProxy %d failed: %v", i+1, err)
			break
		}
		t.Logf("%d. id=%d, proxy=%s", i+1, id, proxy)
		pm.ReleaseProxy(id)
	}

	// In ra các proxy lỗi
	fmt.Println("\n=== Error Proxies ===")
	errorProxies, _ := pm.GetErrorProxies()
	for _, ep := range errorProxies {
		fmt.Printf("ID: %d, Type: %s, ApiKey: %s\nError: %s\n\n", ep.ID, ep.Type, ep.ApiKey, ep.Error)
	}
}
