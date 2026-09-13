// Package notify turns bus events into outbound messages, delivers them
// through channels (Slack, email, SMS), and records every attempt so the
// audit dossier can prove who was told what, and when.
package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// Channel names.
const (
	ChannelSlack = "slack"
	ChannelEmail = "email"
	ChannelSMS   = "sms"
)

// Message is one outbound notification, channel-agnostic.
type Message struct {
	Channel   string
	Recipient string // email address, phone number, or a logical name for a webhook ("ops")
	Subject   string
	Text      string // plain text / Slack mrkdwn
	HTML      string // email only, optional
}

// Channel delivers messages. Send returns a provider reference (message id)
// on success. Wrap non-retryable failures with Permanent.
type Channel interface {
	Name() string
	Send(ctx context.Context, m Message) (ref string, err error)
}

// PermanentError marks a failure that retrying cannot fix (bad recipient,
// rejected payload, missing credentials).
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return "permanent: " + e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent wraps err as non-retryable.
func Permanent(err error) error { return &PermanentError{Err: err} }

// IsPermanent reports whether err is non-retryable.
func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// ---- log channel (default when no provider is configured) ---------------

// LogChannel prints messages instead of sending them.
type LogChannel struct {
	name string
	w    io.Writer
	log  *slog.Logger
}

// NewLogChannel returns a channel that writes to w (stdout if nil).
func NewLogChannel(name string, w io.Writer, log *slog.Logger) *LogChannel {
	if w == nil {
		w = os.Stdout
	}
	if log == nil {
		log = slog.Default()
	}
	return &LogChannel{name: name, w: w, log: log}
}

func (c *LogChannel) Name() string { return c.name }

func (c *LogChannel) Send(_ context.Context, m Message) (string, error) {
	fmt.Fprintf(c.w, "---- [%s → %s] %s\n%s\n", m.Channel, m.Recipient, m.Subject, m.Text)
	c.log.Warn("notification printed, not delivered (no provider configured)", "channel", m.Channel, "recipient", Mask(m.Recipient))
	return "log:" + m.Channel, nil
}

// ---- fake channel (tests) ------------------------------------------------

// FakeChannel records sends and can fail on demand.
type FakeChannel struct {
	name string
	mu   sync.Mutex
	Sent []Message
	// Fail returns an error for the next call(s); nil entries mean success.
	Fail []error
}

// NewFake returns a fake channel.
func NewFake(name string) *FakeChannel { return &FakeChannel{name: name} }

func (f *FakeChannel) Name() string { return f.name }

func (f *FakeChannel) Send(_ context.Context, m Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Fail) > 0 {
		err := f.Fail[0]
		f.Fail = f.Fail[1:]
		if err != nil {
			return "", err
		}
	}
	f.Sent = append(f.Sent, m)
	return fmt.Sprintf("fake:%s:%d", f.name, len(f.Sent)), nil
}

// Mask hides most of an email address or phone number for logs.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	if at := indexByte(s, '@'); at > 0 {
		local := s[:at]
		if len(local) > 2 {
			local = local[:2] + "***"
		} else {
			local = "***"
		}
		return local + s[at:]
	}
	if len(s) > 4 {
		return "***" + s[len(s)-4:]
	}
	return "***"
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
