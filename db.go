package goproxy

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// initDB mở kết nối database
func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// initSchema tạo table nếu chưa tồn tại
func (pm *ProxyManager) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS proxies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		proxy_str TEXT,
		api_key TEXT,
		last_changed DATETIME,
		last_ip TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_type ON proxies(type);
	CREATE INDEX IF NOT EXISTS idx_created ON proxies(created_at);
	`

	_, err := pm.db.Exec(schema)
	return err
}

// AddProxyInput input struct để thêm proxy
type AddProxyInput struct {
	Type      ProxyType
	ProxyStr  *string // nullable
	ApiKey    *string // nullable
	ChangeUrl string
}

// addProxy thêm proxy mới vào danh sách (private)
func (pm *ProxyManager) addProxy(input AddProxyInput) (*Proxy, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Validate type
	if err := pm.validateProxyType(input.Type); err != nil {
		return nil, err
	}

	// Handle nullable fields
	proxyStr := ""
	if input.ProxyStr != nil {
		proxyStr = *input.ProxyStr
	}
	apiKey := ""
	if input.ApiKey != nil {
		apiKey = *input.ApiKey
	}

	result, err := pm.db.Exec(
		`INSERT INTO proxies (type, proxy_str, api_key, created_at, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		input.Type, proxyStr, apiKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert proxy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	proxy := &Proxy{
		ID:        id,
		Type:      input.Type,
		ProxyStr:  proxyStr,
		ApiKey:    apiKey,
		ChangeUrl: input.ChangeUrl,
		CreatedAt: timeNow(),
		UpdatedAt: timeNow(),
	}

	pm.proxyCache[id] = proxy
	return proxy, nil
}

// Close đóng kết nối database
func (pm *ProxyManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.db != nil {
		return pm.db.Close()
	}
	return nil
}
