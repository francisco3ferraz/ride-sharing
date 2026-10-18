// Package ratelimit counts requests against limits that every copy of the Edge shares,
// kept in Valkey. When Valkey can't answer in time, it lets requests through: the
// limits protect capacity, and accounts are protected by Identity's login lockout,
// which doesn't depend on Valkey.
package ratelimit

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
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
}

// New's client must have ContextTimeoutEnabled, or a check's deadline is ignored.
func New(client *redis.Client) *Limiter {
	return &Limiter{limiter: redis_rate.NewLimiter(client)}
}

// Allow counts one request for key against the limit called name. It returns false
// once the limit is used up, with how long until a request will be allowed again.
//
// The key Valkey stores starts with the name: GCRA keeps one timestamp per key, and the
// rate isn't part of it, so two limits sharing a key would corrupt each other.
func (l *Limiter) Allow(ctx context.Context, name, key string, limit Limit) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	res, err := l.limiter.Allow(ctx, name+":"+key, redis_rate.Limit{Rate: limit.Rate, Burst: limit.Burst, Period: limit.Period})
	if err != nil {
		// Not the key, which holds a client address or user ID. A wrong ACL fails here
		// too, as NOPERM; Valkey's ACL LOG shows what it denied.
		log.Printf("check rate limit %s: %v", name, err)
		return true, 0
	}
	if res.Allowed == 0 {
		return false, res.RetryAfter
	}
	return true, 0
}
