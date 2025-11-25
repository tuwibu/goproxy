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
		WaitProxyChange:     true,
		ChangeProxyWaitTime: 10 * time.Second,
		ProxyStrings:        []string{"tmproxy|apiKey123|456"},
		ClearAllProxy:       true,
	})

	if err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	proxies, err := pm.GetAllProxies()
	if err != nil {
		t.Fatalf("failed to get all proxies: %v", err)
	}

	for _, proxy := range proxies {
		fmt.Printf("proxy: %+v\n", proxy)
	}
}
