// Command apiserver runs the read API. For now it's just a hello-world
// routes with the real /map, /assets, /flows, etc. endpoints backed by the
// graph store.
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

	"apiserver/internal/api"
	"apiserver/internal/capture"
	"apiserver/internal/graph"
)

const addr = ":8080"

func main() {
	// Logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	// Declare a context that can be killed
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Declare the graph store for the whole thing
	store := graph.NewStore()

	// stop everything when this function runs through
	defer stop()

	// use a pointer to the http.Server
	srv := &http.Server{
		Addr:    addr,
		Handler: api.NewMux(store),
	}

	// Run the server
	go func() {
		slog.Info("api server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
		}
	}()

	// Run the capture, decode the traffic and store it in the graph in the background
	go func() {
		slog.Info("Capturing...")
		if err := capture.OngoingCapture(ctx, store); err != nil {
			slog.Error("Capture failed", "err", err)
		}
	}()

	// Handle ShutDown
	<-ctx.Done()
	slog.Info("shutting down")

	// Give in-flight requests a few seconds to finish instead of cutting
	// them off immediately when a shutdown signal arrives.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	slog.Info("shutdown complete")
}
