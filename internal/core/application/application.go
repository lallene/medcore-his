package application

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/container"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/middleware"
)

type Module interface {
	Register(app *Application)
}

type Application struct {
	Config           config.Config
	BusinessLocation *time.Location
	DB               *gorm.DB
	Router           *gin.Engine
	Container        *container.Container

	// draining is set on SIGINT/SIGTERM: /health answers 503 so the edge
	// stops routing new traffic while in-flight requests finish.
	draining atomic.Bool
}

// healthPingTimeout bounds the database check of /health.
const healthPingTimeout = 2 * time.Second

func New() *Application {
	cfg := config.Load()

	logger.Init(cfg.AppEnv)
	logger.Info("Démarrage MedCore HIS", "env", cfg.AppEnv)

	if err := scheduling.SetLocation(cfg.Timezone); err != nil {
		logger.Error("Timezone planning", "error", err)
		panic(err)
	}
	logger.Info("Timezone planning", "iana", scheduling.LocationName())
	logger.Info("Timezone métier", "iana", cfg.BusinessTimezone)

	db := database.Connect(cfg.DatabaseURL, cfg.BusinessTimezone)
	di := container.New()

	r := gin.New()

	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	r.Use(gin.Recovery())
	r.Use(middleware.CORS(cfg.CORSOrigin))
	r.Use(middleware.SecurityHeaders())
	// Request bodies are bounded before any handler reads them (GRC15-B07).
	r.Use(middleware.BodyLimit(cfg.HTTP.MaxBodyBytes, cfg.HTTP.MaxUploadBytes))
	r.Use(middleware.RequestLogger())

	app := &Application{
		Config:           cfg,
		BusinessLocation: cfg.BusinessLocation(),
		DB:               db,
		Router:           r,
		Container:        di,
	}

	app.registerCoreRoutes()
	app.registerCoreAudit()

	return app
}

func (a *Application) registerCoreRoutes() {

	a.Router.GET("/swagger/*any",
		ginSwagger.WrapHandler(swaggerFiles.Handler),
	)

	a.Router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "MedCore HIS API",
		})
	})

	// /health is the readiness check of the platform: the process serves
	// traffic only with its database. It answers 503 while draining or when
	// the bounded database ping fails; it never exposes DSN, host or SQL error.
	a.Router.GET("/health", func(c *gin.Context) {
		down := func() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "db": "down"})
		}
		if a.draining.Load() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "draining"})
			return
		}
		sqlDB, err := a.DB.DB()
		if err != nil {
			down()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), healthPingTimeout)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			down()
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "medcore-his-api",
			"db":      "up",
		})
	})
}

func (a *Application) registerCoreAudit() {
	// Schema for audit_logs is owned by cmd/migrate (LOT 26I-3).
	audit.Register(a.DB)
}

func (a *Application) RegisterModule(module Module) {
	module.Register(a)
}

// Run serves the API until SIGINT/SIGTERM, then drains in-flight requests
// within MEDCORE_HTTP_SHUTDOWN_TIMEOUT. A listen failure or a forced stop
// exits non-zero; errors never contain secrets.
func (a *Application) Run() {
	addr := ":" + a.Config.Port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("Erreur lancement API", "port", a.Config.Port, "error", "écoute impossible")
		os.Exit(1)
	}

	srv := newHTTPServer(a.Config.HTTP, addr, a.Router)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("API running", "port", a.Config.Port,
		"readHeaderTimeout", srv.ReadHeaderTimeout.String(), "readTimeout", srv.ReadTimeout.String(),
		"writeTimeout", srv.WriteTimeout.String(), "idleTimeout", srv.IdleTimeout.String())

	runErr := serve(ctx, srv, ln, a.Config.HTTP.ShutdownTimeout, func() {
		a.draining.Store(true)
		logger.Info("API shutdown", "timeout", a.Config.HTTP.ShutdownTimeout.String())
	})
	if sqlDB, err := a.DB.DB(); err == nil {
		_ = sqlDB.Close()
	}
	if runErr != nil {
		logger.Error("Arrêt API", "error", runErr)
		os.Exit(1)
	}
	logger.Info("API stopped")
}

func Singleton[T any](app *Application, factory func(c *container.Container) (T, error)) {
	container.Singleton[T](app.Container, factory)
}

func Factory[T any](app *Application, factory func(c *container.Container) (T, error)) {
	container.FactoryBind[T](app.Container, factory)
}

func Instance[T any](app *Application, service T) {
	container.Instance[T](app.Container, service)
}

func Make[T any](app *Application) T {
	return container.MustMake[T](app.Container)
}

func (a *Application) API() *gin.RouterGroup {
	return a.Router.Group("/api")
}
