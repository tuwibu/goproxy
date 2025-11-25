package goproxy

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestProxyManager(t *testing.T) {
	pm, err := GetInstance(context.Background())
	if err != nil {
		t.Fatalf("failed to get proxy manager: %v", err)
	}
	defer pm.Close()

	err = pm.SetConfig(Config{
		ChangeProxyWaitTime: 10 * time.Second,
		ProxyStrings: []string{
			"tmproxy|19bf363d47582f77ba57283b6a6b2b88|241",
			"static|1.2.3.4:8080",
			"mobilehop|199.188.89.152:8000:proxy:ROpfww2|https://portal.mobilehop.com/proxies/15cd81c0d7554da594e9edde905e4b2f/reset",
		},
		ClearAllProxy: false,
		MaxUsed:       10,
	})

	if err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	// Test GetAvailableProxy (tự động gọi AcquireProxy, trả id + proxyStr)
	fmt.Println("\n=== GetAvailableProxy ===")
	proxyID, proxyStr, err := pm.GetAvailableProxy()
	if err != nil {
		t.Fatalf("failed to get available proxy: %v", err)
	}
	fmt.Printf("ID=%d, ProxyStr=%s\n", proxyID, proxyStr)

	for i := 0; i < 100; i++ {
		// Test GetAvailableProxy lần 2
		fmt.Println("\n=== GetAvailableProxy (2nd call) ===")
		proxyID2, proxyStr2, err := pm.GetAvailableProxy()
		if err != nil {
			t.Fatalf("failed to get available proxy 2nd time: %v", err)
		}
		fmt.Printf("ID=%d, ProxyStr=%s\n", proxyID2, proxyStr2)

		// Release lại
		if err := pm.ReleaseProxy(proxyID2); err != nil {
			t.Fatalf("failed to release proxy 2nd time: %v", err)
		}
	}
}
