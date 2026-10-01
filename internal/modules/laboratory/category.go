package laboratory

import (
	"github.com/lallene/medcore-his/backend/internal/modules/examcategories"
)

// laboratoryCategories retained as alias for repository SQL IN (?) clauses.
var laboratoryCategories = examcategories.Laboratory

func IsLaboratoryCategory(category string) bool {
	return examcategories.IsLaboratory(category)
}
