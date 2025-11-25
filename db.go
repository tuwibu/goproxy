package goproxy

import (
	"crypto/md5"
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
		unique_key TEXT UNIQUE,
		min_time INTEGER,
		change_url TEXT,
		used BOOLEAN DEFAULT false,
		count INTEGER DEFAULT 0,
		last_changed DATETIME,
		last_ip TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_type ON proxies(type);
	CREATE INDEX IF NOT EXISTS idx_created ON proxies(created_at);
	CREATE INDEX IF NOT EXISTS idx_unique_key ON proxies(unique_key);
	CREATE INDEX IF NOT EXISTS idx_used ON proxies(used);
	`

	_, err := pm.db.Exec(schema)
	return err
}

// generateUniqueKey tạo MD5 hash từ apiKey hoặc proxyStr
func generateUniqueKey(proxyStr, apiKey string) string {
	var data string

	if apiKey != "" && proxyStr != "" {
		// Cả 2: nối proxyStr-apiKey
		data = proxyStr + "-" + apiKey
	} else if apiKey != "" {
		// Chỉ apiKey
		data = apiKey
	} else {
		// Chỉ proxyStr
		data = proxyStr
	}

	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)
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
