package access

import (
	"github.com/lallene/medcore-his/backend/internal/core/application"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/gorm"
)

type Module struct{}

func (Module) Register(app *application.Application) {
	logger.Info("Chargement module", "module", "access")
	app.MustMigrate(&PermissionOverride{}, &MatrixOverride{}, &AccessAuditEvent{})
	svc := NewService(app.DB)
	SetRuntime(svc)
	auth.EffectivePermissionsHook = func(db *gorm.DB, userID uint, role string, functions, specialties []string) ([]string, error) {
		return svc.ComputeEffectivePermissions(userID)
	}
	staff.AfterProfileChangeValidate = func(db *gorm.DB, userID uint, active bool, functions, specialties []string) error {
		lock := NewService(db)
		if !active {
			return lock.assertNotLastAdmin(userID, nil)
		}
		var role string
		_ = db.Table("users").Select("role").Where("id=?", userID).Scan(&role)
		overlays, err := lock.loadMatrixOverlays()
		if err != nil {
			return err
		}
		overrides, err := lock.loadUserOverrides(userID)
		if err != nil {
			return err
		}
		perms := rbac.EffectiveStaffPermissionsFull(role, functions, specialties, overlays, overrides)
		return lock.assertNotLastAdmin(userID, perms)
	}
	g := app.API()
	g.Use(auth.Middleware(app.Config.JWTSecret, app.DB))
	RegisterRoutes(g, NewHandler(svc))
}
