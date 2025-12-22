package goproxy

import (
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
	ProxyTypeSticky    ProxyType = "sticky"
	ProxyTypeKiotProxy ProxyType = "kiotproxy"
	ProxyTypeAuto      ProxyType = "auto"
	ProxyTypeIPv4Xoay  ProxyType = "ipv4xoay"
)

// Proxy đại diện cho một proxy entry
type Proxy struct {
	ID          int64
	Type        ProxyType
	ProxyStr    string
	ApiKey      string
	ChangeUrl   string
	MinTime     int  // thời gian tối thiểu giữa các lần thay đổi (giây)
	Running     bool // cờ chỉ proxy có đang được sử dụng hay không
	Used        int  // số lần proxy đã được sử dụng
	Unique      bool // có check running hay không (tmproxy/mobilehop/static=true, sticky=tùy chỉnh)
	LastChanged time.Time
	LastIP      string
	Error       string // lỗi nếu GetNewProxy thất bại
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProxyManager quản lý danh sách proxy (Singleton)
type ProxyManager struct {
	db                  *sql.DB
	mu                  sync.RWMutex
	changeProxyWaitTime time.Duration
	maxUsed             int
	isBlockAssets       bool // Cờ đánh dấu có bật chế độ block assets hay không
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

func (pm *ProxyManager) validateProxyType(t ProxyType) error {
	switch t {
	case ProxyTypeTMProxy, ProxyTypeStatic, ProxyTypeMobileHop, ProxyTypeSticky, ProxyTypeKiotProxy, ProxyTypeAuto, ProxyTypeIPv4Xoay:
		return nil
	}
	return fmt.Errorf("invalid type: %s", t)
}

type Config struct {
	ChangeProxyWaitTime time.Duration
	ProxyStrings        []string
	ClearAllProxy       bool
	MaxUsed             int
	IsBlockAssets       bool // Nếu true, tạo local dumbproxy instance để block static assets
}

func (pm *ProxyManager) SetConfig(config Config) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.changeProxyWaitTime = config.ChangeProxyWaitTime

	// Nếu IsBlockAssets thay đổi hoặc ClearAllProxy, dừng tất cả dumbproxy instances
	if config.ClearAllProxy || pm.isBlockAssets != config.IsBlockAssets {
		GetDumbProxyManager().StopAll()
	}

	pm.isBlockAssets = config.IsBlockAssets

	if config.ClearAllProxy {
		pm.db.Exec("DELETE FROM proxies")
		pm.proxyCache = make(map[int64]*Proxy)
	} else {
		// Reset tất cả proxy: used=0, running=false, error=''
		pm.db.Exec("UPDATE proxies SET used=0, running=false, error='', updated_at=?", time.Now())
		for _, p := range pm.proxyCache {
			p.Used = 0
			p.Running = false
			p.Error = ""
			p.UpdatedAt = time.Now()
		}
	}

	ids, err := pm.LoadProxiesFromList(config.ProxyStrings)
	if err != nil {
		return fmt.Errorf("failed to load proxies: %w", err)
	}

	// Nếu IsBlockAssets được bật, khởi động dumbproxy instances
	if config.IsBlockAssets {
		for _, id := range ids {
			if proxy, ok := pm.proxyCache[id]; ok && proxy.ProxyStr != "" {
				_, err := GetDumbProxyManager().StartInstance(id, proxy.ProxyStr)
				if err != nil {
					// Log error nhưng tiếp tục
					continue
				}
			}
		}
	}

	// Lưu MaxUsed vào ProxyManager (thêm field mới)
	pm.maxUsed = config.MaxUsed
	return nil
}
