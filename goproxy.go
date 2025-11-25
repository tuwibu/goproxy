package goproxy

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// ProxyType định nghĩa loại proxy
type ProxyType string

const (
	ProxyTypeTMProxy   ProxyType = "tmproxy"
	ProxyTypeStatic    ProxyType = "static"
	ProxyTypeMobileHop ProxyType = "mobilehop"
)

// Proxy đại diện cho một proxy entry
type Proxy struct {
	ID          int64
	Type        ProxyType
	ProxyStr    string
	ApiKey      string
	ChangeUrl   string
	MinTime     int  // thời gian tối thiểu giữa các lần thay đổi (giây)
	Used        bool // cờ chỉ proxy có đang được sử dụng hay không
	Count       int  // số lần proxy đã được sử dụng
	LastChanged time.Time
	LastIP      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func timeNow() time.Time {
	return time.Now()
}

// ProxyManager quản lý danh sách proxy (Singleton)
type ProxyManager struct {
	db                  *sql.DB
	mu                  sync.RWMutex
	waitProxyChange     bool
	changeProxyWaitTime time.Duration
	proxyCache          map[int64]*Proxy
	initialized         bool
}

var (
	instance *ProxyManager
	once     sync.Once
)

// GetInstance trả về singleton instance của ProxyManager
func GetInstance() (*ProxyManager, error) {
	var err error
	once.Do(func() {
		instance, err = newProxyManager()
	})
	return instance, err
}

// newProxyManager khởi tạo ProxyManager mới
func newProxyManager() (*ProxyManager, error) {
	db, err := initDB("proxy.db")
	if err != nil {
		return nil, err
	}

	pm := &ProxyManager{
		db:         db,
		proxyCache: make(map[int64]*Proxy),
	}

	// Khởi tạo schema
	if err := pm.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	pm.initialized = true
	return pm, nil
}

type Config struct {
	WaitProxyChange     bool
	ChangeProxyWaitTime time.Duration
	ProxyStrings        []string
	ClearAllProxy       bool // nếu true, xóa tất cả proxy trước khi add
}

func (pm *ProxyManager) SetConfig(config Config) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.waitProxyChange = config.WaitProxyChange
	pm.changeProxyWaitTime = config.ChangeProxyWaitTime

	// Nếu ClearAllProxy = true, xóa tất cả proxy trước
	if config.ClearAllProxy {
		_, err := pm.db.Exec("DELETE FROM proxies")
		if err != nil {
			return fmt.Errorf("failed to clear proxies: %w", err)
		}
		pm.proxyCache = make(map[int64]*Proxy)
	}

	_, err := pm.LoadProxiesFromList(config.ProxyStrings)
	if err != nil {
		return fmt.Errorf("failed to load proxies: %w", err)
	}
	return nil
}

// WaitProxyChange chờ proxy thay đổi IP hoặc tự động gọi change API
// proxy: proxy object từ database
// proxyStr: format "ip:port" hoặc "ip:port:username:password"
// auto: nếu true, tự động gọi change_url API để đổi proxy, nếu false chỉ check IP từ database
// Returns: IP mới của proxy hoặc error
func (pm *ProxyManager) WaitProxyChange(ctx context.Context, proxy *Proxy, proxyStr string, auto bool) (string, error) {
	// Static proxy không thể thay đổi IP
	if proxy.Type == ProxyTypeStatic {
		return "", fmt.Errorf("static proxy cannot change IP")
	}

	// Kiểm tra định dạng proxy string
	_, err := parseProxyString(proxyStr)
	if err != nil {
		return "", fmt.Errorf("invalid proxy string: %w", err)
	}

	// Lấy IP hiện tại của proxy
	initialResp, err := CheckProxy(ctx, proxyStr)
	if err != nil {
		return "", fmt.Errorf("failed to get initial proxy IP: %w", err)
	}
	initialIP := initialResp.Query

	if proxy.Type == ProxyTypeMobileHop {
		// MobileHop: ngay lập tức gọi change_url API
		if proxy.ChangeUrl == "" {
			return "", fmt.Errorf("mobilehop proxy has no change_url configured")
		}

		if err := pm.callChangeURL(ctx, proxy.ChangeUrl); err != nil {
			return "", fmt.Errorf("failed to call change_url: %w", err)
		}

		// Chờ và kiểm tra IP thay đổi (timeout: 300 giây)
		return pm.waitForIPChange(ctx, proxyStr, initialIP, 300*time.Second)
	}

	// TMProxy: nếu auto=true thì gọi API để đổi proxy
	if auto {
		if proxy.ChangeUrl != "" {
			if err := pm.callChangeURL(ctx, proxy.ChangeUrl); err != nil {
				return "", fmt.Errorf("failed to call change_url: %w", err)
			}
			// Chờ và kiểm tra IP thay đổi (timeout: 300 giây)
			return pm.waitForIPChange(ctx, proxyStr, initialIP, 300*time.Second)
		}
		return "", fmt.Errorf("tmproxy has no change_url configured")
	}

	// auto=false: chỉ check xem IP có đã thay đổi trong database không
	// Nếu IP thay đổi, return IP mới, nếu không return error
	updatedProxy, err := pm.GetProxyByID(proxy.ID)
	if err != nil {
		return "", fmt.Errorf("failed to get updated proxy: %w", err)
	}

	if updatedProxy.LastIP != "" && updatedProxy.LastIP != initialIP {
		return updatedProxy.LastIP, nil
	}

	return "", fmt.Errorf("proxy IP has not changed yet, call with auto=true to force change")
}
