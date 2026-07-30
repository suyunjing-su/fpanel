package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address           string
	DatabasePath      string
	JWTSecret         string
	LogLevel          string
	AllowedOrigins    []string
	ShutdownTimeout   time.Duration
	TokenTTL          time.Duration
	BootstrapUsername string
	BootstrapPassword string
}

func Load() (Config, error) {
	shutdownTimeout, err := durationEnv("SHUTDOWN_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	tokenTTL, err := durationEnv("TOKEN_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Address:           env("HTTP_ADDR", ":6365"),
		DatabasePath:      env("DB_PATH", "/app/data/gost.db"),
		JWTSecret:         strings.TrimSpace(os.Getenv("JWT_SECRET")),
		LogLevel:          strings.ToLower(env("LOG_LEVEL", "info")),
		AllowedOrigins:    splitCSV(os.Getenv("ALLOWED_ORIGINS")),
		ShutdownTimeout:   shutdownTimeout,
		TokenTTL:          tokenTTL,
		BootstrapUsername: env("BOOTSTRAP_USERNAME", "admin"),
		BootstrapPassword: strings.TrimSpace(os.Getenv("BOOTSTRAP_PASSWORD")),
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	if cfg.BootstrapPassword != "" && len(cfg.BootstrapPassword) < 12 {
		return Config{}, errors.New("BOOTSTRAP_PASSWORD must contain at least 12 characters")
	}
	if cfg.ShutdownTimeout <= 0 || cfg.TokenTTL <= 0 {
		return Config{}, errors.New("timeouts must be positive")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return duration, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %s=%q", key, value)
	}
	return time.Duration(seconds) * time.Second, nil
}
