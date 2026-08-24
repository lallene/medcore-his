package insurance_receivables

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "insurance_receivables")
	app.MustMigrate(&Settlement{}, &SettlementAllocation{}, &ReceivableMetadata{}, &FollowUp{}, &SubmissionBatch{}, &SubmissionBatchItem{})
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(NewService(app.DB)))
}
