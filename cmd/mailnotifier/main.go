package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	smtp "github.com/emersion/go-smtp"

	"github.com/KevinCFechtel/MailNotifyer/internal/config"
	"github.com/KevinCFechtel/MailNotifyer/internal/queue"
	"github.com/KevinCFechtel/MailNotifyer/internal/smtpserver"
	"github.com/KevinCFechtel/MailNotifyer/internal/target/pushover"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	target := pushover.New(
		cfg.PushoverToken,
		cfg.PushoverUser,
		cfg.PushoverDevice,
		cfg.PushoverEndpoint,
		cfg.PushoverTimeout,
	)
	deliveryQueue, err := queue.New(cfg.QueueDir, target, logger)
	if err != nil {
		logger.Error("cannot initialize queue", "error", err)
		os.Exit(1)
	}
	backend, err := smtpserver.NewBackend(cfg.SMTPAllowedCIDRs, deliveryQueue, logger)
	if err != nil {
		logger.Error("cannot initialize SMTP backend", "error", err)
		os.Exit(1)
	}

	smtpServer := smtp.NewServer(backend)
	smtpServer.Addr = cfg.SMTPAddr
	smtpServer.Domain = cfg.SMTPDomain
	smtpServer.MaxMessageBytes = cfg.MaxMessageBytes
	smtpServer.MaxRecipients = 100
	smtpServer.ReadTimeout = 60 * time.Second
	smtpServer.WriteTimeout = 60 * time.Second
	smtpServer.EnableSMTPUTF8 = true

	healthServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go deliveryQueue.Run(ctx)

	errs := make(chan error, 2)
	go func() {
		logger.Info("SMTP server listening", "address", cfg.SMTPAddr, "domain", cfg.SMTPDomain)
		errs <- smtpServer.ListenAndServe()
	}()
	go func() {
		logger.Info("health server listening", "address", cfg.HTTPAddr)
		errs <- healthServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errs:
		if !errors.Is(err, smtp.ErrServerClosed) && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := smtpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("SMTP shutdown failed", "error", err)
	}
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("health server shutdown failed", "error", err)
	}
}

func runHealthcheck() {
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		slog.Error("invalid health address", "address", address, "error", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		slog.Error("health request failed", "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Error("health request returned unexpected status", "status", resp.StatusCode)
		os.Exit(1)
	}
}

func healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
