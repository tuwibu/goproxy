package handler

import (
	"github.com/tuwibu/goproxy/pkg/dumbproxy/auth"
	clog "github.com/tuwibu/goproxy/pkg/dumbproxy/log"
)

type Config struct {
	// Dialer optionally specifies dialer to use for creating
	// connections originating from proxy.
	Dialer HandlerDialer
	// Auth optionally specifies request validator used to verify users
	// and return their username.
	Auth auth.Auth
	// Logger specifies optional custom logger.
	Logger *clog.CondLogger
	// Forward optionally specifies custom connection pairing function
	// which does actual data forwarding.
	Forward ForwardFunc
	// UserIPHints specifies whether allow IP hints set by user or not
	UserIPHints bool
}
