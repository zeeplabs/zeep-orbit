// Command zeep-builder is the sandbox daemon of the AI Frontend Builder. It
// executes workspace operations and fixed build steps on per-session git
// working copies and serves the preview of the last successful build. It is
// called only by Orbit; every route needs a signed X-Builder-Token.
//
// Environment (the only variables it reads):
//
//	BUILDER_ADDR                 listen address (default ":8081")
//	BUILDER_SHARED_SECRET        HMAC secret shared with Orbit (required, min 16 bytes)
//	BUILDER_WORK_DIR             session directories (default "/work")
//	BUILDER_TEMPLATES_DIR        template directories (default "/opt/templates")
//	BUILDER_MAX_ACTIVE_SESSIONS  capacity limit (default 4)
//	BUILDER_NPM_REGISTRY         registry URL handed to npm (the registry proxy)
//	BUILDER_NPM_CACHE            shared npm cache (default "<work dir>/.npm-cache")
//	BUILDER_HOME                 HOME of build steps (default "/tmp")
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/builder"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const (
	defaultAddr         = ":8081"
	defaultWorkDir      = "/work"
	defaultTemplatesDir = "/opt/templates"
	maxSessionsLimit    = 1024

	// writeTimeout must cover the longest route (a build step plus margin):
	// net/http resets the write deadline to this value for every request and
	// the handlers tighten it per route.
	writeTimeout    = 15 * time.Minute
	shutdownTimeout = 60 * time.Second
)

// config is the parsed environment. It holds the shared secret, so it must
// never be logged or formatted.
type config struct {
	addr         string
	secret       []byte
	workDir      string
	templatesDir string
	maxSessions  int
	npmRegistry  string
	npmCache     string
	home         string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig reads and validates the environment. Errors name the variable
// but never echo its value (the secret and the registry URL may be sensitive).
func loadConfig(getenv func(string) string) (config, error) {
	get := func(name, def string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return def
	}
	cfg := config{
		addr:         get("BUILDER_ADDR", defaultAddr),
		secret:       []byte(getenv("BUILDER_SHARED_SECRET")),
		templatesDir: get("BUILDER_TEMPLATES_DIR", defaultTemplatesDir),
		npmRegistry:  getenv("BUILDER_NPM_REGISTRY"),
		home:         getenv("BUILDER_HOME"),
		maxSessions:  builder.DefaultMaxActiveSessions,
	}
	if len(cfg.secret) == 0 {
		return config{}, errors.New("BUILDER_SHARED_SECRET is required")
	}
	if len(cfg.secret) < builder.MinSecretLen {
		return config{}, fmt.Errorf("BUILDER_SHARED_SECRET must be at least %d bytes", builder.MinSecretLen)
	}

	var err error
	if cfg.workDir, err = filepath.Abs(get("BUILDER_WORK_DIR", defaultWorkDir)); err != nil {
		return config{}, errors.New("BUILDER_WORK_DIR is not a valid path")
	}
	if cfg.templatesDir, err = filepath.Abs(cfg.templatesDir); err != nil {
		return config{}, errors.New("BUILDER_TEMPLATES_DIR is not a valid path")
	}
	cfg.npmCache = getenv("BUILDER_NPM_CACHE")
	if cfg.npmCache == "" {
		cfg.npmCache = filepath.Join(cfg.workDir, ".npm-cache")
	}
	if cfg.npmCache, err = filepath.Abs(cfg.npmCache); err != nil {
		return config{}, errors.New("BUILDER_NPM_CACHE is not a valid path")
	}
	if cfg.home != "" && !filepath.IsAbs(cfg.home) {
		return config{}, errors.New("BUILDER_HOME must be an absolute path")
	}
	if v := getenv("BUILDER_MAX_ACTIVE_SESSIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxSessionsLimit {
			return config{}, fmt.Errorf("BUILDER_MAX_ACTIVE_SESSIONS must be an integer between 1 and %d", maxSessionsLimit)
		}
		cfg.maxSessions = n
	}
	if cfg.npmRegistry != "" {
		u, err := url.Parse(cfg.npmRegistry)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return config{}, errors.New("BUILDER_NPM_REGISTRY must be an http(s) URL")
		}
	}
	return cfg, nil
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	signer, err := builder.NewSigner(cfg.secret)
	if err != nil {
		return fmt.Errorf("BUILDER_SHARED_SECRET: %w", err)
	}

	// Session roots may hold unpublished customer code: private to the
	// builder user. The npm cache holds public packages only.
	if err := os.MkdirAll(cfg.workDir, 0o700); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	if err := os.MkdirAll(cfg.npmCache, 0o755); err != nil {
		return fmt.Errorf("create npm cache dir: %w", err)
	}

	// git gets its own private HOME (default under os.TempDir()): BUILDER_HOME
	// belongs to build steps, and git's home is chmod'ed 0700 on startup.
	gitRepo, err := builder.NewGitRepo(builder.GitConfig{})
	if err != nil {
		return fmt.Errorf("git: %w", err)
	}
	executor, err := builder.NewExecutor(builder.ExecConfig{
		WorkDir:     cfg.workDir,
		NpmRegistry: cfg.npmRegistry,
		NpmCache:    cfg.npmCache,
		Home:        cfg.home,
	})
	if err != nil {
		return fmt.Errorf("executor: %w", err)
	}
	api, err := builder.NewServer(builder.ServerConfig{
		WorkDir:           cfg.workDir,
		TemplatesDir:      cfg.templatesDir,
		Signer:            signer,
		Executor:          executor,
		Git:               gitRepo,
		MaxActiveSessions: cfg.maxSessions,
		Version:           version,
		Logger:            logger,
	})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// ReadTimeout stays 0: a template upload may be large, so body reads
		// are bounded per route by the handlers instead.
		WriteTimeout:   writeTimeout,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}

	// Paths and limits only: the secret and the registry URL are never logged.
	logger.Info("zeep-builder starting",
		"addr", cfg.addr, "version", version,
		"work_dir", cfg.workDir, "templates_dir", cfg.templatesDir,
		"max_active_sessions", cfg.maxSessions)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
