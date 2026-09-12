// Package app is the shared bootstrap for ingestion services: config from
// env + flags, the dedup store, the publisher (AMQP or dry-run), one poller
// per source, and the health HTTP server, with graceful shutdown.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"soteria/libs/feedkit/dedup"
	"soteria/libs/feedkit/event"
	"soteria/libs/feedkit/health"
	"soteria/libs/feedkit/httpx"
	"soteria/libs/feedkit/poller"
	"soteria/libs/feedkit/publish"
	"soteria/libs/feedkit/source"
)

// Config is the resolved runtime configuration.
type Config struct {
	Producer     string
	RabbitURL    string
	DBPath       string
	HTTPAddr     string
	Backfill     time.Duration
	Interval     time.Duration // default cadence; a SourceSpec may override
	Once         bool
	DryRun       bool
	Contact      string // goes into the User-Agent
	Log          *slog.Logger
	HTTP         *httpx.Client
	// Env returns a raw environment value (after .env loading) for
	// service-specific settings such as OPENFDA_API_KEY or RASFF_MODE.
	Env func(key, def string) string
}

// SourceSpec is one feed to poll and its cadence (0 → Config.Interval).
type SourceSpec struct {
	Source   source.Source
	Interval time.Duration
}

// Builder constructs the service's sources from the resolved config.
type Builder func(cfg Config) ([]SourceSpec, error)

// Main parses config, wires everything and blocks until shutdown. It exits
// the process with a non-zero status on fatal errors or a failed --once run.
func Main(producer string, build Builder) {
	if err := run(producer, build); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run(producer string, build Builder) error {
	_ = godotenv.Load() // optional; real deployments use the environment
	env := func(key, def string) string {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			return v
		}
		return def
	}

	fs := flag.NewFlagSet(producer, flag.ExitOnError)
	once := fs.Bool("once", false, "run a single poll of every source and exit")
	dry := fs.Bool("dry-run", false, "print events as JSON lines instead of publishing; nothing is marked seen")
	backfillDays := fs.Int("backfill-days", envInt(env, "BACKFILL_DAYS", 30), "how far back to look on the first run")
	interval := fs.Duration("interval", envDuration(env, "POLL_INTERVAL", 5*time.Minute), "default poll cadence")
	dbPath := fs.String("db", env("DB_PATH", filepath.Join("data", producer+".db")), "SQLite dedup store path")
	httpAddr := fs.String("http", env("HTTP_ADDR", ":8080"), "health/metrics listen address")
	rabbit := fs.String("rabbitmq", env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"), "RabbitMQ URL")
	logLevel := fs.String("log-level", env("LOG_LEVEL", "info"), "debug|info|warn|error")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	log := newLogger(*logLevel, producer)
	contact := env("CONTACT_EMAIL", "ops@soteria.dev")
	cfg := Config{
		Producer:  producer,
		RabbitURL: *rabbit,
		DBPath:    *dbPath,
		HTTPAddr:  *httpAddr,
		Backfill:  time.Duration(*backfillDays) * 24 * time.Hour,
		Interval:  *interval,
		Once:      *once,
		DryRun:    *dry,
		Contact:   contact,
		Log:       log,
		HTTP:      httpx.New(fmt.Sprintf("Soteria-Ingestion/%s (+%s)", producer, contact)),
		Env:       env,
	}

	specs, err := build(cfg)
	if err != nil {
		return fmt.Errorf("build sources: %w", err)
	}
	if len(specs) == 0 {
		return errors.New("no sources configured")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := dedup.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	var pub publish.Publisher
	if cfg.DryRun {
		pub = publish.NewDryRun(os.Stdout)
		// Dry-run must not advance the dedup store, so use a throwaway one.
		store.Close()
		if store, err = dedup.Open(":memory:"); err != nil {
			return err
		}
		log.Info("dry-run: printing events, not publishing, not marking seen")
	} else {
		if pub, err = publish.NewAMQP(cfg.RabbitURL, event.Exchange, log); err != nil {
			return err
		}
	}
	defer pub.Close()

	tracker := health.New(producer)
	pollers := make([]*poller.Poller, 0, len(specs))
	for _, s := range specs {
		iv := s.Interval
		if iv <= 0 {
			iv = cfg.Interval
		}
		pollers = append(pollers, poller.New(poller.Config{
			Producer: producer,
			Interval: iv,
			Backfill: cfg.Backfill,
			Once:     cfg.Once,
		}, s.Source, store, pub, tracker, log))
		log.Info("source registered", "source", s.Source.Name(), "interval", iv)
	}
	tracker.SetReady(true)

	// Health server only makes sense for a long-running process.
	var srv *http.Server
	if !cfg.Once {
		srv = &http.Server{Addr: cfg.HTTPAddr, Handler: tracker.Handler(), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			log.Info("health server listening", "addr", cfg.HTTPAddr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("health server failed", "err", err)
			}
		}()
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(pollers))
	for _, p := range pollers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- p.Run(ctx)
		}()
	}
	wg.Wait()
	close(errs)

	if srv != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}

	var failed []error
	for err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			failed = append(failed, err)
		}
	}
	if cfg.Once && len(failed) > 0 {
		return fmt.Errorf("once: %d source(s) failed: %w", len(failed), errors.Join(failed...))
	}
	return nil
}

func newLogger(level, producer string) *slog.Logger {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(level)); err != nil {
		lv = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	return slog.New(h).With("service", producer)
}

func envInt(env func(string, string) string, key string, def int) int {
	if n, err := strconv.Atoi(env(key, "")); err == nil {
		return n
	}
	return def
}

func envDuration(env func(string, string) string, key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(env(key, "")); err == nil {
		return d
	}
	return def
}
