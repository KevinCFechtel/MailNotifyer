package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	smtp "github.com/emersion/go-smtp"

	"github.com/KevinCFechtel/MailNotifyer/internal/config"
	"github.com/KevinCFechtel/MailNotifyer/internal/health"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go deliveryQueue.Run(ctx)

	errs := make(chan error, 1)
	go func() {
		logger.Info("SMTP server listening", "address", cfg.SMTPAddr, "domain", cfg.SMTPDomain)
		errs <- smtpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errs:
		if !errors.Is(err, smtp.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := smtpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("SMTP shutdown failed", "error", err)
	}
}

func runHealthcheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	endpoint := strings.TrimSpace(os.Getenv("HEALTHCHECK_URL"))
	if endpoint == "" {
		endpoint = health.DefaultEndpoint
	}
	checker := health.NewChecker(endpoint, &http.Client{})
	if err := checker.Check(ctx); err != nil {
		slog.Error("healthcheck failed", "error", err)
		os.Exit(1)
	}
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
