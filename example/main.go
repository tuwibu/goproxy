package main

import (
	"fmt"
	"log"
	"time"

	"github.com/tuwibu/goproxy"
)

func main() {
	// Khởi tạo ProxyManager
	pm, err := goproxy.GetInstance()
	if err != nil {
		log.Fatalf("Failed to get ProxyManager instance: %v", err)
	}

	// Cấu hình với IsBlockAssets = true
	config := goproxy.Config{
		ChangeProxyWaitTime: 30 * time.Second,
		ProxyStrings: []string{
			"kiotproxy|K0703e37ad8b74981b2a2bf42484df44e|130",
		},
		ClearAllProxy: true,
		MaxUsed:       10,
		IsBlockAssets: true, // Bật chế độ block static assets
	}

	err = pm.SetConfig(config)
	if err != nil {
		log.Fatalf("Failed to set config: %v", err)
	}

	fmt.Println("=== IsBlockAssets Test ===")
	fmt.Printf("Config:\n")
	fmt.Printf("  - ProxyStrings: %v\n", config.ProxyStrings)
	fmt.Printf("  - IsBlockAssets: %v\n", config.IsBlockAssets)
	fmt.Printf("  - MaxUsed: %d\n", config.MaxUsed)
	fmt.Println()

	// Lấy proxy (threadId = 1)
	proxyID, proxyStr, err := pm.GetAvailableProxy(1)
	if err != nil {
		log.Fatalf("Failed to get available proxy: %v", err)
	}

	fmt.Println("=== Proxy Info ===")
	fmt.Printf("ID: %d\n", proxyID)
	fmt.Printf("ProxyStr (ConnectionInfo): %s\n", proxyStr)
	fmt.Println()

	// Kiểm tra kết quả
	if config.IsBlockAssets {
		expectedPort := 20000 + int(proxyID)
		expectedConnectionInfo := fmt.Sprintf("127.0.0.1:%d", expectedPort)

		fmt.Println("=== Verification ===")
		fmt.Printf("Expected ConnectionInfo: %s\n", expectedConnectionInfo)
		fmt.Printf("Actual ConnectionInfo: %s\n", proxyStr)

		if proxyStr == expectedConnectionInfo {
			fmt.Println("SUCCESS: ConnectionInfo is correct!")
			fmt.Println()
			fmt.Println("Khi IsBlockAssets = true:")
			fmt.Println("  - Static assets (js, css, images, fonts, video, audio) -> Direct connection")
			fmt.Println("  - Other requests -> Via upstream proxy")
		} else {
			fmt.Println("FAILED: ConnectionInfo mismatch!")
		}
	}

	// Hiển thị số lượng dumbproxy instances đang chạy
	instanceCount := goproxy.GetDumbProxyManager().GetInstanceCount()
	fmt.Printf("\nDumbProxy instances running: %d\n", instanceCount)

	// Giữ chương trình chạy để test (nhấn Ctrl+C để dừng)
	fmt.Println("\nPress Ctrl+C to stop...")
	select {}
}
