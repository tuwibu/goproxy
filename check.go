package goproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// parseProxyString tách proxy string thành các thành phần
// Format: ip:port hoặc ip:port:username:password
func parseProxyString(proxyStr string) (ProxyStringInfo, error) {
	parts := strings.Split(strings.TrimSpace(proxyStr), ":")
	if len(parts) < 2 {
		return ProxyStringInfo{}, fmt.Errorf("invalid proxy format: %s, expected ip:port or ip:port:username:password", proxyStr)
	}

	info := ProxyStringInfo{
		Address: fmt.Sprintf("%s:%s", parts[0], parts[1]),
	}

	if len(parts) == 4 {
		info.Username = parts[2]
		info.Password = parts[3]
	} else if len(parts) > 4 {
		return ProxyStringInfo{}, fmt.Errorf("invalid proxy format: %s, too many parts", proxyStr)
	}

	return info, nil
}

func GetLocalIP() (CheckProxyResponse, error) {
	url := "https://worker.goprofilev2.net/ip"
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer func(Body io.ReadCloser) {
		if errClose := Body.Close(); errClose != nil {
			log.Fatal(errClose.Error())
		}
	}(resp.Body)
	body, errBody := io.ReadAll(resp.Body)
	if errBody != nil {
		return CheckProxyResponse{}, errBody
	}
	response := CheckProxyResponse{}
	if err := json.Unmarshal(body, &response); err != nil {
		return CheckProxyResponse{}, err
	}
	return response, nil
}

// CheckProxy kiểm tra proxy với format string (http protocol only)
// proxyStr format: ip:port hoặc ip:port:username:password
func CheckProxy(ctx context.Context, proxyStr string) (CheckProxyResponse, error) {
	info, err := parseProxyString(proxyStr)
	if err != nil {
		return CheckProxyResponse{}, err
	}

	var proxyURL *url.URL
	if info.Username != "" && info.Password != "" {
		proxyURL, err = url.Parse(fmt.Sprintf("http://%s:%s@%s", info.Username, info.Password, info.Address))
	} else {
		proxyURL, err = url.Parse(fmt.Sprintf("http://%s", info.Address))
	}
	if err != nil {
		return CheckProxyResponse{}, fmt.Errorf("failed to parse proxy url: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ip.zmmo.net/ip", nil)
	if err != nil {
		return CheckProxyResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return CheckProxyResponse{}, fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CheckProxyResponse{}, fmt.Errorf("proxy error: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CheckProxyResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	response := CheckProxyResponse{}
	if err := json.Unmarshal(body, &response); err != nil {
		return CheckProxyResponse{}, fmt.Errorf("failed to parse response: %w", err)
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

// waitForIPChange chờ IP proxy thay đổi hoặc timeout
func (pm *ProxyManager) waitForIPChange(ctx context.Context, proxyStr, initialIP string, timeout time.Duration) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context canceled")
		case <-timeoutTimer.C:
			return "", fmt.Errorf("timeout waiting for proxy IP to change after %v", timeout)
		case <-ticker.C:
			resp, err := CheckProxy(ctx, proxyStr)
			if err != nil {
				// Lỗi tạm thời, tiếp tục chờ
				continue
			}

			currentIP := resp.Query
			if currentIP != initialIP && currentIP != "" {
				return currentIP, nil
			}
		}
	}
}
