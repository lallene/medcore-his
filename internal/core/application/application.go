package application

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/container"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/middleware"
)

type Module interface {
	Register(app *Application)
}

type Application struct {
	Config    config.Config
	DB        *gorm.DB
	Router    *gin.Engine
	Container *container.Container
}

func New() *Application {
	cfg := config.Load()

	logger.Init(cfg.AppEnv)
	logger.Info("Démarrage MedCore HIS", "env", cfg.AppEnv)

	if err := scheduling.SetLocation(cfg.Timezone); err != nil {
		logger.Error("Timezone planning", "error", err)
		panic(err)
	}
	logger.Info("Timezone planning", "iana", scheduling.LocationName())

	db := database.Connect(cfg.DatabaseURL)
	di := container.New()

	r := gin.New()

	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	r.Use(gin.Recovery())
	r.Use(middleware.CORS(cfg.CORSOrigin))
	r.Use(middleware.RequestLogger())

	app := &Application{
		Config:    cfg,
		DB:        db,
		Router:    r,
		Container: di,
	}

	app.registerCoreRoutes()
	app.registerCoreAudit()
	app.MustMigrate(&workflow.History{})

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

	a.Router.GET("/health", func(c *gin.Context) {
		sqlDB, err := a.DB.DB()

		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"db":     "down",
			})
			return
		}

		if err := sqlDB.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"db":     "down",
			})
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
	a.MustMigrate(&audit.AuditLog{})
	audit.Register(a.DB)
}

func (a *Application) RegisterModule(module Module) {
	module.Register(a)
}

func (a *Application) Run() {
	logger.Info("API running", "port", a.Config.Port)

	if err := a.Router.Run(":" + a.Config.Port); err != nil {
		logger.Error("Erreur lancement API", "error", err)
		panic(err)
	}
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

func (a *Application) MustMigrate(models ...any) {
	if err := a.DB.AutoMigrate(models...); err != nil {
		logger.Error("Erreur migration", "error", err)
		panic(err)
	}
}
