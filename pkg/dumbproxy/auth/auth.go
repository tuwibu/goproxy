package auth

import (
	"context"
	"io"
	"net/http"
)

type Auth interface {
	Validate(ctx context.Context, wr http.ResponseWriter, req *http.Request) (string, bool)
	io.Closer
}

// NewAuth creates a new Auth instance - simplified to only support NoAuth
func NewAuth(paramstr string) (Auth, error) {
	return NoAuth{}, nil
}
