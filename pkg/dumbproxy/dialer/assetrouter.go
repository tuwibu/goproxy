package dialer

import (
	"context"
	"net"
	"path"
	"strings"

	"github.com/tuwibu/goproxy/pkg/dumbproxy/dialer/dto"
)

// Static asset extensions - các extension được coi là static assets
var staticAssetExtensions = map[string]bool{
	// JavaScript & CSS
	".js":  true,
	".css": true,
	".mjs": true,
	".cjs": true,

	// Images
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".svg":  true,
	".webp": true,
	".ico":  true,
	".avif": true,
	".bmp":  true,
	".tiff": true,
	".tif":  true,

	// Fonts
	".woff":  true,
	".woff2": true,
	".ttf":   true,
	".otf":   true,
	".eot":   true,

	// Video
	".mp4":  true,
	".webm": true,
	".mkv":  true,
	".avi":  true,
	".mov":  true,
	".wmv":  true,
	".flv":  true,
	".m4v":  true,

	// Audio
	".mp3":  true,
	".ogg":  true,
	".wav":  true,
	".flac": true,
	".m4a":  true,
	".aac":  true,
	".wma":  true,

	// Documents
	".pdf": true,

	// Maps
	".map": true,
}

// AssetRoutingDialer routes requests based on content type
// Static assets go through direct connection, other requests go through upstream proxy
type AssetRoutingDialer struct {
	directDialer   Dialer // For static assets (direct connection)
	upstreamDialer Dialer // For other requests (via upstream proxy)
}

// NewAssetRoutingDialer creates a new AssetRoutingDialer
func NewAssetRoutingDialer(direct, upstream Dialer) *AssetRoutingDialer {
	return &AssetRoutingDialer{
		directDialer:   direct,
		upstreamDialer: upstream,
	}
}

// isStaticAsset checks if the request is for a static asset
func (d *AssetRoutingDialer) isStaticAsset(ctx context.Context) bool {
	// Get the original request from context
	req, _ := dto.FilterParamsFromContext(ctx)
	if req == nil {
		return false
	}

	// Check URL path extension
	urlPath := req.URL.Path
	ext := strings.ToLower(path.Ext(urlPath))
	if staticAssetExtensions[ext] {
		return true
	}

	// Check Accept header for media types
	accept := req.Header.Get("Accept")
	if accept != "" {
		if strings.Contains(accept, "image/") ||
			strings.Contains(accept, "video/") ||
			strings.Contains(accept, "audio/") ||
			strings.Contains(accept, "font/") ||
			strings.Contains(accept, "application/font") {
			return true
		}
	}

	// Check for common CDN/asset paths
	lowerPath := strings.ToLower(urlPath)
	if strings.Contains(lowerPath, "/assets/") ||
		strings.Contains(lowerPath, "/static/") ||
		strings.Contains(lowerPath, "/images/") ||
		strings.Contains(lowerPath, "/img/") ||
		strings.Contains(lowerPath, "/fonts/") ||
		strings.Contains(lowerPath, "/css/") ||
		strings.Contains(lowerPath, "/js/") ||
		strings.Contains(lowerPath, "/media/") {

		// Additional check: ensure it's not an API call masquerading as static
		if !strings.Contains(lowerPath, "/api/") {
			// Check if URL has query params that suggest dynamic content
			if req.URL.RawQuery == "" || !strings.Contains(req.URL.RawQuery, "callback") {
				return true
			}
		}
	}

	return false
}

// DialContext dials with context, routing based on asset type
func (d *AssetRoutingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d.isStaticAsset(ctx) {
		return d.directDialer.DialContext(ctx, network, address)
	}
	return d.upstreamDialer.DialContext(ctx, network, address)
}

// Dial dials without context, defaults to upstream
func (d *AssetRoutingDialer) Dial(network, address string) (net.Conn, error) {
	// Without context, we can't determine if it's a static asset
	// Default to upstream for safety
	return d.upstreamDialer.Dial(network, address)
}
