package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/user/ticket-booking/internal/api"
	"github.com/user/ticket-booking/internal/auth"
	"github.com/user/ticket-booking/internal/lock"
	"github.com/user/ticket-booking/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	httpAddr := envOrDefault("HTTP_ADDR", ":8081")
	jwtSecret := envOrDefault("JWT_SECRET", "dev-secret-key-change-in-prod")
	holdTTLSec := envIntOrDefault("HOLD_TTL_SECONDS", 600)
	cleanupSec := envIntOrDefault("CLEANUP_INTERVAL_SECONDS", 5)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	locker := lock.NewRedisStyleLocker()
	jwtManager := auth.NewManager(jwtSecret, 15*time.Minute, 7*24*time.Hour)
	bookingSvc := service.NewBookingService(locker, jwtManager, time.Duration(holdTTLSec)*time.Second)

	bookingSvc.StartHoldReaper(ctx, time.Duration(cleanupSec)*time.Second)

	evt := bookingSvc.CreateEvent(
		"Go Systems & Highload Conference 2026",
		"Технопарк Зал А",
		time.Now().UTC().Add(14*24*time.Hour),
		3, 8, 950000,
	)
	logger.Info("seeded demo event", "event_id", evt.ID, "title", evt.Title, "total_seats", evt.TotalSeats)

	handler := api.NewHandler(bookingSvc, jwtManager)
	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           handler.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("ticket-booking HTTP server listening", "addr", httpAddr, "hold_ttl_sec", holdTTLSec)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down ticket-booking service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("ticket-booking stopped cleanly")
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
