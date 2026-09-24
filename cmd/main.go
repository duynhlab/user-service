package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/duynhlab/pkg/authmw"
	"github.com/duynhlab/pkg/httpmw"
	"github.com/duynhlab/pkg/logger/slogx"
	"github.com/duynhlab/pkg/migratex"
	"github.com/duynhlab/pkg/obsx"
	"github.com/duynhlab/user-service/config"
	migrations "github.com/duynhlab/user-service/db/migrations"
	seed "github.com/duynhlab/user-service/db/seed"
	database "github.com/duynhlab/user-service/internal/core"
	"github.com/duynhlab/user-service/internal/core/repository/psql"
	logicv1 "github.com/duynhlab/user-service/internal/logic/v1"
	webv1 "github.com/duynhlab/user-service/internal/web/v1"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	logger := slogx.New(slogx.Config{Level: os.Getenv("LOG_LEVEL")})
	slogx.SetDefault(logger)

	// Subcommands (`migrate`, `seed`) run an embedded SQL set and exit; no args
	// serves the app.
	if len(os.Args) > 1 && runSubcommand(ctx, os.Args[1], cfg, logger) {
		return
	}

	if err := cfg.Validate(); err != nil {
		panic("Configuration validation failed: " + err.Error())
	}

	logger.Info(ctx, "Service starting",
		slog.String("service.version", cfg.Service.Version),
		slog.String("deployment.environment.name", cfg.Service.Env),
	)

	// RFC-0014: single OTel wiring point — traces per TRACING_ENABLED, OTLP
	// metrics (the only pipeline since the P3 cutover; OTEL_METRICS_ENABLED
	// defaults on, =false is a kill switch), logs behind OTEL_LOGS_ENABLED.
	// The config is built once so the tracer scope name and the startup log
	// reflect the values obsx actually uses.
	otelCfg := obsx.ConfigFromEnv()
	var tp interface{ Shutdown(context.Context) error }
	obs, err := obsx.SetupObservability(ctx, otelCfg)
	if err != nil {
		logger.Warn(ctx, "Failed to initialize OpenTelemetry", slogx.Err(err))
	} else {
		tp = obs
		// The facade reaches OTLP through the global logger provider obsx
		// just installed; rebuilding it only wires Flush, so a Fatal record
		// is exported before the process exits.
		logger = slogx.New(slogx.Config{Level: os.Getenv("LOG_LEVEL"), Flush: obs.ForceFlush})
		slogx.SetDefault(logger)
		logger.Info(ctx, "OpenTelemetry initialized",
			slog.Bool("traces", obs.Enabled().Traces),
			slog.Bool("otlp_metrics", obs.Enabled().Metrics),
			slog.Bool("otlp_logs", obs.Enabled().Logs),
			slog.Float64("sample_rate", otelCfg.SampleRate),
		)
	}

	if stopProfiling := initProfiling(ctx, cfg, logger); stopProfiling != nil {
		defer func() { _ = stopProfiling(context.Background()) }()
	}

	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		logger.Error(ctx, "Failed to connect to database", slogx.Err(err))
		return
	}
	defer pool.Close()
	logger.Info(ctx, "Database connection pool established")

	// Initialize Dependency Injection
	userRepo := psql.NewUserRepository(pool)
	userService := logicv1.NewUserService(userRepo)
	userHandler := webv1.NewUserHandler(userService)

	// Local OIDC JWT verification (cached Keycloak JWKS) is the only
	// credential — no gRPC fallback. NewVerifier refreshes in the background
	// and does not block on an unreachable JWKS, so it is safe to build at
	// startup. JWKSURL is optional: empty derives the Keycloak realm certs
	// endpoint from the issuer.
	verifier, err := authmw.NewVerifier(authmw.Config{
		Issuer:   cfg.OIDCIssuer,
		Audience: cfg.OIDCAudience,
		JWKSURL:  cfg.OIDCJWKSURL,
	})
	if err != nil {
		logger.Fatal(ctx, "Failed to initialize JWT verifier", slogx.Err(err))
	}

	// Second verifier for the protected Backoffice group (ADR-050): operators
	// live in the WORKFORCE realm; the customer verifier above never sees
	// their tokens and vice versa.
	staffVerifier, err := authmw.NewVerifier(authmw.Config{
		Issuer:   cfg.OIDCStaffIssuer,
		Audience: cfg.OIDCAudience,
		JWKSURL:  cfg.OIDCStaffJWKSURL,
	})
	if err != nil {
		logger.Fatal(ctx, "Failed to initialize staff JWT verifier", slogx.Err(err))
	}

	var isShuttingDown atomic.Bool
	srv := setupServer(cfg, obsx.ConfigFromEnv().ServiceName, logger, verifier, staffVerifier, &isShuttingDown, userHandler)
	runGracefulShutdown(cfg, srv, tp, pool, logger, &isShuttingDown)
}

// runSubcommand handles the `migrate` and `seed` subcommands. It returns true
// when a subcommand was recognised and executed (the caller then exits), or
// false to fall through to serving the app.
//
// `migrate` applies the versioned schema migrations and runs in every
// environment (init container, direct DB host). `seed` applies DEV-ONLY demo
// data and is invoked explicitly — never by `migrate` or the serve path — so
// production databases are never seeded.
func runSubcommand(ctx context.Context, cmd string, cfg *config.Config, logger *slogx.Logger) bool {
	switch cmd {
	case "migrate":
		if err := migratex.Run(migrations.FS, "sql", cfg.Database.BuildDSN()); err != nil {
			logger.Fatal(ctx, "Schema migration failed", slogx.Err(err))
		}
		logger.Info(ctx, "Schema migrations applied")
		return true
	case "seed":
		// Demo data is DEV-ONLY; refuse to seed a production database.
		if cfg.IsProduction() {
			logger.Fatal(ctx, "seed refused in production — demo data is dev-only")
		}
		if err := applySeed(ctx, cfg); err != nil {
			logger.Fatal(ctx, "Demo seed failed", slogx.Err(err))
		}
		logger.Info(ctx, "Demo seed data applied")
		return true
	default:
		return false
	}
}

// applySeed executes the embedded dev-only seed SQL directly against the database.
// It does NOT use golang-migrate: seeds are idempotent (ON CONFLICT) and must not
// share the schema_migrations version table with the schema migrations. Simple
// query protocol lets each multi-statement seed file run in one Exec.
func applySeed(ctx context.Context, cfg *config.Config) error {
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.BuildDSN())
	if err != nil {
		return fmt.Errorf("parse seed DSN: %w", err)
	}
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect for seed: %w", err)
	}
	defer pool.Close()

	entries, err := fs.ReadDir(seed.FS, "sql")
	if err != nil {
		return fmt.Errorf("read seed dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		b, readErr := fs.ReadFile(seed.FS, "sql/"+name)
		if readErr != nil {
			return fmt.Errorf("read seed %s: %w", name, readErr)
		}
		if _, execErr := pool.Exec(ctx, string(b)); execErr != nil {
			return fmt.Errorf("apply seed %s: %w", name, execErr)
		}
	}
	return nil
}

func initProfiling(ctx context.Context, cfg *config.Config, logger *slogx.Logger) func(context.Context) error {
	if !cfg.Profiling.Enabled {
		logger.Info(ctx, "Profiling disabled (PROFILING_ENABLED=false)")
		return nil
	}
	stop, err := obsx.SetupProfiling()
	if err != nil {
		logger.Warn(ctx, "Failed to initialize profiling", slogx.Err(err))
		return nil
	}
	logger.Info(ctx, "Profiling initialized")
	return stop
}

func setupServer(cfg *config.Config, otelServiceName string, logger *slogx.Logger, verifier *authmw.Verifier, staffVerifier *authmw.Verifier, isShuttingDown *atomic.Bool, userHandler *webv1.UserHandler) *http.Server {
	// gin.New, not gin.Default: Default installs gin's own logger and
	// recovery, which print the raw path and client address past the facade.
	r := gin.New()

	r.Use(httpmw.Tracing(otelServiceName))
	r.Use(httpmw.Logging(logger.Slog()))
	r.Use(httpmw.Recovery(logger.Slog()))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/ready", func(c *gin.Context) {
		if isShuttingDown.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "shutting_down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// User v1 routes — Variant A edge naming (see api-naming-convention.md)
	r.GET("/user/v1/public/users/:id", userHandler.GetUser)

	// Protected: the Backoffice operator search + case view (RFC-0023),
	// staff-realm verified + role-gated; customer routes are untouched.
	webv1.RegisterProtectedRoutes(r, userHandler, staffVerifier)

	privateUsers := r.Group("/user/v1/private/users")
	privateUsers.Use(authmw.MiddlewareJWT(verifier))
	{
		privateUsers.GET("/profile", userHandler.GetProfile)
		privateUsers.PUT("/profile", userHandler.UpdateProfile)
	}

	return &http.Server{
		Addr:              ":" + cfg.Service.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func runGracefulShutdown(
	cfg *config.Config,
	srv *http.Server,
	tp interface{ Shutdown(context.Context) error },
	pool interface{ Close() },
	logger *slogx.Logger,
	isShuttingDown *atomic.Bool,
) {
	bg := context.Background()
	go func() {
		logger.Info(bg, "Starting user service", slog.String("port", cfg.Service.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(bg, "Failed to start server", slogx.Err(err))
		}
	}()
	logger.ProcessStarted(bg, slogx.ComponentAPI)

	ctx, stop := signal.NotifyContext(bg, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	<-ctx.Done()
	logger.Info(bg, "Shutdown signal received")

	isShuttingDown.Store(true)
	drainDelay := cfg.GetReadinessDrainDelayDuration()
	if drainDelay > 0 {
		logger.Info(bg, "Readiness drain delay started", slog.Duration("delay", drainDelay))
		time.Sleep(drainDelay)
	}

	shutdownTimeout := cfg.GetShutdownTimeoutDuration()
	shutdownCtx, cancel := context.WithTimeout(bg, shutdownTimeout)
	defer cancel()

	logger.Info(bg, "Shutting down server...", slog.Duration("timeout", shutdownTimeout))

	outcome := slogx.OutcomeGraceful
	if err := srv.Shutdown(shutdownCtx); err != nil {
		outcome = slogx.OutcomeError
		logger.Error(bg, "HTTP server shutdown error", slogx.Err(err))
	} else {
		logger.Info(bg, "HTTP server shutdown complete")
	}

	pool.Close()
	logger.Info(bg, "Database pool closed")

	// process.stopped is written BEFORE the OTel SDK shuts down: a record
	// emitted after it is dropped rather than exported.
	logger.ProcessStopped(bg, slogx.ComponentAPI, outcome)

	// Shutdown the OTel SDK — flushes pending spans plus any OTLP
	// metrics/logs providers built behind the RFC-0014 flags.
	if tp != nil {
		if err := tp.Shutdown(shutdownCtx); err != nil {
			logger.Error(bg, "OpenTelemetry shutdown error", slogx.Err(err))
		} else {
			logger.Info(bg, "OpenTelemetry shutdown complete")
		}
	}

	logger.Info(bg, "Graceful shutdown complete")
}
