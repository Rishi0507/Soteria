// Package publish delivers events to RabbitMQ with publisher confirms and
// automatic reconnection, or prints them in dry-run mode.
package publish

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Message is one event to publish.
type Message struct {
	RoutingKey string
	MessageID  string    // event_id; consumers dedupe on it
	Type       string    // event_type
	AppID      string    // producer
	Timestamp  time.Time // occurred_at
	Body       []byte
}

// Publisher delivers messages. Publish returns nil only once the message is
// durably accepted (broker confirm in the AMQP case).
type Publisher interface {
	Publish(ctx context.Context, m Message) error
	Close() error
}

// ---- dry run ---------------------------------------------------------------

// DryRun writes each message body to w (one JSON document per line).
type DryRun struct {
	mu sync.Mutex
	w  io.Writer
}

// NewDryRun returns a publisher that prints to w (stdout if nil).
func NewDryRun(w io.Writer) *DryRun {
	if w == nil {
		w = os.Stdout
	}
	return &DryRun{w: w}
}

func (d *DryRun) Publish(_ context.Context, m Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := fmt.Fprintf(d.w, "%s\n", m.Body)
	return err
}

func (d *DryRun) Close() error { return nil }

// ---- AMQP ------------------------------------------------------------------

// AMQP publishes to a durable topic exchange with confirms enabled.
//
// Connection loss is detected two ways: a failed Publish reconnects inline,
// and a background watcher reacts to the broker closing the connection so an
// idle service (nothing to publish) still notices an outage — Connected()
// feeds /healthz — and reconnects before the next item arrives.
type AMQP struct {
	url      string
	exchange string
	log      *slog.Logger

	mu     sync.Mutex
	conn   *amqp.Connection
	ch     *amqp.Channel
	gen    uint64 // bumped on every successful connect; identifies which watcher is current
	closed bool   // Close() called; watcher must stop reconnecting

	// MaxAttempts is how many times a single Publish will (re)connect and
	// retry before failing (default 3).
	MaxAttempts int
	// ConfirmTimeout bounds the wait for a broker ack (default 10s).
	ConfirmTimeout time.Duration
}

// NewAMQP dials the broker and declares the exchange. The exchange
// declaration is idempotent so infra-managed topology is left untouched.
func NewAMQP(url, exchange string, log *slog.Logger) (*AMQP, error) {
	if log == nil {
		log = slog.Default()
	}
	p := &AMQP{url: url, exchange: exchange, log: log, MaxAttempts: 3, ConfirmTimeout: 10 * time.Second}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

// Connected reports whether the broker connection is currently up.
func (p *AMQP) Connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.healthy()
}

// connect (re)establishes the connection and a confirm-mode channel and
// starts a watcher for the new connection. Caller holds mu.
func (p *AMQP) connect() error {
	p.teardown()
	conn, err := amqp.DialConfig(p.url, amqp.Config{
		Heartbeat: 10 * time.Second,
		Dial:      amqp.DefaultDial(10 * time.Second),
	})
	if err != nil {
		return fmt.Errorf("amqp: dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("amqp: channel: %w", err)
	}
	if err := ch.ExchangeDeclare(p.exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("amqp: declare exchange %q: %w", p.exchange, err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("amqp: confirm mode: %w", err)
	}
	p.conn, p.ch = conn, ch
	p.gen++
	p.log.Info("amqp connected", "exchange", p.exchange)
	go p.watch(p.gen, conn.NotifyClose(make(chan *amqp.Error, 1)))
	return nil
}

// watch waits for the broker to close the connection of generation gen,
// then reconnects with backoff until it succeeds, Close() is called, or a
// Publish has already established a newer generation (which has its own
// watcher). A failed attempt does not advance the generation, so the loop
// keeps trying through a long broker outage.
func (p *AMQP) watch(gen uint64, closed <-chan *amqp.Error) {
	err, ok := <-closed
	if !ok || err == nil {
		return // closed by us
	}
	p.log.Warn("amqp connection lost", "err", err)
	for attempt := 1; ; attempt++ {
		p.mu.Lock()
		if p.closed || p.gen != gen {
			p.mu.Unlock()
			return
		}
		cerr := p.connect()
		p.mu.Unlock()
		if cerr == nil {
			return
		}
		p.log.Warn("amqp background reconnect failed", "attempt", attempt, "err", cerr)
		time.Sleep(backoff(attempt))
	}
}

func (p *AMQP) teardown() {
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}

func (p *AMQP) healthy() bool {
	return p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed()
}

// Publish sends m and waits for the broker confirm, reconnecting with
// backoff on failure.
func (p *AMQP) Publish(ctx context.Context, m Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if !p.healthy() {
			if err := p.connect(); err != nil {
				lastErr = err
				p.log.Warn("amqp reconnect failed", "attempt", attempt, "err", err)
				if err := wait(ctx, backoff(attempt)); err != nil {
					return err
				}
				continue
			}
		}
		err := p.publishOnce(ctx, m)
		if err == nil {
			return nil
		}
		lastErr = err
		p.log.Warn("amqp publish failed", "attempt", attempt, "message_id", m.MessageID, "err", err)
		p.teardown()
		if err := wait(ctx, backoff(attempt)); err != nil {
			return err
		}
	}
	return fmt.Errorf("amqp: publish %s failed after %d attempts: %w", m.MessageID, p.MaxAttempts, lastErr)
}

func (p *AMQP) publishOnce(ctx context.Context, m Message) error {
	cctx, cancel := context.WithTimeout(ctx, p.ConfirmTimeout)
	defer cancel()
	conf, err := p.ch.PublishWithDeferredConfirmWithContext(cctx, p.exchange, m.RoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    m.MessageID,
		Type:         m.Type,
		AppId:        m.AppID,
		Timestamp:    m.Timestamp,
		Body:         m.Body,
	})
	if err != nil {
		return err
	}
	acked, err := conf.WaitContext(cctx)
	if err != nil {
		return fmt.Errorf("confirm wait: %w", err)
	}
	if !acked {
		return errors.New("broker nacked message")
	}
	return nil
}

// Close closes the channel and connection.
func (p *AMQP) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.teardown()
	return nil
}

func backoff(attempt int) time.Duration {
	d := time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
