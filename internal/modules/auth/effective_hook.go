package auth

import "gorm.io/gorm"

// EffectivePermissionsHook allows the access module to inject GRANT/DENY + matrix overlays
// into every authenticated request without an import cycle.
var EffectivePermissionsHook func(db *gorm.DB, userID uint, role string, functions, specialties []string) ([]string, error)
