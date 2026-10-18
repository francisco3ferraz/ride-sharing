// Package ratelimit counts requests against limits that every copy of the Edge shares,
// kept in Valkey. When Valkey can't answer in time, it lets requests through: the
// limits protect capacity, and accounts are protected by Identity's login lockout,
// which doesn't depend on Valkey.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// How long a check may take before the request goes ahead unchecked. A check is one
// round trip inside the cluster, normally well under a millisecond.
const checkTimeout = 50 * time.Millisecond

// Limit allows Rate requests per Period, in bursts of up to Burst. The env tags let
// config read one from <prefix>RATE, <prefix>BURST and <prefix>PERIOD.
type Limit struct {
	Rate   int           `env:"RATE"`
	Burst  int           `env:"BURST"`
	Period time.Duration `env:"PERIOD"`
}

func (l Limit) Validate() error {
	if l.Rate <= 0 || l.Burst <= 0 || l.Period <= 0 {
		return errors.New("rate, burst and period must all be positive")
	}
	return nil
}

// Limiter checks limits with GCRA, through redis_rate. Each check is one Lua script, so
// concurrent checks from every copy of the Edge can't race.
type Limiter struct {
	limiter *redis_rate.Limiter
	checks  metric.Int64Counter
}

// New's client must have ContextTimeoutEnabled, or a check's deadline is ignored.
func New(client *redis.Client) (*Limiter, error) {
	// From the global meter provider, which telemetry.Setup installs. In Prometheus it's
	// rate_limit_checks_total.
	checks, err := otel.Meter("github.com/francisco3ferraz/ride-sharing/services/edge/internal/ratelimit").Int64Counter(
		"rate_limit.checks",
		metric.WithDescription("Rate-limit checks by limit and result: allowed, limited, or error when Valkey didn't answer and the request went ahead."),
		metric.WithUnit("{check}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create rate limit counter: %w", err)
	}
	return &Limiter{limiter: redis_rate.NewLimiter(client), checks: checks}, nil
}

// Allow counts one request for key against the limit called name. It returns false
// once the limit is used up, with how long until a request will be allowed again.
//
// The key Valkey stores starts with the name: GCRA keeps one timestamp per key, and the
// rate isn't part of it, so two limits sharing a key would corrupt each other.
func (l *Limiter) Allow(ctx context.Context, name, key string, limit Limit) (bool, time.Duration) {
	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	res, err := l.limiter.Allow(checkCtx, name+":"+key, redis_rate.Limit{Rate: limit.Rate, Burst: limit.Burst, Period: limit.Period})
	if err != nil {
		// Not the key, which holds a client address or user ID. A wrong ACL fails here
		// too, as NOPERM; Valkey's ACL LOG shows what it denied.
		log.Printf("check rate limit %s: %v", name, err)
		l.count(ctx, name, "error")
		return true, 0
	}
	if res.Allowed == 0 {
		l.count(ctx, name, "limited")
		return false, res.RetryAfter
	}
	l.count(ctx, name, "allowed")
	return true, 0
}

// Never the key as an attribute: every distinct value would be a time series of its own.
func (l *Limiter) count(ctx context.Context, name, result string) {
	l.checks.Add(ctx, 1, metric.WithAttributes(attribute.String("limit", name), attribute.String("result", result)))
}
