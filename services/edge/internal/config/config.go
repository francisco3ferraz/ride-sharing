// Package config loads the Edge's settings from environment variables.
package config

import "github.com/caarlos0/env/v11"

type Config struct {
	ListenAddr   string `env:"LISTEN_ADDR" envDefault:":8080"`
	PricingAddr  string `env:"PRICING_ADDR" envDefault:"localhost:50051"`
	IdentityAddr string `env:"IDENTITY_ADDR" envDefault:"localhost:50052"`
	// Identity serves it on 8080, which the Edge itself listens on, so running both
	// locally means port-forwarding Identity's to 8081.
	IdentityJWKSURL string `env:"IDENTITY_JWKS_URL" envDefault:"http://localhost:8081/.well-known/jwks.json"`
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}
