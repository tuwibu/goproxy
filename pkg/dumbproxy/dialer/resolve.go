package dialer

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/tuwibu/goproxy/pkg/dumbproxy/dialer/dto"
	"github.com/hashicorp/go-multierror"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type NameResolvingDialer struct {
	next     Dialer
	resolver Resolver
}

func NewNameResolvingDialer(next Dialer, resolver Resolver) NameResolvingDialer {
	return NameResolvingDialer{
		next:     next,
		resolver: resolver,
	}
}

func (nrd NameResolvingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if WantsHostname(ctx, network, address, nrd.next) {
		return nrd.next.DialContext(ctx, network, address)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("failed to extract host and port from %s: %w", address, err)
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		// literal IP address, just do unmapping
		return nrd.next.DialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
	}

	var resolveNetwork string
	switch network {
	case "udp4", "tcp4", "ip4":
		resolveNetwork = "ip4"
	case "udp6", "tcp6", "ip6":
		resolveNetwork = "ip6"
	case "udp", "tcp", "ip":
		resolveNetwork = "ip"
	default:
		return nil, fmt.Errorf("resolving dial %q: unsupported network %q", address, network)
	}

	res, err := nrd.resolver.LookupNetIP(ctx, resolveNetwork, host)
	if err != nil {
		return nil, fmt.Errorf("resolving %q (%s) failed: %w", host, network, err)
	}
	for i := range res {
		res[i] = res[i].Unmap()
	}

	ctx = dto.OrigDstToContext(ctx, address)

	var dialErr error
	var conn net.Conn

	for _, ip := range res {
		conn, err = nrd.next.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = multierror.Append(dialErr, err)
	}

	return nil, fmt.Errorf("failed to dial %s: %w", address, dialErr)
}

func (nrd NameResolvingDialer) Dial(network, address string) (net.Conn, error) {
	return nrd.DialContext(context.Background(), network, address)
}

func (nrd NameResolvingDialer) WantsHostname(ctx context.Context, net, address string) bool {
	return WantsHostname(ctx, net, address, nrd.next)
}

var _ Dialer = NameResolvingDialer{}
var _ HostnameWanter = NameResolvingDialer{}

// BoundDialer is a dialer that binds to a specific local address
type BoundDialer struct {
	next      *net.Dialer
	localAddr string
}

// NewBoundDialer creates a new dialer optionally bound to a local address
func NewBoundDialer(next *net.Dialer, localAddr string) *BoundDialer {
	return &BoundDialer{
		next:      next,
		localAddr: localAddr,
	}
}

func (d *BoundDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := d.next
	if d.localAddr != "" {
		switch network {
		case "tcp", "tcp4", "tcp6":
			addr, err := net.ResolveTCPAddr(network, d.localAddr+":0")
			if err != nil {
				return nil, fmt.Errorf("failed to resolve local address: %w", err)
			}
			dialer = &net.Dialer{
				LocalAddr: addr,
				Timeout:   d.next.Timeout,
				Deadline:  d.next.Deadline,
				KeepAlive: d.next.KeepAlive,
			}
		case "udp", "udp4", "udp6":
			addr, err := net.ResolveUDPAddr(network, d.localAddr+":0")
			if err != nil {
				return nil, fmt.Errorf("failed to resolve local address: %w", err)
			}
			dialer = &net.Dialer{
				LocalAddr: addr,
				Timeout:   d.next.Timeout,
				Deadline:  d.next.Deadline,
				KeepAlive: d.next.KeepAlive,
			}
		}
	}
	return dialer.DialContext(ctx, network, address)
}

func (d *BoundDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

var _ Dialer = (*BoundDialer)(nil)
