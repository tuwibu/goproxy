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


// ProxyManager quản lý danh sách proxy (Singleton)
type ProxyManager struct {
	ctx                 context.Context
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
func GetInstance(ctx context.Context) (*ProxyManager, error) {
	var err error
	once.Do(func() {
		instance, err = newProxyManager(ctx)
	})
	return instance, err
}

// newProxyManager khởi tạo ProxyManager mới
func newProxyManager(ctx context.Context) (*ProxyManager, error) {
	db, err := initDB("proxy.db")
	if err != nil {
		return nil, err
	}

	pm := &ProxyManager{
		ctx:        ctx,
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
	ClearAllProxy       bool
}

func (pm *ProxyManager) validateProxyType(t ProxyType) error {
	switch t {
	case ProxyTypeTMProxy, ProxyTypeStatic, ProxyTypeMobileHop:
		return nil
	}
	return fmt.Errorf("invalid type: %s", t)
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
func (pm *ProxyManager) WaitProxyChange(ctx context.Context, proxy *Proxy, proxyStr string, auto bool) (string, error) {
	if proxy.Type == ProxyTypeStatic {
		return "", fmt.Errorf("static proxy cannot change IP")
	}

	if _, err := parseProxyString(proxyStr); err != nil {
		return "", err
	}

	initialResp, err := CheckProxy(ctx, proxyStr)
	if err != nil {
		return "", err
	}

	// MobileHop luôn gọi change_url
	if proxy.Type == ProxyTypeMobileHop || auto {
		if proxy.ChangeUrl == "" {
			return "", fmt.Errorf("no change_url configured")
		}
		if err := pm.callChangeURL(ctx, proxy.ChangeUrl); err != nil {
			return "", err
		}
		return pm.waitForIPChange(ctx, proxyStr, initialResp.Query, 300*time.Second)
	}

	// auto=false: check LastIP
	p, err := pm.GetProxyByID(proxy.ID)
	if err != nil {
		return "", err
	}
	if p.LastIP != "" && p.LastIP != initialResp.Query {
		return p.LastIP, nil
	}
	return "", fmt.Errorf("IP not changed")
}
