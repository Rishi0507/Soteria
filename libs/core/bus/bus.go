// Package bus is the messaging seam every Soteria service publishes and consumes through.
//
// Two implementations ship: amqp (RabbitMQ, production) and inmem (tests, and the
// end-to-end demo). Services depend only on the interfaces here, so the whole
// resolution -> containment -> rescue chain is exercisable without a broker.
package bus

import (
	"context"

	"soteria/libs/core/events"
)

// Publisher publishes an envelope to the exchange its event type maps to.
type Publisher interface {
	Publish(ctx context.Context, env events.Envelope) error
	Close() error
}

// Handler processes one delivery. Returning an error nacks the message, which
// (after the retry budget in /contracts/rabbitmq-topology.md) dead-letters it.
type Handler func(ctx context.Context, env events.Envelope) error

// Subscription describes one queue and its bindings. Queues are declared by their
// owning service on boot so a deploy never needs an infra change.
type Subscription struct {
	Queue       string
	Exchange    string
	BindingKeys []string
}

// Consumer binds queues and dispatches deliveries to handlers.
type Consumer interface {
	Subscribe(sub Subscription, h Handler) error
	Start(ctx context.Context) error
	Close() error
}
