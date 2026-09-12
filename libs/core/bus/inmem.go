package bus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"soteria/libs/core/events"
)

// InMem is a topic-exchange-shaped in-process bus. Delivery is synchronous and
// ordered, which is what makes the end-to-end test deterministic; the routing rules
// (exchange + exact binding key) are the same ones RabbitMQ applies.
type InMem struct {
	mu          sync.RWMutex
	subs        []inmemSub
	published   []events.Envelope
	deadLetters []DeadLetter
	logger      *slog.Logger
}

type inmemSub struct {
	sub Subscription
	h   Handler
}

// DeadLetter is what would land on <queue>.dlq in RabbitMQ.
type DeadLetter struct {
	Queue    string
	Envelope events.Envelope
	Err      error
}

func NewInMem(logger *slog.Logger) *InMem {
	if logger == nil {
		logger = slog.Default()
	}
	return &InMem{logger: logger}
}

func (b *InMem) Subscribe(sub Subscription, h Handler) error {
	if sub.Queue == "" || sub.Exchange == "" || len(sub.BindingKeys) == 0 {
		return fmt.Errorf("bus: subscription needs a queue, an exchange and at least one binding key")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, inmemSub{sub: sub, h: h})
	return nil
}

// Start is a no-op: InMem delivers inline on Publish.
func (b *InMem) Start(ctx context.Context) error { return nil }

func (b *InMem) Publish(ctx context.Context, env events.Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	exchange, err := events.ExchangeFor(env.EventType)
	if err != nil {
		return err
	}

	b.mu.Lock()
	b.published = append(b.published, env)
	targets := make([]inmemSub, 0, len(b.subs))
	for _, s := range b.subs {
		if s.sub.Exchange != exchange {
			continue
		}
		if matches(s.sub.BindingKeys, env.EventType) {
			targets = append(targets, s)
		}
	}
	b.mu.Unlock()

	for _, t := range targets {
		if err := t.h(ctx, env); err != nil {
			b.logger.Error("handler failed, dead-lettering",
				"queue", t.sub.Queue, "event_type", env.EventType, "event_id", env.EventID, "err", err)
			b.mu.Lock()
			b.deadLetters = append(b.deadLetters, DeadLetter{Queue: t.sub.Queue, Envelope: env, Err: err})
			b.mu.Unlock()
		}
	}
	return nil
}

func (b *InMem) Close() error { return nil }

// Published returns every envelope published so far, in order (test helper).
func (b *InMem) Published() []events.Envelope {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]events.Envelope, len(b.published))
	copy(out, b.published)
	return out
}

// PublishedOfType filters Published by event type (test helper).
func (b *InMem) PublishedOfType(eventType string) []events.Envelope {
	var out []events.Envelope
	for _, e := range b.Published() {
		if e.EventType == eventType {
			out = append(out, e)
		}
	}
	return out
}

// DeadLetters returns handler failures (test helper).
func (b *InMem) DeadLetters() []DeadLetter {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]DeadLetter, len(b.deadLetters))
	copy(out, b.deadLetters)
	return out
}

// matches implements AMQP topic matching: "*" matches exactly one word, "#"
// matches zero or more. Keeping the same semantics as the broker is what lets the
// end-to-end tests trust their bindings.
func matches(bindingKeys []string, routingKey string) bool {
	words := strings.Split(routingKey, ".")
	for _, k := range bindingKeys {
		if topicMatch(strings.Split(k, "."), words) {
			return true
		}
	}
	return false
}

func topicMatch(pattern, words []string) bool {
	if len(pattern) == 0 {
		return len(words) == 0
	}
	switch pattern[0] {
	case "#":
		for i := 0; i <= len(words); i++ {
			if topicMatch(pattern[1:], words[i:]) {
				return true
			}
		}
		return false
	case "*":
		return len(words) > 0 && topicMatch(pattern[1:], words[1:])
	default:
		return len(words) > 0 && pattern[0] == words[0] && topicMatch(pattern[1:], words[1:])
	}
}
