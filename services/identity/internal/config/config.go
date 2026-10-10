// Package config loads the Identity service's settings from environment variables.
package config

import "github.com/caarlos0/env/v11"

type Config struct {
	ListenAddr string `env:"LISTEN_ADDR" envDefault:":50052"`
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}
