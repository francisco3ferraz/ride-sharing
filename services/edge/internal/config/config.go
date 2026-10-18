// Package config loads the Edge's settings from environment variables.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/francisco3ferraz/ride-sharing/services/edge/internal/ratelimit"
)

type Config struct {
	ListenAddr   string `env:"LISTEN_ADDR" envDefault:":8080"`
	PricingAddr  string `env:"PRICING_ADDR" envDefault:"localhost:50051"`
	IdentityAddr string `env:"IDENTITY_ADDR" envDefault:"localhost:50052"`
	// Identity serves it on 8080, which the Edge itself listens on, so running both
	// locally means port-forwarding Identity's to 8081.
	IdentityJWKSURL string `env:"IDENTITY_JWKS_URL" envDefault:"http://localhost:8081/.well-known/jwks.json"`
	ValkeyAddr      string `env:"VALKEY_ADDR" envDefault:"localhost:6379"`
	// The Edge's own ACL user. Its password is the edge key of the valkey-users Secret,
	// which the Tiltfile generates.
	ValkeyUsername string `env:"VALKEY_USERNAME" envDefault:"edge"`
	ValkeyPassword string `env:"VALKEY_PASSWORD"`

	// Each rate limit reads <prefix>RATE, <prefix>BURST and <prefix>PERIOD, a Go
	// duration such as 1s or 1h. Load sets their defaults.
	IPLimit   ratelimit.Limit `envPrefix:"RATE_LIMIT_IP_"`
	UserLimit ratelimit.Limit `envPrefix:"RATE_LIMIT_USER_"`
}

func Load() (Config, error) {
	// envDefault on Limit's fields would give every limit the same values, so the
	// defaults are set here. env.Parse keeps any field no variable is set for.
	cfg := Config{
		IPLimit:   ratelimit.Limit{Rate: 20, Burst: 40, Period: time.Second},
		UserLimit: ratelimit.Limit{Rate: 10, Burst: 20, Period: time.Second},
	}
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}

	limits := map[string]ratelimit.Limit{
		"RATE_LIMIT_IP_":   cfg.IPLimit,
		"RATE_LIMIT_USER_": cfg.UserLimit,
	}
	for prefix, limit := range limits {
		if err := limit.Validate(); err != nil {
			return Config{}, fmt.Errorf("%s*: %w", prefix, err)
		}
	}
	return cfg, nil
}
