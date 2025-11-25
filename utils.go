package goproxy

import "fmt"

// validateProxyType kiểm tra proxy type có hợp lệ không
func (pm *ProxyManager) validateProxyType(proxyType ProxyType) error {
	switch proxyType {
	case ProxyTypeTMProxy, ProxyTypeStatic, ProxyTypeMobileHop:
		return nil
	default:
		return fmt.Errorf("unknown proxy type: %s", proxyType)
	}
}
