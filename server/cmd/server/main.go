package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mlhiter/mspace/server/internal/control"
)

func main() {
	loadLocalEnv()
	cfg := loadConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL is required for mspace server")
		os.Exit(1)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		slog.Error("ping postgres", "error", err)
		os.Exit(1)
	}
	if err := control.Migrate(ctx, pool); err != nil {
		slog.Error("migrate postgres", "error", err)
		os.Exit(1)
	}

	store := control.NewPostgresStore(pool)
	github := control.GitHubHTTPClient{
		ClientID:     cfg.GitHubClientID,
		ClientSecret: cfg.GitHubClientSecret,
	}
	server := control.NewServer(cfg, store, github)

	slog.Info("starting mspace server", "addr", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, server.Routes()); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() control.Config {
	return control.Config{
		Addr:               envDefault("MSPACE_SERVER_ADDR", "127.0.0.1:8787"),
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		GitHubClientID:     strings.TrimSpace(os.Getenv("MSPACE_GITHUB_CLIENT_ID")),
		GitHubClientSecret: strings.TrimSpace(os.Getenv("MSPACE_GITHUB_CLIENT_SECRET")),
		GitHubRedirectURI:  strings.TrimSpace(os.Getenv("MSPACE_GITHUB_REDIRECT_URI")),
	}
}

func envDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func loadLocalEnv() {
	root, err := findProjectRoot()
	if err != nil {
		slog.Warn("skip local env files", "error", err)
		return
	}

	initialEnv := map[string]bool{}
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			initialEnv[key] = true
		}
	}

	for _, path := range []string{
		filepath.Join(root, ".env"),
		filepath.Join(root, ".env.local"),
		filepath.Join(root, "server", ".env"),
		filepath.Join(root, "server", ".env.local"),
	} {
		if err := loadEnvFile(path, initialEnv); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("load local env file", "path", path, "error", err)
		}
	}
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "server", "go.mod")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("project root not found from %s", dir)
		}
		dir = parent
	}
}

func loadEnvFile(path string, initialEnv map[string]bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d missing '='", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d empty key", path, lineNumber)
		}
		if initialEnv[key] {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("%s:%d set %s: %w", path, lineNumber, key, err)
		}
	}
	return scanner.Err()
}
