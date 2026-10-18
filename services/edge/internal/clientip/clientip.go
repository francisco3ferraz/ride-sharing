// Package clientip finds the address of the client that sent a request, and carries it
// in the request's context and on to the services the Edge calls.
package clientip

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// MetadataKey is the gRPC metadata entry the client's address goes in. Identity reads it
// for its login lockout.
const MetadataKey = "client-ip"

// FromRequest returns the address Traefik received the request from, the last entry
// Traefik appended to X-Forwarded-For. Anything before that entry came from the
// client, which can write whatever it likes there. r.RemoteAddr is Traefik's own pod
// address, the same for every request, so it's never used.
//
// It returns false when the last entry is missing or isn't an address, which means the
// request didn't come through Traefik (a port-forward, or another pod). It never falls
// back to an earlier entry.
func FromRequest(r *http.Request) (netip.Addr, bool) {
	// Values, not Get: Get returns the first header line, and the last entry is in the
	// last line.
	lines := r.Header.Values("X-Forwarded-For")
	if len(lines) == 0 {
		return netip.Addr{}, false
	}
	last := lines[len(lines)-1]
	if i := strings.LastIndexByte(last, ','); i >= 0 {
		last = last[i+1:]
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(last))
	if err != nil {
		return netip.Addr{}, false
	}
	// An IPv4 client can also be written as IPv6 (::ffff:192.0.2.1). Either way it's one
	// client, so it must make one key.
	return addr.Unmap(), true
}

// Unexported, so no other package can read or replace the address.
type contextKey struct{}

func NewContext(ctx context.Context, addr netip.Addr) context.Context {
	return context.WithValue(ctx, contextKey{}, addr)
}

// FromContext returns the address NewContext put in ctx. ok is false when there's none.
func FromContext(ctx context.Context) (netip.Addr, bool) {
	addr, ok := ctx.Value(contextKey{}).(netip.Addr)
	return addr, ok
}

// UnaryClientInterceptor sends the address in ctx, when there is one, with every gRPC
// call, so no handler has to remember to.
func UnaryClientInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	if addr, ok := FromContext(ctx); ok {
		// Append, not NewOutgoingContext, which would replace any metadata already there.
		ctx = metadata.AppendToOutgoingContext(ctx, MetadataKey, addr.String())
	}
	return invoker(ctx, method, req, reply, cc, opts...)
}
