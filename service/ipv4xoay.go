package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

const (
	ipv4xoayBaseURL = "https://proxyxoay.shop/api/get.php"
)

// IPv4XoayResponse cấu trúc response từ IPv4Xoay API
type IPv4XoayResponse struct {
	Status                int    `json:"status"`
	Message               string `json:"message"`
	ProxyHTTP             string `json:"proxyhttp"`
	ProxySOCKS5           string `json:"proxysocks5"`
	NhaMang               string `json:"Nha Mang"`
	ViTri                 string `json:"Vi Tri"`
	TokenExpirationDate   string `json:"Token expiration date"`
	IP                    string `json:"ip"`
}

// IPv4Xoay service để interact với IPv4Xoay API (Singleton)
type IPv4Xoay struct {
	client *http.Client
}

var (
	ipv4xoayInstance *IPv4Xoay
	ipv4xoayOnce     sync.Once
)

// GetIPv4Xoay trả về singleton instance của IPv4Xoay
func GetIPv4Xoay() *IPv4Xoay {
	ipv4xoayOnce.Do(func() {
		ipv4xoayInstance = &IPv4Xoay{
			client: &http.Client{},
		}
	})
	return ipv4xoayInstance
}

// GetProxy lấy proxy từ IPv4Xoay (xài chung API cho cả GetNew và GetCurrent)
// Phương án 3: Nếu bị block (status 101), return (nil, nil) để thử lại sau
func (i *IPv4Xoay) GetProxy(apiKey string) (*IPv4XoayResponse, error) {
	url := fmt.Sprintf("%s?key=%s&nhamang=random&tinhthanh=0", ipv4xoayBaseURL, apiKey)

	resp, err := i.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result IPv4XoayResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Status 100: thành công
	if result.Status == 100 {
		return &result, nil
	}

	// Status 101: bị block, return (nil, nil) để thử lại sau (phương án 3)
	if result.Status == 101 {
		return nil, nil
	}

	// Status khác: lỗi
	return &result, fmt.Errorf("ipv4xoay api returned status: %d, message: %s", result.Status, result.Message)
}

// GetNewProxy wrapper để compatible với logic LoadProxiesFromList
func (i *IPv4Xoay) GetNewProxy(apiKey string) (*IPv4XoayResponse, error) {
	return i.GetProxy(apiKey)
}

// GetCurrentProxy wrapper để compatible với logic LoadProxiesFromList
func (i *IPv4Xoay) GetCurrentProxy(apiKey string) (*IPv4XoayResponse, error) {
	return i.GetProxy(apiKey)
}
