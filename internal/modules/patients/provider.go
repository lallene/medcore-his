package patients

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/container"
)

type Provider struct{}

func (Provider) Register(app *application.Application) {
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
}
