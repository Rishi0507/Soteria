// Command containment-service decides auto-hold vs. human review and writes the
// inventory hold.
//
// Env:
//
//	RABBITMQ_URL    amqp URL; empty runs with the in-process bus
//	INVENTORY_PATH  JSON lot-level inventory snapshot backing the Shopify fake
//	                (default ./testdata/inventory.json). Replaced by the shared
//	                Shopify client library once it lands, see pkg/shopify.
//	AUTO_HOLD_THRESHOLD / SKU_SCOPE_THRESHOLD  initial thresholds
//	CORS_ALLOWED_ORIGINS  comma-separated origins allowed to call this API from a
//	                      browser (e.g. http://localhost:5173); empty disables CORS
//	PORT            HTTP port (default 8082)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/httpmw"
	coreshopify "soteria/libs/core/shopify"
	"soteria/libs/shopify"
	"soteria/libs/shopify/hold"
	"soteria/services/containment-service/api"
	"soteria/services/containment-service/containment"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inv, err := openInventory(ctx, logger)
	if err != nil {
		logger.Error("cannot open inventory", "err", err)
		os.Exit(1)
	}

	cfg := containment.DefaultConfig()
	cfg.AutoHoldThreshold = envFloat("AUTO_HOLD_THRESHOLD", cfg.AutoHoldThreshold)
	cfg.SKUScopeThreshold = envFloat("SKU_SCOPE_THRESHOLD", cfg.SKUScopeThreshold)

	var (
		publisher bus.Publisher
		consumer  bus.Consumer
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

	svc := containment.New(containment.NewStore(cfg), inv, publisher, logger)
	if err := svc.Register(consumer); err != nil {
		logger.Error("cannot subscribe", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8082"),
		Handler:           httpmw.CORS(os.Getenv("CORS_ALLOWED_ORIGINS"), api.New(svc, nil).Routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("containment-service listening", "addr", srv.Addr,
			"auto_hold_threshold", cfg.AutoHoldThreshold, "sku_scope_threshold", cfg.SKUScopeThreshold)
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

// openInventory returns the live store when it is configured, and the in-memory
// fake otherwise.
//
// The distinction has to be loud: every hold this service performs is published as
// containment.action.taken.v1 and lands in the audit dossier, so running against
// the fake while believing the store is wired would put holds on the record that
// never happened. A half-configured store is therefore a startup failure, not a
// silent downgrade.
func openInventory(ctx context.Context, logger *slog.Logger) (coreshopify.InventoryClient, error) {
	shop, token := os.Getenv("SHOPIFY_SHOP"), os.Getenv("SHOPIFY_ACCESS_TOKEN")
	switch {
	case shop != "" && token != "":
		client, err := shopify.New(shop, token)
		if err != nil {
			return nil, fmt.Errorf("shopify client: %w", err)
		}
		quarantine := env("QUARANTINE_LOCATION", "Quarantine")
		selling, quarantined, err := hold.ResolveLocations(ctx, client, quarantine)
		if err != nil {
			return nil, fmt.Errorf("resolve store locations (need a %q location): %w", quarantine, err)
		}
		logger.Info("inventory target: live store",
			"shop", shop, "selling_location", selling, "quarantine_location", quarantined)
		return hold.New(client, selling, quarantined), nil
	case shop != "" || token != "":
		return nil, fmt.Errorf("SHOPIFY_SHOP and SHOPIFY_ACCESS_TOKEN must be set together")
	default:
		path := env("INVENTORY_PATH", "testdata/inventory.json")
		logger.Warn("inventory target: in-memory fake, no store configured. Holds are not real", "path", path)
		return loadInventory(path)
	}
}

func loadInventory(path string) (*coreshopify.Fake, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lots []coreshopify.InventoryLot
	if err := json.Unmarshal(raw, &lots); err != nil {
		return nil, err
	}
	return coreshopify.NewFake(lots...), nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
