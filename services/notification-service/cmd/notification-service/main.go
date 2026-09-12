// notification-service turns containment, rescue and evasion events into
// Slack alerts for the retailer and email/SMS for affected customers, and
// records every delivery for the audit dossier.
//
// Environment:
//
//	RABBITMQ_URL        amqp URL; empty runs on the in-process bus (use --replay)
//	SLACK_WEBHOOK_URL   incoming webhook for the retailer ops channel
//	RESEND_API_KEY      https://resend.com API key for customer email
//	EMAIL_FROM          sender, e.g. "Sotería Alerts <alerts@example.com>" (default onboarding@resend.dev)
//	RESCUE_CONSENT_URL  storefront consent page, "{rescue_id}" substituted
//	                    (default http://localhost:5173/rescue/{rescue_id})
//	RETAILER_NAME       appears in customer copy (default "Your grocer")
//	DB_PATH             delivery ledger (default data/notification.db)
//	PORT                HTTP port for /healthz /metrics /v1/... (default 8084)
//	LOG_LEVEL           debug|info|warn|error
//
// Without SLACK_WEBHOOK_URL / RESEND_API_KEY the corresponding channel prints
// to stdout instead of sending, and says so loudly.
//
// Flags:
//
//	--replay <file>   publish the envelopes in a JSON file (array) into the
//	                  bus and exit; the quickest way to see real Slack/email
//	                  output without running the whole chain.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/services/notification-service/internal/notify"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()
	_ = godotenv.Load(filepath.Join("..", "..", ".env")) // repo-root .env when run from the service dir
	replay := flag.String("replay", "", "JSON file of envelopes to publish into the bus, then exit")
	flag.Parse()

	log := newLogger(env("LOG_LEVEL", "info"))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ledger, err := notify.OpenLedger(env("DB_PATH", filepath.Join("data", "notification.db")))
	if err != nil {
		return err
	}
	defer ledger.Close()

	channels := map[string]notify.Channel{}
	if u := os.Getenv("SLACK_WEBHOOK_URL"); u != "" {
		channels[notify.ChannelSlack] = notify.NewSlack(u)
	} else {
		channels[notify.ChannelSlack] = notify.NewLogChannel("slack-log", os.Stdout, log)
		log.Warn("SLACK_WEBHOOK_URL not set: Slack alerts will be printed, not sent")
	}
	if k := os.Getenv("RESEND_API_KEY"); k != "" {
		channels[notify.ChannelEmail] = notify.NewResend(k, env("EMAIL_FROM", "Sotería Alerts <onboarding@resend.dev>"))
	} else {
		channels[notify.ChannelEmail] = notify.NewLogChannel("email-log", os.Stdout, log)
		log.Warn("RESEND_API_KEY not set: customer emails will be printed, not sent")
	}
	// SMS has no provider yet (Twilio is a paid service); print so the flow is visible.
	channels[notify.ChannelSMS] = notify.NewLogChannel("sms-log", os.Stdout, log)

	router := &notify.Router{
		OpsRecipient: "ops",
		ConsentURL:   env("RESCUE_CONSENT_URL", "http://localhost:5173/rescue/{rescue_id}"),
		RetailerName: env("RETAILER_NAME", ""),
	}

	var (
		consumer  bus.Consumer
		publisher bus.Publisher
		out       notify.DeliveredPublisher
		broker    func() bool
	)
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		amqpBus, err := bus.DialAMQP(url, log)
		if err != nil {
			return err
		}
		defer amqpBus.Close()
		consumer, publisher = amqpBus, amqpBus
		amqpOut, err := notify.NewAMQPOut(url, log)
		if err != nil {
			return err
		}
		defer amqpOut.Close()
		out, broker = amqpOut, amqpOut.Connected
		log.Info("bus: rabbitmq", "url", redact(url))
	} else {
		mem := bus.NewInMem(log)
		consumer, publisher = mem, mem
		out = &notify.MemOut{}
		log.Warn("RABBITMQ_URL not set: running on the in-process bus; only --replay can feed events")
	}

	svc := notify.New(router, ledger, channels, out, log)
	for _, sub := range notify.Subscriptions() {
		if err := consumer.Subscribe(sub, svc.Handle); err != nil {
			return err
		}
	}

	if *replay != "" {
		return doReplay(ctx, *replay, publisher, log)
	}

	srv := &http.Server{Addr: ":" + env("PORT", "8084"), Handler: svc.Handler(broker), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("http listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "err", err)
		}
	}()
	err = consumer.Start(ctx)
	if err == nil {
		<-ctx.Done() // the in-process bus's Start is a no-op; stay up until signalled
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	return err
}

func doReplay(ctx context.Context, path string, pub bus.Publisher, log *slog.Logger) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var envs []events.Envelope
	if err := json.Unmarshal(data, &envs); err != nil {
		return fmt.Errorf("replay: %s: %w", path, err)
	}
	for i := range envs {
		if envs[i].EventID == "" {
			envs[i].EventID = events.NewID()
		}
		if envs[i].OccurredAt.IsZero() {
			envs[i].OccurredAt = time.Now().UTC()
		}
		if envs[i].EventVersion == 0 {
			envs[i].EventVersion = 1
		}
		if err := pub.Publish(ctx, envs[i]); err != nil {
			return fmt.Errorf("replay: publish %s: %w", envs[i].EventType, err)
		}
		log.Info("replayed", "event_type", envs[i].EventType, "event_id", envs[i].EventID)
	}
	// On AMQP the handlers run asynchronously; give them a moment before exit.
	if _, ok := pub.(*bus.AMQP); ok {
		time.Sleep(3 * time.Second)
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(level)); err != nil {
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lv})).With("service", notify.Producer)
}

func redact(url string) string {
	if i := indexOf(url, "@"); i > 0 {
		return "amqp://***@" + url[i+1:]
	}
	return url
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
