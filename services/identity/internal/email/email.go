// Package email writes the emails Identity sends and sends them over SMTP. It knows
// nothing about users or tokens beyond an address and a token's value.
package email

import (
	"context"
	"fmt"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// Bounds each step of a send (dialing, every SMTP command), not just the connection.
const timeout = 10 * time.Second

const passwordResetBody = `Someone asked to reset the password of your ride-sharing account. To choose a new one, open this link within an hour:

%s

If it wasn't you, ignore this email. Your password stays as it is.
`

type Sender struct {
	client *gomail.Client
	from   string
	webURL string
}

// New doesn't connect: each send opens its own connection, so one Sender can send from
// many goroutines at once.
func New(host string, port int, from, webURL string) (*Sender, error) {
	client, err := gomail.NewClient(host,
		gomail.WithPort(port),
		// Mailpit speaks plain SMTP with no login. A real mail server needs TLSMandatory
		// and SMTP auth.
		gomail.WithTLSPolicy(gomail.NoTLS),
		gomail.WithTimeout(timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("create smtp client: %w", err)
	}
	return &Sender{client: client, from: from, webURL: webURL}, nil
}

// SendPasswordReset emails a link to the web app's reset page. The token goes in the
// link's fragment, which browsers never send to a server, so it stays out of access
// logs and Referer headers.
func (s *Sender) SendPasswordReset(ctx context.Context, to, token string) error {
	msg, err := s.passwordReset(to, token)
	if err != nil {
		return err
	}
	if err := s.client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("send password reset email: %w", err)
	}
	return nil
}

func (s *Sender) passwordReset(to, token string) (*gomail.Msg, error) {
	msg := gomail.NewMsg()
	if err := msg.From(s.from); err != nil {
		return nil, fmt.Errorf("set sender: %w", err)
	}
	if err := msg.To(to); err != nil {
		return nil, fmt.Errorf("set recipient: %w", err)
	}
	msg.Subject("Reset your password")
	msg.SetBodyString(gomail.TypeTextPlain, fmt.Sprintf(passwordResetBody, s.webURL+"/reset-password#token="+token))
	return msg, nil
}
