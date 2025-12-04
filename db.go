package goproxy

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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
		unique BOOLEAN DEFAULT false,
		last_changed INTEGER,
		last_ip TEXT,
		error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_type ON proxies(type);
	CREATE INDEX IF NOT EXISTS idx_unique_key ON proxies(unique_key);
	`)
	if err != nil {
		return err
	}

	// Migration: Thêm cột unique nếu chưa tồn tại
	pm.db.Exec(`ALTER TABLE proxies ADD COLUMN unique BOOLEAN DEFAULT false`)

	// Migration: Cập nhật unique=true cho các proxy type cũ
	pm.db.Exec(`UPDATE proxies SET unique=true WHERE type IN ('tmproxy', 'mobilehop', 'static', 'kiotproxy')`)

	return nil
}

func (pm *ProxyManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.db != nil {
		return pm.db.Close()
	}
	return nil
}

// generateRandomString tạo chuỗi ngẫu nhiên với độ dài cho trước
func generateRandomString(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback nếu crypto/rand lỗi
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[:length]
	}
	return hex.EncodeToString(bytes)[:length]
}

// processStickyProxyStr xử lý proxy string cho sticky type, thay thế ${random} bằng chuỗi ngẫu nhiên
func processStickyProxyStr(proxyStr string) string {
	// Format: ip:port:username:password
	// Username có thể chứa ${random} cần thay thế
	parts := strings.Split(proxyStr, ":")
	if len(parts) >= 3 {
		username := parts[2]
		// Thay thế ${random} bằng chuỗi ngẫu nhiên 8 ký tự
		if strings.Contains(username, "${random}") {
			randomStr := generateRandomString(8)
			username = strings.ReplaceAll(username, "${random}", randomStr)
			parts[2] = username
			return strings.Join(parts, ":")
		}
	}
	return proxyStr
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
		unique := false

		// Xác định unique theo loại proxy
		// tmproxy, mobilehop, static, kiotproxy: unique = true
		// sticky: có thể truyền true/false, default = false
		if pType == ProxyTypeTMProxy || pType == ProxyTypeMobileHop || pType == ProxyTypeStatic || pType == ProxyTypeKiotProxy {
			unique = true
		}

		if strings.Contains(parts[1], ":") {
			proxyStr = parts[1]
		} else {
			apiKey = parts[1]
		}

		if len(parts) > 2 && parts[2] != "" {
			// Với sticky: parts[2] có thể là unique flag (true/false)
			if pType == ProxyTypeSticky && (parts[2] == "true" || parts[2] == "false") {
				unique = parts[2] == "true"
				// Check parts[3] và parts[4] cho minTime/changeUrl
				if len(parts) > 3 && parts[3] != "" {
					if val, _ := strconv.Atoi(parts[3]); val > 0 {
						minTime = val
						if len(parts) > 4 {
							changeUrl = parts[4]
						}
					} else {
						changeUrl = parts[3]
						if len(parts) > 4 {
							if val, _ := strconv.Atoi(parts[4]); val > 0 {
								minTime = val
							}
						}
					}
				}
			} else {
				// Không phải sticky unique flag: xử lý minTime/changeUrl như cũ
				if val, _ := strconv.Atoi(parts[2]); val > 0 {
					minTime = val
					if len(parts) > 3 {
						changeUrl = parts[3]
					}
				} else {
					changeUrl = parts[2]
					if len(parts) > 3 {
						if val, _ := strconv.Atoi(parts[3]); val > 0 {
							minTime = val
						}
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

		// KiotProxy: lấy proxy từ API
		if pType == ProxyTypeKiotProxy && apiKey != "" {
			// Parse region từ parts nếu có (có thể ở vị trí 3 hoặc 4)
			// Lưu region vào changeUrl để dùng sau này
			if len(parts) > 3 && parts[3] != "" && !strings.Contains(parts[3], "://") {
				// Kiểm tra xem có phải là số không (minTime)
				if val, _ := strconv.Atoi(parts[3]); val == 0 {
					changeUrl = parts[3] // region
				} else if len(parts) > 4 {
					changeUrl = parts[4] // region
				}
			}

			region := changeUrl
			resp, err := service.GetKiotProxy().GetCurrentProxy(apiKey)

			// Nếu GetCurrentProxy lỗi hoặc resp nil hoặc không success, thử GetNewProxy
			if err != nil || resp == nil || !resp.Success {
				if newResp, err := service.GetKiotProxy().GetNewProxy(apiKey, region); err == nil && newResp != nil && newResp.Success {
					resp = newResp
				} else {
					continue
				}
			}

			// Chỉ lấy proxyStr nếu resp hợp lệ và Success == true
			if resp != nil && resp.Success {
				// Format: host:port:: (không có username/password)
				proxyStr = fmt.Sprintf("%s::", resp.Data.HTTP)
			}
		}

		// Sticky: lưu proxyStr gốc (có ${random}), sẽ xử lý khi GetAvailableProxy

		// Tính uniqueKey: MD5 hash của apiKey (tmproxy/kiotproxy) hoặc proxyStr (static/mobilehop/sticky)
		var uniqueKey string
		if pType == ProxyTypeTMProxy || pType == ProxyTypeKiotProxy {
			uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(apiKey)))
		} else {
			// Với sticky, dùng proxyStr gốc (chưa thay ${random}) để tính uniqueKey
			if pType == ProxyTypeSticky {
				// Lấy proxyStr gốc từ parts[1]
				originalProxyStr := parts[1]
				uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(originalProxyStr)))
			} else {
				uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(proxyStr)))
			}
		}

		id, err := pm.upsertProxy(pType, proxyStr, apiKey, changeUrl, minTime, uniqueKey, unique)
		if err != nil {
			return nil, err
		}

		// Luôn luôn set last_changed = now() khi load
		pm.db.Exec(`UPDATE proxies SET last_changed=? WHERE id=?`, time.Now().Unix(), id)

		ids = append(ids, id)
	}

	return ids, nil
}

func (pm *ProxyManager) upsertProxy(pType ProxyType, proxyStr, apiKey, changeUrl string, minTime int, uniqueKey string, unique bool) (int64, error) {
	now := time.Now()

	result, err := pm.db.Exec(
		`INSERT INTO proxies (type, proxy_str, api_key, unique_key, min_time, change_url, unique, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pType, proxyStr, apiKey, uniqueKey, minTime, changeUrl, unique, now, now,
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
			Unique:      unique,
			LastChanged: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		return id, nil
	}

	if !strings.Contains(err.Error(), "UNIQUE") {
		return 0, err
	}

	pm.db.Exec(`UPDATE proxies SET proxy_str=?, min_time=?, change_url=?, unique=?, updated_at=? WHERE unique_key=?`,
		proxyStr, minTime, changeUrl, unique, now, uniqueKey)

	var id int64
	pm.db.QueryRow(`SELECT id FROM proxies WHERE unique_key=?`, uniqueKey).Scan(&id)

	// Update cache để đồng bộ với DB
	if cached, ok := pm.proxyCache[id]; ok {
		cached.ProxyStr = proxyStr
		cached.MinTime = minTime
		cached.ChangeUrl = changeUrl
		cached.Unique = unique
		cached.UpdatedAt = now
	}

	return id, nil
}

func (pm *ProxyManager) GetAvailableProxy() (id int64, proxyStr string, err error) {
	pm.mu.RLock()
	now := time.Now()
	nowUnix := now.Unix()

	// Điều kiện:
	// - Với unique=false (sticky có thể không unique): không kiểm tra running/used, chỉ cần error rỗng
	// - Với unique=true: running=false và (used < max_used hoặc last_changed + min_time < now)
	rows, err := pm.db.Query(`
		SELECT id, type, proxy_str, api_key, change_url, min_time, running, used, unique, last_ip, last_changed, error, created_at, updated_at
		FROM proxies
		WHERE (error IS NULL OR error='')
		AND (
			-- unique=false: chỉ cần không có error (sticky không unique)
			(unique = false)
			OR
			-- unique=true: phải running=false và đủ điều kiện
			(
				unique = true
				AND running=false
				AND (
					used < ?
					OR
					(min_time = 0 OR (last_changed IS NULL OR (? - last_changed >= min_time)))
				)
			)
		)
		ORDER BY 
			CASE WHEN unique = false THEN 0 ELSE 1 END,
			used ASC,
			id ASC
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
	err = rows.Scan(&p.ID, &p.Type, &p.ProxyStr, &apiKey, &changeUrl, &p.MinTime, &p.Running, &p.Used, &p.Unique, &lastIP, &lastChangedUnix, &errStr, &p.CreatedAt, &p.UpdatedAt)
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

	// Proxy không unique: không cần set running/used, chỉ cần xử lý proxyStr và trả về
	if !p.Unique {
		// Sticky: xử lý proxyStr để thay thế ${random}
		if p.Type == ProxyTypeSticky {
			processedProxyStr := processStickyProxyStr(p.ProxyStr)
			return p.ID, processedProxyStr, nil
		}
		// Các loại khác: trả về proxyStr thường
		return p.ID, p.ProxyStr, nil
	}

	// Acquire proxy: set running=true trước (chưa tăng used)
	pm.mu.Lock()
	if _, err := pm.db.Exec(`UPDATE proxies SET running=true, updated_at=? WHERE id=?`, now, p.ID); err != nil {
		pm.mu.Unlock()
		return 0, "", fmt.Errorf("failed to acquire proxy: %v", err)
	}
	if cached, ok := pm.proxyCache[p.ID]; ok {
		cached.Running = true
		cached.UpdatedAt = now
	}
	pm.mu.Unlock()

	// Kiểm tra điều kiện restart: last_changed + min_time <= time hiện tại
	timeSinceLastChange := now.Sub(p.LastChanged).Seconds()
	canChangeIP := p.MinTime == 0 || timeSinceLastChange >= float64(p.MinTime)

	// Sticky với unique=true: thay ${random} = restart (change IP)
	if p.Type == ProxyTypeSticky && p.Unique {
		if canChangeIP {
			// Đủ điều kiện restart: reset used=1, update last_changed
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET last_changed=?, used=1, error='', updated_at=? WHERE id=?`, now.Unix(), now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.LastChanged = now
				cached.Used = 1
				cached.Error = ""
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
		} else {
			// Không đủ điều kiện restart: tăng used++
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET used=used+1, updated_at=? WHERE id=?`, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.Used = cached.Used + 1
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
		}

		// Xử lý proxyStr để thay thế ${random}
		processedProxyStr := processStickyProxyStr(p.ProxyStr)
		return p.ID, processedProxyStr, nil
	}

	// TMProxy: restart nếu đủ điều kiện
	if p.Type == ProxyTypeTMProxy && canChangeIP && p.ApiKey != "" {
		// TMProxy: gọi GetNewProxy
		resp, err := service.GetTMProxy().GetNewProxy(p.ApiKey, 0, 0)
		if err != nil {
			// GetNewProxy thất bại - đánh dấu error, giữ running=true
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
			// API trả về error code - đánh dấu error, giữ running=true
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

		// GetNewProxy thành công - update proxy mới, reset used=1, giữ running=true, clear error
		newProxyStr := fmt.Sprintf("%s:%s:%s", resp.Data.HTTPS, resp.Data.Username, resp.Data.Password)

		pm.mu.Lock()
		pm.db.Exec(`UPDATE proxies SET proxy_str=?, last_changed=?, used=1, error='', updated_at=? WHERE id=?`, newProxyStr, now.Unix(), now, p.ID)
		if cached, ok := pm.proxyCache[p.ID]; ok {
			cached.ProxyStr = newProxyStr
			cached.LastChanged = now
			cached.Used = 1
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

	// MobileHop: restart nếu đủ điều kiện
	if p.Type == ProxyTypeMobileHop && canChangeIP && p.ChangeUrl != "" {
		// Gọi callChangeURL
		if err := pm.callChangeURL(context.Background(), p.ChangeUrl); err != nil {
			// callChangeURL thất bại - đánh dấu error, giữ running=true
			errMsg := fmt.Sprintf("callChangeURL failed: %v", err)
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET running=false, updated_at=? WHERE id=?`, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.Running = false
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
			return 0, "", fmt.Errorf("%s", errMsg)
		}

		// callChangeURL thành công - update last_changed, reset used=1, giữ running=true, clear error
		pm.mu.Lock()
		pm.db.Exec(`UPDATE proxies SET last_changed=?, used=1, error='', updated_at=? WHERE id=?`, now.Unix(), now, p.ID)
		if cached, ok := pm.proxyCache[p.ID]; ok {
			cached.LastChanged = now
			cached.Used = 1
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

	// KiotProxy: restart nếu đủ điều kiện
	if p.Type == ProxyTypeKiotProxy && canChangeIP && p.ApiKey != "" {
		// Parse region từ changeUrl nếu có
		region := ""
		if p.ChangeUrl != "" {
			region = p.ChangeUrl
		}

		// KiotProxy: gọi GetNewProxy
		resp, err := service.GetKiotProxy().GetNewProxy(p.ApiKey, region)
		if err != nil {
			// GetNewProxy thất bại - đánh dấu error, giữ running=true
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

		if !resp.Success {
			// API trả về error - đánh dấu error, giữ running=true
			errMsg := fmt.Sprintf("kiotproxy api returned code: %d, message: %s, error: %s", resp.Code, resp.Message, resp.Error)
			pm.mu.Lock()
			pm.db.Exec(`UPDATE proxies SET error=?, updated_at=? WHERE id=?`, errMsg, now, p.ID)
			if cached, ok := pm.proxyCache[p.ID]; ok {
				cached.Error = errMsg
				cached.UpdatedAt = now
			}
			pm.mu.Unlock()
			return 0, "", fmt.Errorf("%s", errMsg)
		}

		// GetNewProxy thành công - update proxy mới, reset used=1, giữ running=true, clear error
		newProxyStr := fmt.Sprintf("%s::", resp.Data.HTTP)

		pm.mu.Lock()
		pm.db.Exec(`UPDATE proxies SET proxy_str=?, last_changed=?, used=1, error='', updated_at=? WHERE id=?`, newProxyStr, now.Unix(), now, p.ID)
		if cached, ok := pm.proxyCache[p.ID]; ok {
			cached.ProxyStr = newProxyStr
			cached.LastChanged = now
			cached.Used = 1
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

	// Không đủ điều kiện restart: update used++ và trả về proxy hiện tại
	pm.mu.Lock()
	pm.db.Exec(`UPDATE proxies SET used=used+1, updated_at=? WHERE id=?`, now, p.ID)
	if cached, ok := pm.proxyCache[p.ID]; ok {
		cached.Used = cached.Used + 1
		cached.UpdatedAt = now
	}
	pm.mu.Unlock()

	return p.ID, p.ProxyStr, nil
}
