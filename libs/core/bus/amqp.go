package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"soteria/libs/core/events"
)

// MaxRetries is the delivery budget for one message, mirroring
// /contracts/rabbitmq-topology.md. It is enforced by the broker through
// x-delivery-limit on quorum queues rather than by counting here: a consumer
// that tracked attempts in memory would forget them on restart, and a poison
// message would then retry forever.
const MaxRetries = 5

// AMQP is the production bus. It declares the exchanges it publishes to and the
// queues/DLQs its owner service consumes from, all idempotently on boot.
type AMQP struct {
	conn    *amqp.Connection
	pubCh   *amqp.Channel
	pubMu   sync.Mutex
	logger  *slog.Logger
	subs    []amqpSub
	closers []func() error
}

type amqpSub struct {
	sub Subscription
	h   Handler
}

func DialAMQP(url string, logger *slog.Logger) (*AMQP, error) {
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("bus: dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("bus: open publish channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("bus: enable publisher confirms: %w", err)
	}
	b := &AMQP{conn: conn, pubCh: ch, logger: logger}
	for _, x := range []string{
		events.ExchangeIngestion, events.ExchangeResolution, events.ExchangeContainment,
		events.ExchangeRescue, events.ExchangeEvasion, events.ExchangeAudit, events.ExchangeNotification,
	} {
		if err := ch.ExchangeDeclare(x, "topic", true, false, false, false, nil); err != nil {
			b.Close()
			return nil, fmt.Errorf("bus: declare exchange %s: %w", x, err)
		}
		dlx := dlxFor(x)
		if err := ch.ExchangeDeclare(dlx, "topic", true, false, false, false, nil); err != nil {
			b.Close()
			return nil, fmt.Errorf("bus: declare dlx %s: %w", dlx, err)
		}
	}
	return b, nil
}

func dlxFor(exchange string) string {
	return strings.TrimSuffix(exchange, ".x") + ".dlx"
}

// dlxForQueue derives a consumer's dead-letter exchange from its queue prefix:
// resolution.recall-raw -> resolution.dlx.
func dlxForQueue(queue string) string {
	if i := strings.Index(queue, "."); i > 0 {
		return queue[:i] + ".dlx"
	}
	return queue + ".dlx"
}

func (b *AMQP) Publish(ctx context.Context, env events.Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	exchange, err := events.ExchangeFor(env.EventType)
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("bus: marshal envelope: %w", err)
	}

	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	conf, err := b.pubCh.PublishWithDeferredConfirmWithContext(ctx, exchange, env.EventType, true, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     env.EventID,
		Type:          env.EventType,
		Timestamp:     env.OccurredAt,
		CorrelationId: env.CorrelationID,
		AppId:         env.Producer,
		Body:          body,
	})
	if err != nil {
		return fmt.Errorf("bus: publish %s: %w", env.EventType, err)
	}
	ok, err := conf.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("bus: await confirm for %s: %w", env.EventType, err)
	}
	if !ok {
		return fmt.Errorf("bus: broker nacked %s (%s)", env.EventType, env.EventID)
	}
	return nil
}

// Subscribe declares the queue, its DLQ and the bindings, then registers the handler.
func (b *AMQP) Subscribe(sub Subscription, h Handler) error {
	if sub.Queue == "" || sub.Exchange == "" || len(sub.BindingKeys) == 0 {
		return fmt.Errorf("bus: subscription needs a queue, an exchange and at least one binding key")
	}
	ch, err := b.conn.Channel()
	if err != nil {
		return fmt.Errorf("bus: open channel for %s: %w", sub.Queue, err)
	}
	// The dead-letter exchange belongs to the consumer, not to the producer whose
	// exchange the queue happens to be bound to: a handler failure here is this
	// service's problem to replay.
	dlx := dlxForQueue(sub.Queue)
	dlq := sub.Queue + ".dlq"
	if err := ch.ExchangeDeclare(dlx, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("bus: declare dlx %s: %w", dlx, err)
	}
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("bus: declare dlq %s: %w", dlq, err)
	}
	if err := ch.QueueBind(dlq, "#", dlx, false, nil); err != nil {
		return fmt.Errorf("bus: bind dlq %s: %w", dlq, err)
	}
	// Quorum queues count deliveries themselves and dead-letter once the limit
	// is reached. Classic queues have no such counter: rejecting with requeue
	// leaves x-death unset, so the same message comes straight back and a
	// handler that always fails spins forever.
	if _, err := ch.QueueDeclare(sub.Queue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": dlx,
		"x-queue-type":           "quorum",
		"x-delivery-limit":       int32(MaxRetries),
	}); err != nil {
		return fmt.Errorf("bus: declare queue %s: %w", sub.Queue, err)
	}
	for _, k := range sub.BindingKeys {
		if err := ch.QueueBind(sub.Queue, k, sub.Exchange, false, nil); err != nil {
			return fmt.Errorf("bus: bind %s -> %s (%s): %w", sub.Queue, sub.Exchange, k, err)
		}
	}
	if err := ch.Qos(16, 0, false); err != nil {
		return fmt.Errorf("bus: qos for %s: %w", sub.Queue, err)
	}
	b.subs = append(b.subs, amqpSub{sub: sub, h: h})
	b.closers = append(b.closers, ch.Close)

	deliveries, err := ch.Consume(sub.Queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("bus: consume %s: %w", sub.Queue, err)
	}
	go b.pump(sub, h, deliveries)
	return nil
}

func (b *AMQP) pump(sub Subscription, h Handler, deliveries <-chan amqp.Delivery) {
	for d := range deliveries {
		env, err := events.InboundEnvelope(d.RoutingKey, d.Body)
		if err != nil {
			b.logger.Error("undecodable message, dead-lettering", "queue", sub.Queue, "err", err)
			_ = d.Reject(false)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = h(ctx, env)
		cancel()
		if err == nil {
			_ = d.Ack(false)
			continue
		}
		// Requeue and let the broker decide when enough is enough: after
		// x-delivery-limit attempts it dead-letters the message for us, so a
		// message is always either handled or somewhere we can find it.
		b.logger.Error("handler failed",
			"queue", sub.Queue, "event_type", env.EventType, "event_id", env.EventID,
			"deliveries", deliveryCount(d.Headers), "limit", MaxRetries, "err", err)
		_ = d.Reject(true)
	}
}

// deliveryCount reports how many times the broker has delivered this message,
// for logging only. Quorum queues set x-delivery-count; the value is absent on
// the first delivery and on classic queues.
func deliveryCount(h amqp.Table) int {
	switch v := h["x-delivery-count"].(type) {
	case int64:
		return int(v) + 1
	case int32:
		return int(v) + 1
	case int:
		return v + 1
	default:
		return 1
	}
}

// Start blocks until ctx is done; consumers are already pumping from Subscribe.
func (b *AMQP) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (b *AMQP) Close() error {
	for _, c := range b.closers {
		_ = c()
	}
	if b.pubCh != nil {
		_ = b.pubCh.Close()
	}
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}
