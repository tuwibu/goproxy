package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

const (
	tmproxyBaseURL = "https://tmproxy.com/api/proxy"
)

// TMProxyResponse cấu trúc response từ TMProxy API
type TMProxyResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    TMProxyData `json:"data"`
}

// TMProxyData chứa thông tin proxy từ TMProxy
type TMProxyData struct {
	IPAllow      string `json:"ip_allow"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	PublicIP     string `json:"public_ip"`
	ISPName      string `json:"isp_name"`
	LocationName string `json:"location_name"`
	SOCKS5       string `json:"socks5"`
	HTTPS        string `json:"https"`
	Timeout      int    `json:"timeout"`
	NextRequest  int    `json:"next_request"`
	ExpiredAt    int64  `json:"expired_at"`
}

// TMProxy service để interact với TMProxy API (Singleton)
type TMProxy struct {
	client *http.Client
}

var (
	tmproxyInstance *TMProxy
	tmproxyOnce     sync.Once
)

// GetTMProxy trả về singleton instance của TMProxy
func GetTMProxy() *TMProxy {
	tmproxyOnce.Do(func() {
		tmproxyInstance = &TMProxy{
			client: &http.Client{},
		}
	})
	return tmproxyInstance
}

// GetNewProxyRequest payload cho get-new-proxy
type GetNewProxyRequest struct {
	APIKey     string `json:"api_key"`
	IDLocation int    `json:"id_location"`
	IDISP      int    `json:"id_isp"`
}

// GetNewProxy lấy proxy mới từ TMProxy
func (t *TMProxy) GetNewProxy(apiKey string, idLocation, idISP int) (*TMProxyResponse, error) {
	payload := GetNewProxyRequest{
		APIKey:     apiKey,
		IDLocation: idLocation,
		IDISP:      idISP,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := t.client.Post(
		fmt.Sprintf("%s/get-new-proxy", tmproxyBaseURL),
		"application/json",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmproxy api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result TMProxyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

// GetCurrentProxyRequest payload cho get-current-proxy
type GetCurrentProxyRequest struct {
	APIKey string `json:"api_key"`
}

// GetCurrentProxy lấy proxy hiện tại từ TMProxy
func (t *TMProxy) GetCurrentProxy(apiKey string) (*TMProxyResponse, error) {
	payload := GetCurrentProxyRequest{
		APIKey: apiKey,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := t.client.Post(
		fmt.Sprintf("%s/get-current-proxy", tmproxyBaseURL),
		"application/json",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmproxy api returned status %d: %s", resp.StatusCode, string(body))
	}

	var result TMProxyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}
