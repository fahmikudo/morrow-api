// Command server runs the Morrow API.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"morrow-api/internal/bootstrap"
	"morrow-api/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := bootstrap.NewLogger(cfg)
	ctx := context.Background()

	app, err := bootstrap.New(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() {
		_ = app.Close()
	}()

	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()

	go app.Reminders.Run(schedulerCtx)
	go app.Calendar.Run(schedulerCtx)
	go app.TaskSync.Run(schedulerCtx)

	server := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           app.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	failures := make(chan error, 1)
	go func() {
		log.Info("morrow-api listening", "bind_addr", cfg.BindAddr)
		failures <- server.ListenAndServe()
	}()

	select {
	case <-signalCtx.Done():
		stopScheduler()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return server.Shutdown(shutdownCtx)
	case err := <-failures:
		stopScheduler()

		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
