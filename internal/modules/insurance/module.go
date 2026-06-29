package insurance

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "insurance")

	app.MustMigrate(
		&company.InsuranceCompany{},
		&guarantor.InsuranceGuarantor{},
		&coverage.PatientCoverage{},
		&voucher.InsuranceVoucher{},
	)

	Provider{}.Register(app)

	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret))

	companyHandler := application.Make[*company.Handler](app)

	guarantorHandler := application.Make[*guarantor.Handler](app)
	guarantor.RegisterRoutes(protected, guarantorHandler)

	coverageHandler := application.Make[*coverage.Handler](app)
	coverage.RegisterRoutes(protected, coverageHandler)

	voucherHandler := application.Make[*voucher.Handler](app)
	voucher.RegisterRoutes(protected, voucherHandler)

	company.RegisterRoutes(protected, companyHandler)
}
