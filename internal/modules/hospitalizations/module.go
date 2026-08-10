package hospitalizations

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "hospitalizations")
	app.MustMigrate(&Hospitalization{}, &Room{}, &Bed{}, &BedAssignment{})
	if err := app.DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalization_bed_assignments_active_bed ON hospitalization_bed_assignments (bed_id) WHERE released_at IS NULL AND deleted_at IS NULL").Error; err != nil {
		panic(err)
	}
	if err := app.DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalization_bed_assignments_active_stay ON hospitalization_bed_assignments (hospitalization_id) WHERE released_at IS NULL AND deleted_at IS NULL").Error; err != nil {
		panic(err)
	}
	repo := NewRepository(app.DB)
	service := NewService(app.DB, repo)
	handler := NewHandler(service)
	protected := app.API()
	protected.Use(auth.Middleware(app.Config.JWTSecret))
	RegisterRoutes(protected, handler, NewBedHandler(service))
}
