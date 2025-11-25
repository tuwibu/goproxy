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
		last_changed DATETIME,
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
	pm.db.Exec(`UPDATE proxies SET running=true, used=used+1, updated_at=? WHERE id=?`, now, id)
	if p, ok := pm.proxyCache[id]; ok {
		p.Running, p.Used, p.UpdatedAt = true, p.Used+1, now
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

func (pm *ProxyManager) GetProxyByID(id int64) (*Proxy, error) {
	pm.mu.RLock()
	var p Proxy
	var lastIP sql.NullString
	var lastChanged sql.NullTime
	var errStr sql.NullString
	err := pm.db.QueryRow(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, running, used, last_ip, last_changed, error, created_at, updated_at
		FROM proxies WHERE id = ?
	`, id).Scan(&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Running, &p.Used, &lastIP, &lastChanged, &errStr, &p.CreatedAt, &p.UpdatedAt)
	pm.mu.RUnlock()

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}

	if lastIP.Valid {
		p.LastIP = lastIP.String
	}
	if lastChanged.Valid {
		p.LastChanged = lastChanged.Time
	}
	if errStr.Valid {
		p.Error = errStr.String
	}

	// Update cache với latest data từ database
	pm.mu.Lock()
	pm.proxyCache[id] = &p
	pm.mu.Unlock()

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
		getNewProxyCalled := false
		if pType == ProxyTypeTMProxy && apiKey != "" {
			resp, err := service.GetTMProxy().GetCurrentProxy(apiKey)

			// Nếu GetCurrentProxy lỗi hoặc resp nil, thử GetNewProxy
			if err != nil || resp == nil {
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp != nil && newResp.Code == 0 {
					resp = newResp
					getNewProxyCalled = true
				} else {
					continue
				}
			} else if resp.Code != 0 && resp.Code != 27 {
				// Nếu Code không phải 0 hoặc 27, thử GetNewProxy
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp != nil && newResp.Code == 0 {
					resp = newResp
					getNewProxyCalled = true
				} else {
					continue
				}
			} else if resp.Code == 27 || resp.Data.Timeout == 0 {
				// Nếu Code == 27 hoặc Timeout == 0, thử GetNewProxy
				if newResp, err := service.GetTMProxy().GetNewProxy(apiKey, 0, 0); err == nil && newResp != nil && newResp.Code == 0 {
					resp = newResp
					getNewProxyCalled = true
				} else {
					continue
				}
			}

			// Chỉ lấy proxyStr nếu resp hợp lệ và Code == 0
			if resp != nil && resp.Code == 0 {
				proxyStr = fmt.Sprintf("%s:%s:%s", resp.Data.HTTPS, resp.Data.Username, resp.Data.Password)
			}
		}

		// Tính uniqueKey: tmproxy dùng apiKey, các loại khác dùng proxyStr
		var uniqueKey string
		if pType == ProxyTypeTMProxy {
			uniqueKey = apiKey
		} else {
			uniqueKey = proxyStr
		}

		id, err := pm.upsertProxy(pType, proxyStr, apiKey, changeUrl, minTime, uniqueKey)
		if err != nil {
			return nil, err
		}

		if getNewProxyCalled {
			pm.db.Exec(`UPDATE proxies SET last_changed=? WHERE id=?`, time.Now(), id)
		}

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
		pm.db.Exec(`UPDATE proxies SET last_changed=? WHERE id=?`, now, id)
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
	return id, nil
}

func (pm *ProxyManager) GetAllProxies() ([]*Proxy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, running, used, last_ip, last_changed, error, created_at, updated_at
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
		var lastChanged sql.NullTime
		var errStr sql.NullString
		rows.Scan(&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Running, &p.Used, &lastIP, &lastChanged, &errStr, &p.CreatedAt, &p.UpdatedAt)
		if lastIP.Valid {
			p.LastIP = lastIP.String
		}
		if lastChanged.Valid {
			p.LastChanged = lastChanged.Time
		}
		if errStr.Valid {
			p.Error = errStr.String
		}
		proxies = append(proxies, &p)
	}

	return proxies, rows.Err()
}

func (pm *ProxyManager) GetAvailableProxy() (id int64, proxyStr string, err error) {
	pm.mu.RLock()
	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, running, used, last_ip, last_changed, error, created_at, updated_at
		FROM proxies WHERE running=false AND used<? AND (error IS NULL OR error='')
		ORDER BY RANDOM()
		LIMIT 1
	`, pm.maxUsed)

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
	var lastChanged sql.NullTime
	var errStr sql.NullString
	err = rows.Scan(&p.ID, &p.Type, &p.ProxyStr, &p.ApiKey, &p.ChangeUrl, &p.MinTime, &p.Running, &p.Used, &lastIP, &lastChanged, &errStr, &p.CreatedAt, &p.UpdatedAt)
	rows.Close()
	pm.mu.RUnlock()

	if err != nil {
		return 0, "", err
	}

	if lastIP.Valid {
		p.LastIP = lastIP.String
	}
	if lastChanged.Valid {
		p.LastChanged = lastChanged.Time
	}
	if errStr.Valid {
		p.Error = errStr.String
	}

	// Static proxy: không cần change, tiếp tục đến phần acquire

	now := time.Now()

	// TMProxy: kiểm tra minTime và tự động change proxy nếu đủ thời gian
	if p.Type == ProxyTypeTMProxy && p.MinTime > 0 {
		timeSinceLastChange := now.Sub(p.LastChanged).Seconds()

		// Nếu đủ minTime, gọi change proxy
		if timeSinceLastChange >= float64(p.MinTime) {
			if p.ApiKey != "" {
				// TMProxy: gọi GetNewProxy
				resp, err := service.GetTMProxy().GetNewProxy(p.ApiKey, 0, 0)
				if err != nil {
					// GetNewProxy thất bại - đánh dấu error và trả về
					errMsg := fmt.Sprintf("GetNewProxy failed: %v", err)
					pm.mu.Lock()
					pm.db.Exec(`UPDATE proxies SET error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
					if cached, ok := pm.proxyCache[p.ID]; ok {
						cached.Error = errMsg
						cached.UpdatedAt = now
					}
					pm.mu.Unlock()
					return 0, "", fmt.Errorf("%s", errMsg)
				}

				if resp.Code != 0 {
					// API trả về error code - đánh dấu error và trả về
					errMsg := fmt.Sprintf("tmproxy api returned code: %d, message: %s", resp.Code, resp.Message)
					pm.mu.Lock()
					pm.db.Exec(`UPDATE proxies SET error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
					if cached, ok := pm.proxyCache[p.ID]; ok {
						cached.Error = errMsg
						cached.UpdatedAt = now
					}
					pm.mu.Unlock()
					return 0, "", fmt.Errorf("%s", errMsg)
				}

				// GetNewProxy thành công - update proxy mới, reset used và clear error
				newProxyStr := fmt.Sprintf("%s:%s:%s", resp.Data.HTTPS, resp.Data.Username, resp.Data.Password)

				pm.mu.Lock()
				pm.db.Exec(`UPDATE proxies SET proxy_str=?, last_changed=?, used=0, error='', updated_at=? WHERE id=?`, newProxyStr, now, now, p.ID)
				if cached, ok := pm.proxyCache[p.ID]; ok {
					cached.ProxyStr = newProxyStr
					cached.LastChanged = now
					cached.Used = 0
					cached.Error = ""
					cached.UpdatedAt = now
				}
				pm.mu.Unlock()

				p.ProxyStr = newProxyStr
				p.LastChanged = now
				p.Used = 0
				p.Error = ""
				p.UpdatedAt = now

				// Đợi ChangeProxyWaitTime trước khi trả result
				if pm.changeProxyWaitTime > 0 {
					time.Sleep(pm.changeProxyWaitTime)
				}
			}
		}
	}

	// MobileHop: kiểm tra used gần maxUsed và tự động change proxy nếu gần cuối
	if p.Type == ProxyTypeMobileHop && p.ChangeUrl != "" {
		// Nếu used gần bằng maxUsed (còn 1 lần nữa là hết), thực hiện change proxy
		if p.Used >= pm.maxUsed-1 {
			// Gọi callChangeURL
			if err := pm.callChangeURL(context.Background(), p.ChangeUrl); err != nil {
				// callChangeURL thất bại - đánh dấu error và trả về
				errMsg := fmt.Sprintf("callChangeURL failed: %v", err)
				pm.mu.Lock()
				pm.db.Exec(`UPDATE proxies SET error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
				if cached, ok := pm.proxyCache[p.ID]; ok {
					cached.Error = errMsg
					cached.UpdatedAt = now
				}
				pm.mu.Unlock()
				return 0, "", fmt.Errorf("%s", errMsg)
			}

			// callChangeURL thành công - update last_changed, reset used và clear error
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET last_changed=?, used=0, error='', updated_at=? WHERE id=?`, now, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.LastChanged = now
				cached.Used = 0
				cached.Error = ""
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()

			p.LastChanged = now
			p.Used = 0
			p.Error = ""
			p.UpdatedAt = now

			// Đợi ChangeProxyWaitTime trước khi trả result
			if pm.changeProxyWaitTime > 0 {
				time.Sleep(pm.changeProxyWaitTime)
			}
		}
	}

	// Tự động gọi AcquireProxy để set running=true, used+=1
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

	return p.ID, p.ProxyStr, nil
}
