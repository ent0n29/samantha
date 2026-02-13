package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ent0n29/samantha/internal/app"
	"github.com/ent0n29/samantha/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx := context.Background()
	build, err := app.Build(ctx, cfg)
	if err != nil {
		log.Fatalf("app init failed: %v", err)
	}
	defer func() {
		if err := build.Cleanup(); err != nil {
			log.Printf("cleanup error: %v", err)
		}
	}()

	cfg = build.Config
	if build.Voice.Detail != "" {
		log.Printf("voice provider: %s", build.Voice.Detail)
	} else {
		log.Printf("voice provider: %s", cfg.VoiceProvider)
	}
	if build.TaskService != nil {
		log.Printf("task runtime: enabled=%t store=%s", build.TaskService.Enabled(), build.TaskService.StoreMode())
	}

	httpServer := &http.Server{
		Addr:    cfg.BindAddr,
		Handler: build.API.Router(),
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	build.Sessions.StartJanitor(runCtx, 5*time.Second)

	go func() {
		log.Printf("server listening on %s", cfg.BindAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("shutdown signal received")

	runCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		_ = httpServer.Close()
	}

	log.Printf("shutdown complete")
}
