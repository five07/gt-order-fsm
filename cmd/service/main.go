package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/five07/gt-order-fsm/internal/api"
	"github.com/five07/gt-order-fsm/internal/fulfillment"
	"github.com/five07/gt-order-fsm/internal/order"
	"github.com/five07/gt-order-fsm/internal/payment"
	"github.com/five07/gt-order-fsm/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(log); err != nil {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	svc := order.NewService(
		store.NewMemory(),
		&payment.Stub{},
		&fulfillment.Stub{},
		log,
		time.Now,
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.New(svc, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Info("listening", "addr", addr)
	srv.ListenAndServe()

	// handle the ctx, interrupt && shutdown

	return nil
}
