# Kế hoạch sửa bugs phát hiện từ tests

## Tổng quan
Sau khi chạy tests, phát hiện **5 bugs chính** cần sửa để code đúng với requirements.

## Chi tiết các bugs

### Bug 1: unique_key không dùng MD5 hash ❌
**File**: `db.go` dòng 150-156  
**Vấn đề**: 
- Hiện tại: `unique_key = apiKey` (tmproxy) hoặc `unique_key = proxyStr` (static/mobilehop)
- Yêu cầu: `unique_key = MD5(apiKey)` (tmproxy) hoặc `unique_key = MD5(proxyStr)` (static/mobilehop)

**Test fail**: 
- `TestLoadProxiesFromListMobileHop`
- `TestLoadProxiesFromListUniqueKeyMD5`

**Cách sửa**:
```go
import "crypto/md5"

// Trong LoadProxiesFromList, thay dòng 150-156:
var uniqueKey string
if pType == ProxyTypeTMProxy {
    uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(apiKey)))
} else {
    uniqueKey = fmt.Sprintf("%x", md5.Sum([]byte(proxyStr)))
}
```

---

### Bug 2: Scan NULL values vào string thường ❌
**File**: `db.go` dòng 271  
**Vấn đề**: 
- Code đang scan `api_key` và `change_url` (có thể NULL) vào `string` thường
- Khi giá trị NULL → lỗi: "converting NULL to string is unsupported"

**Test fail**: 
- `TestGetAvailableProxyConditions/available:_running=false,_used_<_max_used`
- `TestGetAvailableProxyConditionLogic/used_<_max_used,_min_time_not_passed`

**Cách sửa**:
```go
// Thay dòng 267-271:
var p Proxy
var lastIP sql.NullString
var lastChanged sql.NullTime
var errStr sql.NullString
var apiKey sql.NullString      // THÊM
var changeUrl sql.NullString   // THÊM

err = rows.Scan(&p.ID, &p.Type, &p.ProxyStr, &apiKey, &changeUrl, &p.MinTime, &p.Running, &p.Used, &lastIP, &lastChanged, &errStr, &p.CreatedAt, &p.UpdatedAt)

// Sau đó xử lý:
if apiKey.Valid {
    p.ApiKey = apiKey.String
}
if changeUrl.Valid {
    p.ChangeUrl = changeUrl.String
}
```

---

### Bug 3: Logic query GetAvailableProxy sai ❌
**File**: `db.go` dòng 218-259  
**Vấn đề**: 
- Query hiện tại: Tìm `used < maxUsed` TRƯỚC, sau đó mới tìm `used >= maxUsed AND min_time passed`
- Yêu cầu: `running=false AND (used < max_used OR last_changed + min_time < now)`
- Query hiện tại không đúng logic OR

**Test fail**: 
- `TestGetAvailableProxyConditions/available:_running=false,_used_>=_max_used_but_min_time_passed`
- `TestGetAvailableProxyConditions/available:_min_time=0_(always_available)`
- `TestEdgeCaseMinTimeZero`
- `TestEdgeCaseMaxUsedZero`
- `TestGetAvailableProxyMinTimeZero`
- `TestGetAvailableProxyConditionLogic/used_>=_max_used,_min_time_passed`

**Cách sửa**:
```go
// Thay toàn bộ query logic (dòng 218-259) bằng 1 query duy nhất:
rows, err := pm.db.Query(`
    SELECT id, type, proxy_str, api_key, change_url, min_time, running, used, last_ip, last_changed, error, created_at, updated_at
    FROM proxies 
    WHERE running=false 
    AND (error IS NULL OR error='')
    AND (
        used < ? 
        OR 
        (min_time = 0 OR (last_changed IS NULL OR (julianday(?) - julianday(last_changed)) * 86400 >= min_time))
    )
    ORDER BY RANDOM()
    LIMIT 1
`, pm.maxUsed, now)
```

---

### Bug 4: min_time = 0 không được handle đúng ❌
**File**: `db.go` dòng 245  
**Vấn đề**: 
- Query hiện tại có điều kiện `min_time > 0` → loại bỏ các proxy có `min_time = 0`
- Yêu cầu: `min_time = 0` nghĩa là có thể change IP liên tục, nên luôn đủ điều kiện

**Test fail**: 
- `TestEdgeCaseMinTimeZero`
- `TestGetAvailableProxyMinTimeZero`
- `TestGetAvailableProxyConditions/available:_min_time=0_(always_available)`

**Cách sửa**: 
- Đã bao gồm trong Bug 3 (sửa query logic)

---

### Bug 5: LoadProxiesFromList không luôn set last_changed ❌
**File**: `db.go` dòng 163-165  
**Vấn đề**: 
- Hiện tại: Chỉ set `last_changed` khi `getNewProxyCalled = true`
- Yêu cầu: "luôn luôn lưu last_changed là time hiện tại" khi load

**Test fail**: 
- `TestLoadProxiesFromListAlwaysSetLastChanged`

**Cách sửa**:
```go
// Thay dòng 163-165:
// Luôn set last_changed = now() khi load
pm.db.Exec(`UPDATE proxies SET last_changed=? WHERE id=?`, time.Now(), id)

// Xóa điều kiện if getNewProxyCalled
```

---

## Thứ tự ưu tiên sửa

1. **Bug 2** (Scan NULL) - Quan trọng nhất, gây crash
2. **Bug 3 + Bug 4** (Query logic) - Ảnh hưởng core functionality
3. **Bug 1** (MD5 hash) - Đúng requirements nhưng không crash
4. **Bug 5** (last_changed) - Đúng requirements nhưng không crash

## Files cần sửa

- `db.go`: Tất cả 5 bugs
- Có thể cần import thêm: `crypto/md5`, `fmt`

## Sau khi sửa

Chạy lại tests để verify:
```bash
go test -v
```

Kỳ vọng: Tất cả tests pass hoặc chỉ còn các tests liên quan đến TMProxy API calls (cần mock để test đầy đủ).

