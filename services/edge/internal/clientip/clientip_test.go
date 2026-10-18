package clientip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestFromRequest(t *testing.T) {
	tests := []struct {
		name   string
		header []string // one X-Forwarded-For line each
		want   string   // empty when no address should be found
	}{
		{name: "one entry", header: []string{"203.0.113.7"}, want: "203.0.113.7"},
		{name: "forged entries before Traefik's", header: []string{"10.0.0.1, 198.51.100.4, 203.0.113.7"}, want: "203.0.113.7"},
		{name: "no spaces", header: []string{"198.51.100.4,203.0.113.7"}, want: "203.0.113.7"},
		{name: "two header lines", header: []string{"198.51.100.4", "203.0.113.7"}, want: "203.0.113.7"},
		{name: "IPv6", header: []string{"2001:db8::1"}, want: "2001:db8::1"},
		{name: "IPv4 written as IPv6", header: []string{"::ffff:203.0.113.7"}, want: "203.0.113.7"},
		{name: "no header"},
		{name: "empty header", header: []string{""}},
		{name: "last entry empty", header: []string{"203.0.113.7, "}},
		{name: "last entry not an address", header: []string{"203.0.113.7, unknown"}},
		{name: "last entry with a port", header: []string{"203.0.113.7:443"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			for _, line := range tt.header {
				req.Header.Add("X-Forwarded-For", line)
			}

			got, ok := FromRequest(req)

			if tt.want == "" {
				assert.False(t, ok, "found %s", got)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, netip.MustParseAddr(tt.want), got)
		})
	}
}

func TestContext(t *testing.T) {
	_, ok := FromContext(context.Background())
	assert.False(t, ok)

	addr := netip.MustParseAddr("203.0.113.7")
	got, ok := FromContext(NewContext(context.Background(), addr))
	assert.True(t, ok)
	assert.Equal(t, addr, got)
}

func TestUnaryClientInterceptor(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{name: "with an address", ctx: NewContext(context.Background(), netip.MustParseAddr("2001:db8::1")), want: []string{"2001:db8::1"}},
		{name: "without one", ctx: context.Background()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Metadata someone added earlier must survive.
			ctx := metadata.AppendToOutgoingContext(tt.ctx, "earlier", "kept")
			var got metadata.MD
			invoker := func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				got, _ = metadata.FromOutgoingContext(ctx)
				return nil
			}

			err := UnaryClientInterceptor(ctx, "/ridesharing.identity.v1.IdentityService/Login", nil, nil, nil, invoker)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Get(MetadataKey))
			assert.Equal(t, []string{"kept"}, got.Get("earlier"))
		})
	}
}
