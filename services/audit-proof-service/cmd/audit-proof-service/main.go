// Command audit-proof-service records every event of an incident in a
// tamper-evident chain and produces the dossier a retailer hands to an insurer
// or a regulator.
//
// Env:
//
//	RABBITMQ_URL          amqp URL; empty runs on the in-process bus
//	TSA_URL               RFC 3161 timestamp authority (default DigiCert's public one)
//	TSA_DISABLED          set to 1 to skip timestamping entirely
//	CORS_ALLOWED_ORIGINS  origins allowed to call this API from a browser
//	PORT                  HTTP port (default 8084)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/httpmw"
	"soteria/services/audit-proof-service/api"
	"soteria/services/audit-proof-service/audit"
	"soteria/services/audit-proof-service/dossier"
	"soteria/services/audit-proof-service/tsa"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

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
		// Without a broker this service records only what is published in this
		// process, which is useful for tests and useless as evidence. Say so.
		logger.Warn("RABBITMQ_URL unset: running on the in-process bus, so the ledger will be empty")
		inmem := bus.NewInMem(logger)
		publisher, consumer = inmem, inmem
	}

	var stamper dossier.Timestamper
	if os.Getenv("TSA_DISABLED") == "1" {
		logger.Warn("timestamping disabled: dossiers will be hash-chained but not anchored in time")
	} else {
		stamper = tsa.New(os.Getenv("TSA_URL"))
	}

	svc := audit.New(publisher, stamper, logger)
	if err := svc.Register(consumer); err != nil {
		logger.Error("cannot subscribe", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8084"),
		Handler:           httpmw.CORS(os.Getenv("CORS_ALLOWED_ORIGINS"), api.New(svc).Routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("audit-proof-service listening", "addr", srv.Addr)
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

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
