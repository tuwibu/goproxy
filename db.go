package goproxy

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"goproxy/service"

	_ "modernc.org/sqlite"
)

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func (pm *ProxyManager) initSchema() error {
	_, err := pm.db.Exec(`
	CREATE TABLE IF NOT EXISTS proxies (
		id INTEGER PRIMARY KEY,
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
	CREATE INDEX IF NOT EXISTS idx_unique_key ON proxies(unique_key);
	`)
	return err
}

func generateUniqueKey(proxyStr, apiKey string) string {
	data := apiKey
	if apiKey == "" {
		data = proxyStr
	} else if proxyStr != "" {
		data = proxyStr + "-" + apiKey
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(data)))
}

func (pm *ProxyManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.db != nil {
		return pm.db.Close()
	}
	return nil
}

func (pm *ProxyManager) AcquireProxy(id int64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	pm.db.Exec(`UPDATE proxies SET used=true, count=count+1, updated_at=? WHERE id=?`, now, id)
	if p, ok := pm.proxyCache[id]; ok {
		p.Used, p.Count, p.UpdatedAt = true, p.Count+1, now
	}
	return nil
}

func (pm *ProxyManager) ReleaseProxy(id int64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	pm.db.Exec(`UPDATE proxies SET used=false, updated_at=? WHERE id=?`, now, id)
	if p, ok := pm.proxyCache[id]; ok {
		p.Used, p.UpdatedAt = false, now
	}
	return nil
}

func (pm *ProxyManager) GetProxyByID(id int64) (*Proxy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if p, ok := pm.proxyCache[id]; ok {
		return p, nil
	}

	var p Proxy
	var lastIP sql.NullString
	err := pm.db.QueryRow(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, used, count, last_ip, created_at, updated_at
		FROM proxies WHERE id = ?
	`, id).Scan(&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Used, &p.Count, &lastIP, &p.CreatedAt, &p.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}

	if lastIP.Valid {
		p.LastIP = lastIP.String
	}
	return &p, nil
}

func (pm *ProxyManager) LoadProxiesFromList(proxyStrings []string) ([]int64, error) {
	var ids []int64

	for _, s := range proxyStrings {
		parts := strings.Split(strings.TrimSpace(s), "|")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid format: %s", s)
		}

		pType := ProxyType(parts[0])
		if err := pm.validateProxyType(pType); err != nil {
			return nil, err
		}

		var proxyStr, apiKey string
		changeUrl := ""
		minTime := 0

		if strings.Contains(parts[1], ":") {
			proxyStr = parts[1]
		} else {
			apiKey = parts[1]
		}

		if len(parts) > 2 && parts[2] != "" {
			if val, _ := strconv.Atoi(parts[2]); val > 0 {
				minTime = val
				if len(parts) > 3 {
					changeUrl = parts[3]
				}
			} else {
				changeUrl = parts[2]
				if len(parts) > 3 {
					strconv.Atoi(parts[3])
					if val, _ := strconv.Atoi(parts[3]); val > 0 {
						minTime = val
					}
				}
			}
		}

		// TMProxy: lấy proxy từ API
		if pType == ProxyTypeTMProxy && apiKey != "" {
			resp, err := service.GetTMProxy().GetCurrentProxy(apiKey)
			if err != nil || (resp.Code != 0 && resp.Code != 27) {
				continue
			}

			if resp.Code == 27 || resp.Data.Timeout == 0 {
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp.Code == 0 {
					resp = newResp
				} else {
					continue
				}
			}

			if resp.Code == 0 {
				proxyStr = fmt.Sprintf("%s:%s:%s", resp.Data.HTTPS, resp.Data.Username, resp.Data.Password)
			}
		}

		id, err := pm.upsertProxy(pType, proxyStr, apiKey, changeUrl, minTime)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, nil
}

func (pm *ProxyManager) upsertProxy(pType ProxyType, proxyStr, apiKey, changeUrl string, minTime int) (int64, error) {
	key := generateUniqueKey(proxyStr, apiKey)
	now := time.Now()

	result, err := pm.db.Exec(
		`INSERT INTO proxies (type, proxy_str, api_key, unique_key, min_time, change_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pType, proxyStr, apiKey, key, minTime, changeUrl, now, now,
	)

	if err == nil {
		id, _ := result.LastInsertId()
		pm.proxyCache[id] = &Proxy{
			ID:        id,
			Type:      pType,
			ProxyStr:  proxyStr,
			ApiKey:    apiKey,
			ChangeUrl: changeUrl,
			MinTime:   minTime,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return id, nil
	}

	if !strings.Contains(err.Error(), "UNIQUE") {
		return 0, err
	}

	pm.db.Exec(`UPDATE proxies SET proxy_str=?, min_time=?, change_url=?, updated_at=? WHERE unique_key=?`,
		proxyStr, minTime, changeUrl, now, key)

	var id int64
	pm.db.QueryRow(`SELECT id FROM proxies WHERE unique_key=?`, key).Scan(&id)
	return id, nil
}

func (pm *ProxyManager) GetAllProxies() ([]*Proxy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, used, count, last_ip, created_at, updated_at
		FROM proxies ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []*Proxy
	for rows.Next() {
		var p Proxy
		var lastIP sql.NullString
		rows.Scan(&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Used, &p.Count, &lastIP, &p.CreatedAt, &p.UpdatedAt)
		if lastIP.Valid {
			p.LastIP = lastIP.String
		}
		proxies = append(proxies, &p)
	}

	return proxies, rows.Err()
}
