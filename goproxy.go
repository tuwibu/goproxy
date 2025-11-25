package goproxy

import (
	"database/sql"
	"fmt"
	"strings"
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

// GetAllProxies lấy tất cả proxy
func (pm *ProxyManager) GetAllProxies() ([]*Proxy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, last_changed, last_ip, created_at, updated_at
		FROM proxies
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query proxies: %w", err)
	}
	defer rows.Close()

	var proxies []*Proxy
	for rows.Next() {
		var p Proxy
		var lastChanged sql.NullTime
		var lastIP sql.NullString
		err := rows.Scan(
			&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey,
			&lastChanged, &lastIP, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan proxy: %w", err)
		}
		if lastChanged.Valid {
			p.LastChanged = lastChanged.Time
		}
		if lastIP.Valid {
			p.LastIP = lastIP.String
		}
		proxies = append(proxies, &p)
	}

	return proxies, rows.Err()
}

// LoadProxiesFromList load danh sách proxy từ format string
// Format: type|proxyStr[|apiKey][|changeUrl]
// Ví dụ:
//
//	tmproxy|apiKey123
//	static|192.168.1.1:8080
//	static|192.168.1.1:8080:user:pass
//	mobilehop|192.168.1.1:8080:user:pass|https://example.com/change
func (pm *ProxyManager) loadProxiesFromList(proxyStrings []string) ([]int64, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var ids []int64

	for _, proxyStr := range proxyStrings {
		parts := strings.Split(strings.TrimSpace(proxyStr), "|")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid proxy format: %s", proxyStr)
		}

		proxyType := ProxyType(parts[0])
		if err := pm.validateProxyType(proxyType); err != nil {
			return nil, fmt.Errorf("invalid proxy: %s - %w", proxyStr, err)
		}

		var proxyString *string
		var apiKey *string
		changeUrl := ""

		// Detect if parts[1] is proxyStr or apiKey by checking for ':'
		if strings.Contains(parts[1], ":") {
			// parts[1] là proxyStr (ip:port)
			proxyString = &parts[1]
		} else {
			// parts[1] là apiKey (tmproxy case)
			apiKey = &parts[1]
		}

		// changeUrl từ parts[2] nếu có
		if len(parts) > 2 && parts[2] != "" {
			changeUrl = parts[2]
		}

		proxy, err := pm.addProxy(AddProxyInput{
			Type:      proxyType,
			ProxyStr:  proxyString,
			ApiKey:    apiKey,
			ChangeUrl: changeUrl,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to add proxy: %s - %w", proxyStr, err)
		}

		ids = append(ids, proxy.ID)
	}

	return ids, nil
}

// validateProxyType kiểm tra proxy type có hợp lệ không
func (pm *ProxyManager) validateProxyType(proxyType ProxyType) error {
	switch proxyType {
	case ProxyTypeTMProxy, ProxyTypeStatic, ProxyTypeMobileHop:
		return nil
	default:
		return fmt.Errorf("unknown proxy type: %s", proxyType)
	}
}

type Config struct {
	WaitProxyChange     bool
	ChangeProxyWaitTime time.Duration
	ProxyStrings        []string
}

func (pm *ProxyManager) SetConfig(config Config) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.waitProxyChange = config.WaitProxyChange
	pm.changeProxyWaitTime = config.ChangeProxyWaitTime
	_, err := pm.loadProxiesFromList(config.ProxyStrings)
	if err != nil {
		return fmt.Errorf("failed to load proxies: %w", err)
	}
	return nil
}
