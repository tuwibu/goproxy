package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

const (
	kiotproxyBaseURL = "https://api.kiotproxy.com/api/v1/proxies"
)

// KiotProxyResponse cấu trúc response từ KiotProxy API
type KiotProxyResponse struct {
	Success   bool          `json:"success"`
	Code      int           `json:"code"`
	Message   string        `json:"message"`
	Status    string        `json:"status"`
	Error     string        `json:"error"`
	Timestamp int64         `json:"timestamp"`
	Data      KiotProxyData `json:"data"`
}

// KiotProxyData chứa thông tin proxy từ KiotProxy
type KiotProxyData struct {
	RealIPAddress string `json:"realIpAddress"`
	HTTP          string `json:"http"`
	SOCKS5        string `json:"socks5"`
	NextRequestAt int64  `json:"nextRequestAt"`
	HTTPPort      int    `json:"httpPort"`
	SOCKS5Port    int    `json:"socks5Port"`
	Host          string `json:"host"`
	Location      string `json:"location"`
	ExpirationAt  int64  `json:"expirationAt"`
	TTL           int    `json:"ttl"`
	TTC           int    `json:"ttc"`
}

// KiotProxy service để interact với KiotProxy API (Singleton)
type KiotProxy struct {
	client *http.Client
}

var (
	kiotproxyInstance *KiotProxy
	kiotproxyOnce     sync.Once
)

// GetKiotProxy trả về singleton instance của KiotProxy
func GetKiotProxy() *KiotProxy {
	kiotproxyOnce.Do(func() {
		kiotproxyInstance = &KiotProxy{
			client: &http.Client{},
		}
	})
	return kiotproxyInstance
}

// GetNewProxy lấy proxy mới từ KiotProxy
func (k *KiotProxy) GetNewProxy(apiKey, region string) (*KiotProxyResponse, error) {
	url := fmt.Sprintf("%s/new?key=%s", kiotproxyBaseURL, apiKey)
	if region != "" {
		url += fmt.Sprintf("&region=%s", region)
	}

	resp, err := k.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result KiotProxyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !result.Success {
		return &result, fmt.Errorf("kiotproxy api returned error: code=%d, message=%s, error=%s", result.Code, result.Message, result.Error)
	}

	return &result, nil
}

// GetCurrentProxy lấy proxy hiện tại từ KiotProxy
func (k *KiotProxy) GetCurrentProxy(apiKey string) (*KiotProxyResponse, error) {
	url := fmt.Sprintf("%s/current?key=%s", kiotproxyBaseURL, apiKey)

	resp, err := k.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result KiotProxyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !result.Success {
		return &result, fmt.Errorf("kiotproxy api returned error: code=%d, message=%s, error=%s", result.Code, result.Message, result.Error)
	}

	return &result, nil
}

