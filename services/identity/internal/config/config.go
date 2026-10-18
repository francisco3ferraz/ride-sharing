// Package config loads the Identity service's settings from environment variables.
package config

import "github.com/caarlos0/env/v11"

type Config struct {
	ListenAddr     string `env:"LISTEN_ADDR" envDefault:":50052"`
	HTTPListenAddr string `env:"HTTP_LISTEN_ADDR" envDefault:":8080"`
	JWTKeyPath     string `env:"JWT_KEY_PATH" envDefault:"/etc/jwt-signing-key/signing-key.pem"`
	// The SMTP Service of the Mailpit chart the Tiltfile installs.
	SMTPHost string `env:"SMTP_HOST" envDefault:"mailpit-smtp.mailpit.svc"`
	SMTPPort int    `env:"SMTP_PORT" envDefault:"25"`
	MailFrom string `env:"MAIL_FROM" envDefault:"Ride-sharing <no-reply@ride-sharing.localhost>"`
	// Where links in emails point: the web app, with no trailing slash.
	WebURL string `env:"WEB_URL" envDefault:"https://ride-sharing.localhost"`
}

func Load() (Config, error) {
	return env.ParseAs[Config]()
}
