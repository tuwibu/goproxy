package goproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProxyStringInfo đại diện cho thông tin từ proxy string
type ProxyStringInfo struct {
	Address  string // ip:port
	Username string
	Password string
}

func parseProxyString(proxyStr string) (ProxyStringInfo, error) {
	parts := strings.Split(strings.TrimSpace(proxyStr), ":")
	if len(parts) < 2 || len(parts) > 4 {
		return ProxyStringInfo{}, fmt.Errorf("invalid proxy format: %s", proxyStr)
	}

	info := ProxyStringInfo{Address: fmt.Sprintf("%s:%s", parts[0], parts[1])}
	if len(parts) == 4 {
		info.Username, info.Password = parts[2], parts[3]
	}
	return info, nil
}

func CheckProxy(ctx context.Context, proxyStr string) (CheckProxyResponse, error) {
	info, err := parseProxyString(proxyStr)
	if err != nil {
		return CheckProxyResponse{}, err
	}

	urlStr := fmt.Sprintf("http://%s", info.Address)
	if info.Username != "" {
		urlStr = fmt.Sprintf("http://%s:%s@%s", info.Username, info.Password, info.Address)
	}

	proxyURL, _ := url.Parse(urlStr)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   30 * time.Second,
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://ip.zmmo.net/ip", nil)
	resp, err := client.Do(req)
	if err != nil {
		return CheckProxyResponse{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	response := CheckProxyResponse{}
	if err := json.Unmarshal(body, &response); err != nil {
		return CheckProxyResponse{}, err
	}
	if response.Status != "success" {
		return CheckProxyResponse{}, fmt.Errorf("proxy is not live")
	}
	return response, nil
}

func CheckValidIp(ctx context.Context, ip string, count int) (bool, error) {
	url := fmt.Sprintf("https://ip2.egde.net/api/ip/check2?userId=16f2f8c6-7780-4a16-9763-afc5c082e6d7&ip=%s&count=%d", ip, count)
	resp, err := http.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	response := CheckValidIpResponse{}
	if err := json.Unmarshal(body, &response); err != nil {
		return false, err
	}
	if !response.Success {
		return false, fmt.Errorf("error: %s", response.Message)
	}
	return response.Success, nil
}

// callChangeURL gọi change URL API (GET request)
func (pm *ProxyManager) callChangeURL(ctx context.Context, changeURL string) error {
	if changeURL == "" {
		return fmt.Errorf("changeURL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, changeURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call change_url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("change_url returned status %d", resp.StatusCode)
	}

	return nil
}
