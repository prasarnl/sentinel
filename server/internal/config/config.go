package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                   string
	DatabaseURL            string
	JWTSecret              []byte
	AdminBootstrapUser     string
	AdminBootstrapPassword string
	DownloadsDir           string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                   getenv("PORT", "4000"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		JWTSecret:              []byte(os.Getenv("JWT_SECRET")),
		AdminBootstrapUser:     getenv("ADMIN_BOOTSTRAP_USER", "admin"),
		AdminBootstrapPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
		DownloadsDir:           getenv("DOWNLOADS_DIR", "./downloads"),
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) == 0 {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
