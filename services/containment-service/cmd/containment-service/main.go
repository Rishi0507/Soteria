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
//	PORT            HTTP port (default 8082)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/shopify"
	"soteria/services/containment-service/api"
	"soteria/services/containment-service/containment"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	inv, err := loadInventory(env("INVENTORY_PATH", "testdata/inventory.json"))
	if err != nil {
		logger.Error("cannot load inventory snapshot", "err", err)
		os.Exit(1)
	}

	cfg := containment.DefaultConfig()
	cfg.AutoHoldThreshold = envFloat("AUTO_HOLD_THRESHOLD", cfg.AutoHoldThreshold)
	cfg.SKUScopeThreshold = envFloat("SKU_SCOPE_THRESHOLD", cfg.SKUScopeThreshold)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		Handler:           api.New(svc, nil).Routes(),
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

func loadInventory(path string) (*shopify.Fake, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lots []shopify.InventoryLot
	if err := json.Unmarshal(raw, &lots); err != nil {
		return nil, err
	}
	return shopify.NewFake(lots...), nil
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
