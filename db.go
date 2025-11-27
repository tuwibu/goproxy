package goproxy

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tuwibu/goproxy/service"

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
		running BOOLEAN DEFAULT false,
		used INTEGER DEFAULT 0,
		last_changed INTEGER,
		last_ip TEXT,
		error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_type ON proxies(type);
	CREATE INDEX IF NOT EXISTS idx_unique_key ON proxies(unique_key);
	`)
	return err
}

func (pm *ProxyManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.db != nil {
		return pm.db.Close()
	}
	return nil
}

func (pm *ProxyManager) ReleaseProxy(id int64) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	pm.db.Exec(`UPDATE proxies SET running=false, updated_at=? WHERE id=?`, now, id)
	if p, ok := pm.proxyCache[id]; ok {
		p.Running, p.UpdatedAt = false, now
	}
	return nil
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

			// Nếu GetCurrentProxy lỗi hoặc resp nil, thử GetNewProxy
			if err != nil || resp == nil {
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp != nil && newResp.Code == 0 {
					resp = newResp
				} else {
					continue
				}
			} else if resp.Code != 0 && resp.Code != 27 {
				// Nếu Code không phải 0 hoặc 27, thử GetNewProxy
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp != nil && newResp.Code == 0 {
					resp = newResp
				} else {
					continue
				}
			} else if resp.Code == 27 || resp.Data.Timeout == 0 {
				// Nếu Code == 27 hoặc Timeout == 0, thử GetNewProxy
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp != nil && newResp.Code == 0 {
					resp = newResp
				} else {
					continue
				}
			}

			// Chỉ lấy proxyStr nếu resp hợp lệ và Code == 0
			if resp != nil && resp.Code == 0 {
				proxyStr = fmt.Sprintf("%s:%s:%s", resp.Data.HTTPS, resp.Data.Username, resp.Data.Password)
			}
		}

		// Tính uniqueKey: MD5 hash của apiKey (tmproxy) hoặc proxyStr (static/mobilehop)
		var uniqueKey string
		if pType == ProxyTypeTMProxy {
			uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(apiKey)))
		} else {
			uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(proxyStr)))
		}

		id, err := pm.upsertProxy(pType, proxyStr, apiKey, changeUrl, minTime, uniqueKey)
		if err != nil {
			return nil, err
		}

		// Luôn luôn set last_changed = now() khi load
		pm.db.Exec(`UPDATE proxies SET last_changed=? WHERE id=?`, time.Now().Unix(), id)

		ids = append(ids, id)
	}

	return ids, nil
}

func (pm *ProxyManager) upsertProxy(pType ProxyType, proxyStr, apiKey, changeUrl string, minTime int, uniqueKey string) (int64, error) {
	now := time.Now()

	result, err := pm.db.Exec(
		`INSERT INTO proxies (type, proxy_str, api_key, unique_key, min_time, change_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pType, proxyStr, apiKey, uniqueKey, minTime, changeUrl, now, now,
	)

	if err == nil {
		id, _ := result.LastInsertId()
		pm.db.Exec(`UPDATE proxies SET last_changed=? WHERE id=?`, now.Unix(), id)
		pm.proxyCache[id] = &Proxy{
			ID:          id,
			Type:        pType,
			ProxyStr:    proxyStr,
			ApiKey:      apiKey,
			ChangeUrl:   changeUrl,
			MinTime:     minTime,
			Running:     false,
			Used:        0,
			LastChanged: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		return id, nil
	}

	if !strings.Contains(err.Error(), "UNIQUE") {
		return 0, err
	}

	pm.db.Exec(`UPDATE proxies SET proxy_str=?, min_time=?, change_url=?, updated_at=? WHERE unique_key=?`,
		proxyStr, minTime, changeUrl, now, uniqueKey)

	var id int64
	pm.db.QueryRow(`SELECT id FROM proxies WHERE unique_key=?`, uniqueKey).Scan(&id)

	// Update cache để đồng bộ với DB
	if cached, ok := pm.proxyCache[id]; ok {
		cached.ProxyStr = proxyStr
		cached.MinTime = minTime
		cached.ChangeUrl = changeUrl
		cached.UpdatedAt = now
	}

	return id, nil
}

func (pm *ProxyManager) GetAvailableProxy() (id int64, proxyStr string, err error) {
	pm.mu.RLock()
	now := time.Now()
	nowUnix := now.Unix()

	// Điều kiện: running=false và (used < max_used hoặc last_changed + min_time < now)
	// Nếu min_time = 0, nghĩa là luôn đủ điều kiện change IP
	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, running, used, last_ip, last_changed, error, created_at, updated_at
		FROM proxies
		WHERE running=false
		AND (error IS NULL OR error='')
		AND (
			used < ?
			OR
			(min_time = 0 OR (last_changed IS NULL OR (? - last_changed >= min_time)))
		)
		ORDER BY RANDOM()
		LIMIT 1
	`, pm.maxUsed, nowUnix)

	if err != nil {
		pm.mu.RUnlock()
		return 0, "", err
	}

	if !rows.Next() {
		rows.Close()
		pm.mu.RUnlock()
		return 0, "", fmt.Errorf("no available proxy")
	}

	var p Proxy
	var lastIP sql.NullString
	var lastChangedUnix sql.NullInt64
	var errStr sql.NullString
	var apiKey sql.NullString
	var changeUrl sql.NullString
	err = rows.Scan(&p.ID, &p.Type, &p.ProxyStr, &apiKey, &changeUrl, &p.MinTime, &p.Running, &p.Used, &lastIP, &lastChangedUnix, &errStr, &p.CreatedAt, &p.UpdatedAt)
	rows.Close()
	pm.mu.RUnlock()

	if err != nil {
		return 0, "", err
	}

	if apiKey.Valid {
		p.ApiKey = apiKey.String
	}
	if changeUrl.Valid {
		p.ChangeUrl = changeUrl.String
	}
	if lastIP.Valid {
		p.LastIP = lastIP.String
	}
	if lastChangedUnix.Valid {
		p.LastChanged = time.Unix(lastChangedUnix.Int64, 0)
	}
	if errStr.Valid {
		p.Error = errStr.String
	}

	// Acquire proxy: set running=true, used+=1 (theo requirement #13: sau khi lấy được proxy đủ điều kiện)
	pm.mu.Lock()
	if _, err := pm.db.Exec(`UPDATE proxies SET running=true, used=used+1, updated_at=? WHERE id=?`, now, p.ID); err != nil {
		pm.mu.Unlock()
		return 0, "", fmt.Errorf("failed to acquire proxy: %v", err)
	}
	if cached, ok := pm.proxyCache[p.ID]; ok {
		cached.Running = true
		cached.Used = cached.Used + 1
		cached.UpdatedAt = now
	}
	pm.mu.Unlock()

	// Sau khi acquire, kiểm tra last_changed và min_time để quyết định có change IP không
	timeSinceLastChange := now.Sub(p.LastChanged).Seconds()
	canChangeIP := p.MinTime == 0 || timeSinceLastChange >= float64(p.MinTime)

	// TMProxy: change IP nếu đủ điều kiện
	if p.Type == ProxyTypeTMProxy && canChangeIP && p.ApiKey != "" {
		// TMProxy: gọi GetNewProxy
		resp, err := service.GetTMProxy().GetNewProxy(p.ApiKey, 0, 0)
		if err != nil {
			// GetNewProxy thất bại - đánh dấu error, release proxy và trả về
			errMsg := fmt.Sprintf("GetNewProxy failed: %v", err)
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET running=false, error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.Running = false
				cached.Error = errMsg
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
			return 0, "", fmt.Errorf("%s", errMsg)
		}

		if resp.Code != 0 {
			// API trả về error code - đánh dấu error, release proxy và trả về
			errMsg := fmt.Sprintf("tmproxy api returned code: %d, message: %s", resp.Code, resp.Message)
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET running=false, error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.Running = false
				cached.Error = errMsg
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
			return 0, "", fmt.Errorf("%s", errMsg)
		}

		// GetNewProxy thành công - update proxy mới, giữ running=true và used (đã được tăng từ acquire), clear error
		newProxyStr := fmt.Sprintf("%s:%s:%s", resp.Data.HTTPS, resp.Data.Username, resp.Data.Password)

		pm.mu.Lock()
		pm.db.Exec(`UPDATE proxies SET proxy_str=?, last_changed=?, error='', updated_at=? WHERE id=?`, newProxyStr, now.Unix(), now, p.ID)
		if cached, ok := pm.proxyCache[p.ID]; ok {
			cached.ProxyStr = newProxyStr
			cached.LastChanged = now
			cached.Error = ""
			cached.UpdatedAt = now
		}
		pm.mu.Unlock()

		p.ProxyStr = newProxyStr
		p.LastChanged = now
		p.Error = ""
		p.UpdatedAt = now

		// Đợi ChangeProxyWaitTime trước khi trả result
		if pm.changeProxyWaitTime > 0 {
			time.Sleep(pm.changeProxyWaitTime)
		}

		return p.ID, p.ProxyStr, nil
	}

	// MobileHop: change IP nếu đủ điều kiện (giống logic TMProxy)
	if p.Type == ProxyTypeMobileHop && canChangeIP && p.ChangeUrl != "" {
		// Gọi callChangeURL
		if err := pm.callChangeURL(context.Background(), p.ChangeUrl); err != nil {
			// callChangeURL thất bại - đánh dấu error, release proxy và trả về
			errMsg := fmt.Sprintf("callChangeURL failed: %v", err)
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET running=false, error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.Running = false
				cached.Error = errMsg
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
			return 0, "", fmt.Errorf("%s", errMsg)
		}

		// callChangeURL thành công - update last_changed, giữ running=true và used (đã được tăng từ acquire), clear error
		pm.mu.Lock()
		pm.db.Exec(`UPDATE proxies SET last_changed=?, error='', updated_at=? WHERE id=?`, now.Unix(), now, p.ID)
		if cached, ok := pm.proxyCache[p.ID]; ok {
			cached.LastChanged = now
			cached.Error = ""
			cached.UpdatedAt = now
		}
		pm.mu.Unlock()

		p.LastChanged = now
		p.Error = ""
		p.UpdatedAt = now

		// Đợi ChangeProxyWaitTime trước khi trả result
		if pm.changeProxyWaitTime > 0 {
			time.Sleep(pm.changeProxyWaitTime)
		}

		return p.ID, p.ProxyStr, nil
	}

	// Static proxy hoặc không đủ điều kiện change IP: trả về proxy hiện tại
	return p.ID, p.ProxyStr, nil
}
