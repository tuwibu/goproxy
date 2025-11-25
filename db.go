package goproxy

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// AcquireProxy lấy proxy theo ID và đánh dấu là đang sử dụng (increment count, set used=true)
func (pm *ProxyManager) AcquireProxy(id int64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	_, err := pm.db.Exec(
		`UPDATE proxies SET used = true, count = count + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to acquire proxy: %w", err)
	}

	// Update cache
	if p, ok := pm.proxyCache[id]; ok {
		p.Used = true
		p.Count++
		p.UpdatedAt = timeNow()
	}

	return nil
}

// ReleaseProxy giải phóng proxy (set used=false, don't modify count)
func (pm *ProxyManager) ReleaseProxy(id int64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	_, err := pm.db.Exec(
		`UPDATE proxies SET used = false, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to release proxy: %w", err)
	}

	// Update cache
	if p, ok := pm.proxyCache[id]; ok {
		p.Used = false
		p.UpdatedAt = timeNow()
	}

	return nil
}

// GetProxyByID lấy proxy theo ID
func (pm *ProxyManager) GetProxyByID(id int64) (*Proxy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Check cache trước
	if p, ok := pm.proxyCache[id]; ok {
		return p, nil
	}

	var p Proxy
	var lastChanged, lastIP sql.NullString
	err := pm.db.QueryRow(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, used, count, last_changed, last_ip, created_at, updated_at
		FROM proxies WHERE id = ?
	`, id).Scan(&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Used, &p.Count, &lastChanged, &lastIP, &p.CreatedAt, &p.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("proxy not found")
		}
		return nil, fmt.Errorf("failed to query proxy: %w", err)
	}

	if lastChanged.Valid {
		lastChangedTime, _ := time.Parse(time.RFC3339, lastChanged.String)
		p.LastChanged = lastChangedTime
	}
	if lastIP.Valid {
		p.LastIP = lastIP.String
	}

	return &p, nil
}

// GetAllProxies lấy tất cả proxy
func (pm *ProxyManager) GetAllProxies() ([]*Proxy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, used, count, last_changed, last_ip, created_at, updated_at
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
			&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Used, &p.Count,
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

// LoadProxiesFromList load danh sách proxy từ format string (public)
// Format: type|proxyStr[|apiKey][|changeUrl]
// Ví dụ:
//
//	tmproxy|apiKey123
//	static|192.168.1.1:8080
//	static|192.168.1.1:8080:user:pass
//	mobilehop|192.168.1.1:8080:user:pass|https://example.com/change
func (pm *ProxyManager) LoadProxiesFromList(proxyStrings []string) ([]int64, error) {
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
		minTime := 0

		// Detect if parts[1] is proxyStr or apiKey by checking for ':'
		if strings.Contains(parts[1], ":") {
			// parts[1] là proxyStr (ip:port)
			proxyString = &parts[1]
		} else {
			// parts[1] là apiKey (tmproxy case)
			apiKey = &parts[1]
		}

		// Xử lý parts[2]: có thể là URL (changeUrl) hoặc số (minTime)
		if len(parts) > 2 && parts[2] != "" {
			// Thử parse thành số (minTime)
			if val, err := strconv.Atoi(parts[2]); err == nil {
				// parts[2] là minTime (số)
				minTime = val
				// changeUrl từ parts[3] nếu có
				if len(parts) > 3 && parts[3] != "" {
					changeUrl = parts[3]
				}
			} else {
				// parts[2] là changeUrl (không phải số)
				changeUrl = parts[2]
				// minTime từ parts[3] nếu có
				if len(parts) > 3 && parts[3] != "" {
					if val, err := strconv.Atoi(parts[3]); err == nil {
						minTime = val
					}
				}
			}
		}

		proxyStr := ""
		if proxyString != nil {
			proxyStr = *proxyString
		}
		apiKeyStr := ""
		if apiKey != nil {
			apiKeyStr = *apiKey
		}

		// Generate unique key
		uniqueKey := generateUniqueKey(proxyStr, apiKeyStr)

		// Cố gắng insert, nếu unique_key đã tồn tại thì update minTime và changeUrl
		result, err := pm.db.Exec(
			`INSERT INTO proxies (type, proxy_str, api_key, unique_key, min_time, change_url, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			proxyType, proxyStr, apiKeyStr, uniqueKey, minTime, changeUrl,
		)

		var id int64
		if err != nil {
			// Nếu unique_key đã tồn tại, thì update minTime và changeUrl
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				_, updateErr := pm.db.Exec(
					`UPDATE proxies SET min_time = ?, change_url = ?, updated_at = CURRENT_TIMESTAMP
					 WHERE unique_key = ?`,
					minTime, changeUrl, uniqueKey,
				)
				if updateErr != nil {
					return nil, fmt.Errorf("failed to update proxy: %w", updateErr)
				}

				// Lấy ID của proxy vừa update
				var existingID int64
				queryErr := pm.db.QueryRow(
					`SELECT id FROM proxies WHERE unique_key = ?`,
					uniqueKey,
				).Scan(&existingID)
				if queryErr != nil {
					return nil, fmt.Errorf("failed to get proxy id: %w", queryErr)
				}
				id = existingID
			} else {
				return nil, fmt.Errorf("failed to insert proxy: %w", err)
			}
		} else {
			lastID, err := result.LastInsertId()
			if err != nil {
				return nil, fmt.Errorf("failed to get last insert id: %w", err)
			}
			id = lastID
		}

		proxy := &Proxy{
			ID:        id,
			Type:      proxyType,
			ProxyStr:  proxyStr,
			ApiKey:    apiKeyStr,
			ChangeUrl: changeUrl,
			MinTime:   minTime,
			CreatedAt: timeNow(),
			UpdatedAt: timeNow(),
		}

		pm.proxyCache[id] = proxy
		ids = append(ids, proxy.ID)
	}

	return ids, nil
}
