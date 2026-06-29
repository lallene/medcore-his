package insurance

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/container"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
)

type Provider struct{}

func (Provider) Register(app *application.Application) {
	application.Singleton[company.Repository](app, func(c *container.Container) (company.Repository, error) {
		return company.NewRepository(app.DB), nil
	})

	application.Singleton[company.Service](app, func(c *container.Container) (company.Service, error) {
		repo := container.MustMake[company.Repository](c)
		return company.NewService(repo), nil
	})

	application.Singleton[*company.Handler](app, func(c *container.Container) (*company.Handler, error) {
		service := container.MustMake[company.Service](c)
		return company.NewHandler(service), nil
	})

	application.Singleton[guarantor.Repository](app, func(c *container.Container) (guarantor.Repository, error) {
		return guarantor.NewRepository(app.DB), nil
	})

	application.Singleton[guarantor.Service](app, func(c *container.Container) (guarantor.Service, error) {
		repo := container.MustMake[guarantor.Repository](c)
		return guarantor.NewService(repo), nil
	})

	application.Singleton[*guarantor.Handler](app, func(c *container.Container) (*guarantor.Handler, error) {
		service := container.MustMake[guarantor.Service](c)
		return guarantor.NewHandler(service), nil
	})

	application.Singleton[coverage.Repository](app, func(c *container.Container) (coverage.Repository, error) {
		return coverage.NewRepository(app.DB), nil
	})

	application.Singleton[coverage.Service](app, func(c *container.Container) (coverage.Service, error) {
		repo := container.MustMake[coverage.Repository](c)
		return coverage.NewService(repo), nil
	})

	application.Singleton[*coverage.Handler](app, func(c *container.Container) (*coverage.Handler, error) {
		service := container.MustMake[coverage.Service](c)
		return coverage.NewHandler(service), nil
	})

	application.Singleton[voucher.Repository](app, func(c *container.Container) (voucher.Repository, error) {
		return voucher.NewRepository(app.DB), nil
	})

	application.Singleton[voucher.Service](app, func(c *container.Container) (voucher.Service, error) {
		repo := container.MustMake[voucher.Repository](c)
		coverageRepo := container.MustMake[coverage.Repository](c)

		return voucher.NewService(repo, app.DB, coverageRepo), nil
	})

	application.Singleton[*voucher.Handler](app, func(c *container.Container) (*voucher.Handler, error) {
		service := container.MustMake[voucher.Service](c)

		return voucher.NewHandler(service), nil
	})
}
