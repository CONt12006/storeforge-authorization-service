package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddress              string
	DatabaseURL              string
	RedisAddress             string
	RedisPassword            string
	RedisDatabase            int
	InternalAPIKey           string
	SessionServiceURL        string
	SessionServiceAPIKey     string
	SessionValidationEnabled bool
	PermissionsCacheTTL      time.Duration
	HTTPClientTimeout        time.Duration
}

type Loader struct{}

func NewLoader() *Loader {
	return &Loader{}
}

func (l *Loader) Load() (Config, error) {
	redisDB, err := l.intValue("REDIS_DATABASE", 0)
	if err != nil {
		return Config{}, err
	}
	cacheTTL, err := l.durationValue("PERMISSIONS_CACHE_TTL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	clientTimeout, err := l.durationValue("HTTP_CLIENT_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}
	validation, err := l.boolValue("SESSION_VALIDATION_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	config := Config{
		HTTPAddress:              l.value("HTTP_ADDRESS", ":8080"),
		DatabaseURL:              l.value("DATABASE_URL", "postgres://storeforge:storeforge@localhost:5432/storeforge_authorization?sslmode=disable"),
		RedisAddress:             l.value("REDIS_ADDRESS", "localhost:6379"),
		RedisPassword:            os.Getenv("REDIS_PASSWORD"),
		RedisDatabase:            redisDB,
		InternalAPIKey:           l.value("INTERNAL_API_KEY", "change-me"),
		SessionServiceURL:        l.value("SESSION_SERVICE_URL", "http://localhost:8081"),
		SessionServiceAPIKey:     l.value("SESSION_SERVICE_API_KEY", "change-me"),
		SessionValidationEnabled: validation,
		PermissionsCacheTTL:      cacheTTL,
		HTTPClientTimeout:        clientTimeout,
	}
	if config.InternalAPIKey == "" {
		return Config{}, fmt.Errorf("INTERNAL_API_KEY is required")
	}
	return config, nil
}

func (l *Loader) value(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func (l *Loader) intValue(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}

func (l *Loader) boolValue(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}

func (l *Loader) durationValue(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}
