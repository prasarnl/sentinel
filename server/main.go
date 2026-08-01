package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sentinel/server/internal/api"
	"sentinel/server/internal/auth"
	"sentinel/server/internal/config"
	"sentinel/server/internal/db"
	"sentinel/server/internal/models"
	"sentinel/server/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if err := bootstrapAdmin(ctx, pool, cfg.AdminBootstrapUser, cfg.AdminBootstrapPassword); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}

	authSvc := auth.NewService(cfg.JWTSecret)
	hub := ws.NewHub()

	go markStaleHostsOffline(ctx, pool)
	// Endpoints registered without a host have no agent to scrape them, so
	// the server does it directly.
	go api.PollRemoteEndpoints(ctx, pool, hub)

	server := &api.Server{Pool: pool, Auth: authSvc, Hub: hub, DownloadsDir: cfg.DownloadsDir}
	router, err := server.Router()
	if err != nil {
		log.Fatalf("router: %v", err)
	}

	log.Printf("sentinel server listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// bootstrapAdmin creates the initial admin account on first run if the
// users table is empty, so there's always a way to log in.
func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, username, password string) error {
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)`,
		username, hash, models.RoleAdmin,
	)
	if err == nil {
		log.Printf("bootstrapped initial admin user %q", username)
	}
	return err
}

// markStaleHostsOffline periodically flips hosts to "offline" if they
// haven't ingested in a while, so the dashboard reflects reality even
// between agent pushes.
func markStaleHostsOffline(ctx context.Context, pool *pgxpool.Pool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		_, err := pool.Exec(ctx,
			`UPDATE hosts SET status = 'offline' WHERE status = 'online' AND last_seen < now() - interval '60 seconds'`)
		if err != nil {
			log.Printf("mark stale hosts offline: %v", err)
		}
	}
}
