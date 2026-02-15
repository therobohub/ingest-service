package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	// HTTP Server
	Port string

	// Database
	DatabaseURL string

	// JWT Authentication
	JWTSecret       string
	ClockSkewSeconds int

	// Rate Limiting
	RateLimitRPS   int
	RateLimitBurst int

	// Request Limits
	MaxBodyBytes int64
}

// Load reads configuration from environment variables
func Load() *Config {
	cfg := &Config{
		Port:             getEnv("PORT", "8081"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://robohub:robohub@localhost:5432/robohub_ingest?sslmode=disable"),
		JWTSecret:        getEnv("ROBOHUB_JWT_SECRET", ""),
		ClockSkewSeconds: getEnvInt("ROBOHUB_CLOCK_SKEW_SECONDS", 60),
		RateLimitRPS:     getEnvInt("ROBOHUB_RATE_LIMIT_RPS", 10),
		RateLimitBurst:   getEnvInt("ROBOHUB_RATE_LIMIT_BURST", 20),
		MaxBodyBytes:     getEnvInt64("ROBOHUB_MAX_BODY_BYTES", 262144), // 256KB
	}

	if cfg.JWTSecret == "" {
		// In production, this should be required
		// For development, provide a warning
		cfg.JWTSecret = "dev-secret-change-in-production"
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultVal
}

// ClockSkew returns the configured clock skew as a duration
func (c *Config) ClockSkew() time.Duration {
	return time.Duration(c.ClockSkewSeconds) * time.Second
}
