package dashboard

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/container"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "dashboard")

	application.Singleton[Repository](app, func(c *container.Container) (Repository, error) {
		return NewRepository(app.DB), nil
	})

	application.Singleton[Service](app, func(c *container.Container) (Service, error) {
		repo := container.MustMake[Repository](c)
		return NewService(repo), nil
	})

	application.Singleton[*Handler](app, func(c *container.Container) (*Handler, error) {
		service := container.MustMake[Service](c)
		return NewHandler(service), nil
	})

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret, app.DB))

	handler := application.Make[*Handler](app)

	RegisterRoutes(protected, handler)
}
