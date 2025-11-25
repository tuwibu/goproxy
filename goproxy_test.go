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
		ProxyStrings:        []string{"tmproxy|19bf363d47582f77ba57283b6a6b2b88|241"}, // static proxy - ko gọi GetNewProxy
		ClearAllProxy:       false,
		MaxUsed:             10,
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

	// Check state
	p, _ := pm.GetProxyByID(proxyID)
	fmt.Printf("After GetAvailableProxy: running=%v, used=%d\n", p.Running, p.Used)

	// Test ReleaseProxy
	fmt.Println("\n=== Test ReleaseProxy ===")
	if err := pm.ReleaseProxy(proxyID); err != nil {
		t.Fatalf("failed to release proxy: %v", err)
	}
	p, _ = pm.GetProxyByID(proxyID)
	fmt.Printf("After ReleaseProxy: running=%v, used=%d\n", p.Running, p.Used)

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
