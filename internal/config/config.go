package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	SMTPAddr         string
	SMTPDomain       string
	SMTPAllowedCIDRs []string
	MaxMessageBytes  int64
	QueueDir         string
	HTTPAddr         string
	PushoverToken    string
	PushoverUser     string
	PushoverDevice   string
	PushoverEndpoint string
	PushoverTimeout  time.Duration
	LogLevel         string
}

func Load() (Config, error) {
	maxMessageBytes, err := envInt64("SMTP_MAX_MESSAGE_BYTES", 10*1024*1024)
	if err != nil {
		return Config{}, err
	}
	pushoverTimeout, err := envDuration("PUSHOVER_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		SMTPAddr:         env("SMTP_ADDR", ":2525"),
		SMTPDomain:       env("SMTP_DOMAIN", "mailnotifier.local"),
		SMTPAllowedCIDRs: splitCSV(env("SMTP_ALLOWED_CIDRS", "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,::1/128,fc00::/7")),
		MaxMessageBytes:  maxMessageBytes,
		QueueDir:         env("QUEUE_DIR", "/data/queue"),
		HTTPAddr:         env("HTTP_ADDR", ":8080"),
		PushoverToken:    strings.TrimSpace(os.Getenv("PUSHOVER_TOKEN")),
		PushoverUser:     strings.TrimSpace(os.Getenv("PUSHOVER_USER")),
		PushoverDevice:   strings.TrimSpace(os.Getenv("PUSHOVER_DEVICE")),
		PushoverEndpoint: env("PUSHOVER_ENDPOINT", "https://api.pushover.net/1/messages.json"),
		PushoverTimeout:  pushoverTimeout,
		LogLevel:         strings.ToLower(env("LOG_LEVEL", "info")),
	}

	if cfg.PushoverToken == "" {
		return Config{}, fmt.Errorf("PUSHOVER_TOKEN is required")
	}
	if cfg.PushoverUser == "" {
		return Config{}, fmt.Errorf("PUSHOVER_USER is required")
	}
	if cfg.MaxMessageBytes <= 0 {
		return Config{}, fmt.Errorf("SMTP_MAX_MESSAGE_BYTES must be greater than zero")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt64(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	values := strings.Split(value, ",")
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
