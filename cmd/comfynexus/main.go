// Command comfynexus is the gateway entry point.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Youzini-afk/ComfyNexus/internal/config"
	"github.com/Youzini-afk/ComfyNexus/internal/db"
	"github.com/Youzini-afk/ComfyNexus/internal/logging"
	"github.com/Youzini-afk/ComfyNexus/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// We use stdlib log here because logging.New depends on cfg.
		os.Stderr.WriteString("config error: " + err.Error() + "\n")
		os.Exit(1)
	}
	log := logging.New(cfg.LogLevel)
	log.Info("starting ComfyNexus",
		"bind", cfg.Bind, "data_dir", cfg.DataDir, "trust_proxy", cfg.TrustProxy)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d, err := db.Open(ctx, cfg.SQLitePath())
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	srv := server.New(cfg, d, log)
	httpSrv := &http.Server{
		Addr:              cfg.Bind,
		Handler:           srv.Router,
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: WebSocket and long file streams need to live.
		IdleTimeout: 120 * time.Second,
	}

	go func() {
		log.Info("http listening", "addr", cfg.Bind)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			cancel()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
		log.Info("shutdown signal received")
	case <-ctx.Done():
	}
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)
	srv.SSH.CloseAll()
	log.Info("bye")
}
