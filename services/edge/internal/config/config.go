// Package config loads the Edge's settings from environment variables.
package config

import "github.com/caarlos0/env/v11"

type Config struct {
	ListenAddr   string `env:"LISTEN_ADDR" envDefault:":8080"`
	PricingAddr  string `env:"PRICING_ADDR" envDefault:"localhost:50051"`
	IdentityAddr string `env:"IDENTITY_ADDR" envDefault:"localhost:50052"`
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}
