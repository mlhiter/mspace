package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/user"
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

	store, closeStore, err := openStore(ctx, cfg)
	if err != nil {
		slog.Error("open store", "error", err)
		os.Exit(1)
	}
	defer closeStore()

	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bootstrapCancel()
	if err := ensureBootstrapAdmin(bootstrapCtx, store, cfg); err != nil {
		slog.Error("ensure bootstrap admin", "error", err)
		os.Exit(1)
	}
	if persistent, ok := store.(interface{ Persist() error }); ok {
		if err := persistent.Persist(); err != nil {
			slog.Error("persist bootstrap state", "error", err)
			os.Exit(1)
		}
	}
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
		Addr:                   envDefault("MSPACE_SERVER_ADDR", "127.0.0.1:8787"),
		DatabaseURL:            strings.TrimSpace(os.Getenv("DATABASE_URL")),
		StoreMode:              strings.TrimSpace(os.Getenv("MSPACE_STORE")),
		SQLitePath:             strings.TrimSpace(os.Getenv("MSPACE_SQLITE_PATH")),
		GitHubClientID:         strings.TrimSpace(os.Getenv("MSPACE_GITHUB_CLIENT_ID")),
		GitHubClientSecret:     strings.TrimSpace(os.Getenv("MSPACE_GITHUB_CLIENT_SECRET")),
		GitHubRedirectURI:      strings.TrimSpace(os.Getenv("MSPACE_GITHUB_REDIRECT_URI")),
		ServerAdminLogins:      envList("MSPACE_SERVER_ADMIN_LOGINS"),
		BootstrapAdminLogin:    strings.TrimSpace(os.Getenv("MSPACE_BOOTSTRAP_ADMIN_LOGIN")),
		BootstrapAdminPassword: strings.TrimSpace(os.Getenv("MSPACE_BOOTSTRAP_ADMIN_PASSWORD")),
		BootstrapAdminName:     strings.TrimSpace(os.Getenv("MSPACE_BOOTSTRAP_ADMIN_NAME")),
		BootstrapAdminEmail:    strings.TrimSpace(os.Getenv("MSPACE_BOOTSTRAP_ADMIN_EMAIL")),
	}
}

func openStore(ctx context.Context, cfg control.Config) (control.Store, func(), error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.StoreMode))
	if mode == "" {
		if strings.TrimSpace(cfg.DatabaseURL) == "" {
			mode = "sqlite"
		} else {
			mode = "postgres"
		}
	}
	switch mode {
	case "postgres":
		if cfg.DatabaseURL == "" {
			return nil, nil, fmt.Errorf("DATABASE_URL is required when MSPACE_STORE=postgres")
		}
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, fmt.Errorf("connect postgres: %w", err)
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("ping postgres: %w", err)
		}
		if err := control.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, nil, fmt.Errorf("migrate postgres: %w", err)
		}
		slog.Info("using postgres store")
		return control.NewPostgresStore(pool), pool.Close, nil
	case "sqlite":
		path := strings.TrimSpace(cfg.SQLitePath)
		if path == "" {
			path = defaultSQLitePath()
		}
		store, err := control.NewSQLiteStore(ctx, path)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite store: %w", err)
		}
		slog.Info("using sqlite personal store", "path", store.Path())
		return store, func() { _ = store.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported MSPACE_STORE %q", cfg.StoreMode)
	}
}

func defaultSQLitePath() string {
	if dir := strings.TrimSpace(os.Getenv("MSPACE_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "mspace.db")
	}
	if configDir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(configDir) != "" {
		return filepath.Join(configDir, "mspace", "mspace.db")
	}
	if currentUser, err := user.Current(); err == nil && strings.TrimSpace(currentUser.HomeDir) != "" {
		return filepath.Join(currentUser.HomeDir, ".mspace", "mspace.db")
	}
	return filepath.Join(".", "mspace.db")
}

func ensureBootstrapAdmin(ctx context.Context, store control.Store, cfg control.Config) error {
	login := strings.TrimSpace(cfg.BootstrapAdminLogin)
	password := strings.TrimSpace(cfg.BootstrapAdminPassword)
	if login == "" && password == "" {
		return nil
	}
	if login == "" || password == "" {
		return fmt.Errorf("MSPACE_BOOTSTRAP_ADMIN_LOGIN and MSPACE_BOOTSTRAP_ADMIN_PASSWORD must be set together")
	}
	name := strings.TrimSpace(cfg.BootstrapAdminName)
	if name == "" {
		name = login
	}
	user, _, created, err := store.EnsureBootstrapAdmin(ctx, control.PasswordAuthInput{
		Login:    login,
		Name:     name,
		Email:    cfg.BootstrapAdminEmail,
		Password: password,
	})
	if err != nil {
		return err
	}
	if created {
		slog.Info("created bootstrap admin", "login", login, "userID", user.ID)
	} else {
		slog.Info("bootstrap admin already exists", "login", login, "userID", user.ID)
	}
	return nil
}

func envDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	items := []string{}
	for _, item := range strings.Split(raw, ",") {
		value := strings.TrimSpace(item)
		if value != "" {
			items = append(items, value)
		}
	}
	return items
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
