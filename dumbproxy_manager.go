package goproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tuwibu/goproxy/pkg/dumbproxy/auth"
	"github.com/tuwibu/goproxy/pkg/dumbproxy/dialer"
	"github.com/tuwibu/goproxy/pkg/dumbproxy/handler"
)

const BasePort = 20000

// DumbProxyInstance đại diện cho một instance dumbproxy đang chạy
type DumbProxyInstance struct {
	ProxyID    int64
	Port       int
	Server     *http.Server
	Listener   net.Listener
	CancelFunc context.CancelFunc
}

// Stop dừng instance
func (i *DumbProxyInstance) Stop() {
	if i.CancelFunc != nil {
		i.CancelFunc()
	}
	if i.Server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		i.Server.Shutdown(ctx)
	}
	if i.Listener != nil {
		i.Listener.Close()
	}
}

// DumbProxyManager quản lý các dumbproxy instances
type DumbProxyManager struct {
	instances map[int64]*DumbProxyInstance
	mu        sync.RWMutex
}

var (
	dumbProxyManager     *DumbProxyManager
	dumbProxyManagerOnce sync.Once
)

// GetDumbProxyManager trả về singleton instance của DumbProxyManager
func GetDumbProxyManager() *DumbProxyManager {
	dumbProxyManagerOnce.Do(func() {
		dumbProxyManager = &DumbProxyManager{
			instances: make(map[int64]*DumbProxyInstance),
		}
	})
	return dumbProxyManager
}

// StartInstance khởi động một dumbproxy instance mới cho proxy
// upstreamProxyStr: format "host:port:user:pass" hoặc "host:port"
// Trả về connection string (localhost:port)
func (m *DumbProxyManager) StartInstance(proxyID int64, upstreamProxyStr string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop existing instance if any
	if existing, ok := m.instances[proxyID]; ok {
		existing.Stop()
		delete(m.instances, proxyID)
	}

	port := BasePort + int(proxyID)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Create direct dialer (không qua proxy - dùng cho static assets)
	directDialer := dialer.NewBoundDialer(new(net.Dialer), "")

	// Create upstream dialer (qua proxy - dùng cho các request khác)
	upstreamURL := formatProxyURL(upstreamProxyStr)
	upstreamDialer, err := dialer.ProxyDialerFromURL(upstreamURL, directDialer)
	if err != nil {
		return "", fmt.Errorf("failed to create upstream dialer: %w", err)
	}

	// Create asset routing dialer
	assetDialer := dialer.NewAssetRoutingDialer(directDialer, upstreamDialer)

	// Create HTTP server with proxy handler
	proxyHandler := handler.NewProxyHandler(&handler.Config{
		Dialer: assetDialer,
		Auth:   auth.NoAuth{},
	})

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := &http.Server{
		Handler: proxyHandler,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	instance := &DumbProxyInstance{
		ProxyID:    proxyID,
		Port:       port,
		Server:     server,
		Listener:   listener,
		CancelFunc: cancel,
	}

	m.instances[proxyID] = instance

	// Start serving in background
	go func() {
		server.Serve(listener)
	}()

	return addr, nil
}

// StopInstance dừng dumbproxy instance cho proxy
func (m *DumbProxyManager) StopInstance(proxyID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if instance, ok := m.instances[proxyID]; ok {
		instance.Stop()
		delete(m.instances, proxyID)
	}
	return nil
}

// StopAll dừng tất cả dumbproxy instances
func (m *DumbProxyManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, instance := range m.instances {
		instance.Stop()
		delete(m.instances, id)
	}
}

// GetInstanceCount trả về số lượng instances đang chạy
func (m *DumbProxyManager) GetInstanceCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.instances)
}

// formatProxyURL chuyển đổi proxy_str sang URL format
// "host:port" -> "http://host:port"
// "host:port:user:pass" -> "http://user:pass@host:port"
func formatProxyURL(proxyStr string) string {
	parts := strings.Split(proxyStr, ":")
	if len(parts) == 2 {
		// host:port format
		return fmt.Sprintf("http://%s", proxyStr)
	} else if len(parts) == 4 {
		// host:port:user:pass format
		return fmt.Sprintf("http://%s:%s@%s:%s", parts[2], parts[3], parts[0], parts[1])
	}
	// Fallback
	return fmt.Sprintf("http://%s", proxyStr)
}
