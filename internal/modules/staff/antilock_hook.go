package staff

import "gorm.io/gorm"

// AfterProfileChangeValidate is set by the access module to enforce RBAC anti-lockout
// on Staff Upsert (same rules as ACC SetFunctions / SetActive).
var AfterProfileChangeValidate func(db *gorm.DB, userID uint, active bool, functions, specialties []string) error
