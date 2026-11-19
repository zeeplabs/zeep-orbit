// Command zeep-registry-proxy is the only internet path of the zeep-builder
// container. It is a read-only (GET/HEAD) npm registry reverse proxy that
// injects private-registry credentials, so the builder, which runs npm install
// on LLM-generated package.json files, never holds them.
//
// See proxy.go for the environment variables it reads.
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
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ups, err := parseUpstreams(os.Getenv(envUpstreams), os.Getenv)
	if err != nil {
		return fmt.Errorf("%s: %w", envUpstreams, err)
	}
	addr := os.Getenv(envAddr)
	if addr == "" {
		addr = defaultAddr
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newProxy(ups, logger),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Tarballs can be large; this bounds a stalled client, not a big one.
		WriteTimeout:   upstreamTimeout + 30*time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}

	// Names and hosts only: tokens are never logged.
	if ups.def != nil {
		logger.Info("upstream", "name", ups.def.name, "host", ups.def.base.Host, "auth", ups.def.token != "")
	}
	for _, up := range ups.scoped {
		logger.Info("upstream", "name", up.name, "scope", up.scope, "host", up.base.Host, "auth", up.token != "")
	}
	logger.Info("zeep-registry-proxy starting", "addr", addr)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
