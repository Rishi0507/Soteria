// Command resolution-service resolves recall signals to GTIN + lot codes.
//
// Env:
//
//	RABBITMQ_URL         amqp URL; when empty the service runs with an in-process
//	                     bus (useful for local UI work without a broker)
//	SHOPIFY_SHOP         myshopify.com domain; set together with SHOPIFY_ACCESS_TOKEN
//	SHOPIFY_ACCESS_TOKEN Admin API token. With both set the catalog comes from the
//	                     live store; otherwise CATALOG_PATH is used
//	CATALOG_REFRESH      snapshot lifetime, e.g. 5m (default 5m)
//	CATALOG_PATH         JSON catalog snapshot for local work (default ./testdata/catalog.json)
//	CORS_ALLOWED_ORIGINS  comma-separated origins allowed to call this API from a
//	                      browser (e.g. http://localhost:5173); empty disables CORS
//	PORT                 HTTP port (default 8081)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/httpmw"
	"soteria/libs/shopify"
	"soteria/services/resolution-service/api"
	"soteria/services/resolution-service/catalog"
	"soteria/services/resolution-service/resolver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cat, err := openCatalog(logger)
	if err != nil {
		logger.Error("cannot open catalog", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		publisher bus.Publisher
		consumer  bus.Consumer
		busUp     = func() bool { return true }
	)
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		amqpBus, err := bus.DialAMQP(url, logger)
		if err != nil {
			logger.Error("cannot connect to rabbitmq", "err", err)
			os.Exit(1)
		}
		defer amqpBus.Close()
		publisher, consumer = amqpBus, amqpBus
	} else {
		logger.Warn("RABBITMQ_URL unset, running with the in-process bus")
		inmem := bus.NewInMem(logger)
		publisher, consumer = inmem, inmem
	}

	store := resolver.NewStore()
	// The badge endpoint needs the catalog's lot ledger to tell a clean lot from
	// one nobody has heard of.
	store.SetLedger(func(ctx context.Context, gtin string) []string {
		p, ok := cat.ByGTIN(ctx, gtin)
		if !ok {
			return nil
		}
		return p.LotCodes
	})
	res := resolver.New(cat, publisher, store, logger)
	if err := res.Register(consumer); err != nil {
		logger.Error("cannot subscribe", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8081"),
		Handler:           httpmw.CORS(os.Getenv("CORS_ALLOWED_ORIGINS"), api.New(store, busUp).Routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("resolution-service listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped", "err", err)
			stop()
		}
	}()
	go func() {
		if err := consumer.Start(ctx); err != nil {
			logger.Error("consumer stopped", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// openCatalog prefers the live store and falls back to a file snapshot. It never
// guesses: a half-configured store is an error, because scoring recalls against a
// stale fixture while believing it is the real catalog is how a recalled product
// stays on sale.
func openCatalog(logger *slog.Logger) (catalog.Catalog, error) {
	shop, token := os.Getenv("SHOPIFY_SHOP"), os.Getenv("SHOPIFY_ACCESS_TOKEN")
	switch {
	case shop != "" && token != "":
		client, err := shopify.New(shop, token)
		if err != nil {
			return nil, fmt.Errorf("shopify client: %w", err)
		}
		refresh := catalog.DefaultRefresh
		if v := os.Getenv("CATALOG_REFRESH"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("CATALOG_REFRESH %q: %w", v, err)
			}
			refresh = d
		}
		logger.Info("catalog source: live store", "shop", shop, "refresh", refresh)
		return catalog.NewStore(client, refresh, logger), nil
	case shop != "" || token != "":
		return nil, fmt.Errorf("SHOPIFY_SHOP and SHOPIFY_ACCESS_TOKEN must be set together")
	default:
		path := env("CATALOG_PATH", "testdata/catalog.json")
		logger.Warn("catalog source: file snapshot, no store configured", "path", path)
		return catalog.LoadFile(path)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
