package main

import (
	"context"
	"errors"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/httpapi"
	"github.com/hloiseau/le-concierge/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if e := run(); e != nil {
		slog.Error("API stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		r, e := client.Get("http://127.0.0.1:8080/readyz")
		if e != nil {
			return errors.New("unhealthy")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return errors.New("unhealthy")
		}
		return nil
	}
	c, e := config.Load()
	if e != nil {
		return e
	}
	db, e := store.Open(c.DatabasePath)
	if e != nil {
		return errors.New("cannot open database")
	}
	defer db.Close()
	api := httpapi.New(c, db)
	if err := api.RestorePlexAuthorizations(context.Background()); err != nil {
		return err
	}
	server := &http.Server{Addr: c.Listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var workers sync.WaitGroup
	for _, runWorker := range []func(context.Context){api.RunReminders, api.RunAvailability, api.RunMonitoring, api.RunCommunity} {
		workers.Add(1)
		go func() { defer workers.Done(); runWorker(ctx) }()
	}
	defer func() { stop(); workers.Wait() }()
	done := make(chan error, 1)
	go func() { slog.Info("API listening", "address", c.Listen); done <- server.ListenAndServe() }()
	select {
	case e := <-done:
		if !errors.Is(e, http.ErrServerClosed) {
			return e
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
	return nil
}
