// Command order-rescue-service turns a containment into a customer choice.
//
// Env:
//
//	RABBITMQ_URL       amqp URL; empty runs with the in-process bus
//	ORDERS_PATH        JSON in-flight order snapshot (default ./testdata/orders.json)
//	SUBSTITUTES_PATH   JSON substitute catalog (default ./testdata/substitutes.json)
//	OFF_MODE           "http" to call Open Food Facts live, "static" (default) to
//	                   use ./testdata/allergens.json
//	ALLERGENS_PATH     static allergen data (default ./testdata/allergens.json)
//	CONSENT_SECRET     HMAC secret for consent tokens (required outside dev)
//	PORT               HTTP port (default 8083)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/offacts"
	"soteria/services/order-rescue-service/api"
	"soteria/services/order-rescue-service/orders"
	"soteria/services/order-rescue-service/rescue"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	repo, err := orders.LoadFile(env("ORDERS_PATH", "testdata/orders.json"))
	if err != nil {
		logger.Error("cannot load orders", "err", err)
		os.Exit(1)
	}
	subs, err := rescue.LoadCatalog(env("SUBSTITUTES_PATH", "testdata/substitutes.json"))
	if err != nil {
		logger.Error("cannot load substitute catalog", "err", err)
		os.Exit(1)
	}

	var allergens offacts.Provider
	if env("OFF_MODE", "static") == "http" {
		allergens = offacts.NewHTTPClient()
	} else {
		allergens, err = loadAllergens(env("ALLERGENS_PATH", "testdata/allergens.json"))
		if err != nil {
			logger.Error("cannot load allergen data", "err", err)
			os.Exit(1)
		}
	}

	secret := os.Getenv("CONSENT_SECRET")
	if secret == "" {
		logger.Warn("CONSENT_SECRET unset, using a development secret - do not run this in production")
		secret = "dev-consent-secret"
	}

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

	svc := rescue.New(repo, subs, allergens, publisher, secret, logger)
	if err := svc.Register(consumer); err != nil {
		logger.Error("cannot subscribe", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8083"),
		Handler:           api.New(svc, nil).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("order-rescue-service listening", "addr", srv.Addr)
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

func loadAllergens(path string) (*offacts.Static, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var products []offacts.Product
	if err := json.Unmarshal(raw, &products); err != nil {
		return nil, err
	}
	return offacts.NewStatic(products...), nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
