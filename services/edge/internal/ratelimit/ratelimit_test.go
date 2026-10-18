package ratelimit

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/valkey"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.yaml.in/yaml/v3"
)

const testPassword = "a test password"

func TestLimiter(t *testing.T) {
	ctx := t.Context()
	// Where the limiter's counter reports, read on demand. Each subtest checks a limit
	// name of its own.
	metrics := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(metrics)))

	// The cluster's Valkey build, the chart's app version in the Tiltfile. Bump both.
	ctr, err := valkey.Run(ctx, "valkey/valkey:9.1.2")
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)
	connString, err := ctr.ConnectionString(ctx)
	require.NoError(t, err)

	// The container's default user can do anything. The limiter logs in as the Edge
	// does, with the ACL the cluster's Valkey gives it.
	admin := newClient(t, connString, "", "")
	args := []any{"ACL", "SETUSER", "edge", "on", ">" + testPassword}
	for _, rule := range strings.Fields(edgeACL(t)) {
		args = append(args, rule)
	}
	require.NoError(t, admin.Do(ctx, args...).Err())
	limiter := newLimiter(t, newClient(t, connString, "edge", testPassword))

	t.Run("allows a burst, then limits", func(t *testing.T) {
		limit := Limit{Rate: 1, Burst: 3, Period: time.Minute}
		for i := range 3 {
			ok, _ := limiter.Allow(ctx, "burst", "203.0.113.7", limit)
			require.True(t, ok, "request %d", i+1)
		}

		ok, retryAfter := limiter.Allow(ctx, "burst", "203.0.113.7", limit)
		assert.False(t, ok)
		assert.Positive(t, retryAfter)
		assert.LessOrEqual(t, retryAfter, time.Minute)
		assert.Equal(t, map[string]int64{"allowed": 3, "limited": 1}, checkCounts(t, metrics, "burst"))
	})

	t.Run("each key, and each limit, counts separately", func(t *testing.T) {
		limit := Limit{Rate: 1, Burst: 1, Period: time.Minute}
		ok, _ := limiter.Allow(ctx, "one", "203.0.113.7", limit)
		require.True(t, ok)
		ok, _ = limiter.Allow(ctx, "one", "203.0.113.7", limit)
		require.False(t, ok)

		ok, _ = limiter.Allow(ctx, "one", "203.0.113.8", limit)
		assert.True(t, ok, "another key")
		ok, _ = limiter.Allow(ctx, "two", "203.0.113.7", limit)
		assert.True(t, ok, "another limit")
	})

	t.Run("keys can't leave the Edge's prefix", func(t *testing.T) {
		edge := newClient(t, connString, "edge", testPassword)
		assert.ErrorContains(t, edge.Set(ctx, "other:key", "1", time.Minute).Err(), "NOPERM")
	})

	t.Run("lets requests through when Valkey can't be reached", func(t *testing.T) {
		unreachable := newLimiter(t, newClient(t, "redis://127.0.0.1:1", "", ""))

		start := time.Now()
		ok, _ := unreachable.Allow(ctx, "unreachable", "203.0.113.7", Limit{Rate: 1, Burst: 1, Period: time.Minute})
		assert.True(t, ok)
		assert.Less(t, time.Since(start), time.Second)
		assert.Equal(t, map[string]int64{"error": 1}, checkCounts(t, metrics, "unreachable"))
	})

	// Last, since the pause holds every other client until it ends.
	t.Run("lets requests through when Valkey is too slow", func(t *testing.T) {
		require.NoError(t, admin.ClientPause(ctx, 2*time.Second).Err())

		start := time.Now()
		ok, _ := limiter.Allow(ctx, "slow", "203.0.113.7", Limit{Rate: 1, Burst: 1, Period: time.Minute})
		assert.True(t, ok)
		assert.Less(t, time.Since(start), 500*time.Millisecond)
		assert.Equal(t, map[string]int64{"error": 1}, checkCounts(t, metrics, "slow"))
	})
}

func newLimiter(t *testing.T, client *redis.Client) *Limiter {
	t.Helper()
	limiter, err := New(client)
	require.NoError(t, err)
	return limiter
}

// checkCounts returns how many checks of the limit called name the counter recorded,
// by result.
func checkCounts(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "rate_limit.checks" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				limit, _ := point.Attributes.Value("limit")
				result, _ := point.Attributes.Value("result")
				if limit.AsString() == name {
					counts[result.AsString()] += point.Value
				}
			}
		}
	}
	return counts
}

// newClient is set up as the Edge's main.go sets up its client.
func newClient(t *testing.T, connString, username, password string) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(connString)
	require.NoError(t, err)
	opts.Username = username
	opts.Password = password
	opts.ContextTimeoutEnabled = true
	opts.MaxRetries = -1
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// edgeACL reads the Edge's ACL rules from Valkey's Helm values, so this test fails if
// they stop letting the limiter work.
func edgeACL(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../../infra/k8s/platform/valkey/values.yaml")
	require.NoError(t, err)
	var values struct {
		Auth struct {
			ACLUsers map[string]struct {
				Permissions string `yaml:"permissions"`
			} `yaml:"aclUsers"`
		} `yaml:"auth"`
	}
	require.NoError(t, yaml.Unmarshal(data, &values))
	permissions := values.Auth.ACLUsers["edge"].Permissions
	require.NotEmpty(t, permissions, "no ACL for the edge user in values.yaml")
	return permissions
}
