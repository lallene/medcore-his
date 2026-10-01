package examcategories

import "strings"

// Laboratory categories match laboratory Materialize / list filters.
// medical_exams.category is free-text; keep this list authoritative for
// laboratory producers and readiness.
var Laboratory = []string{
	"laboratoire",
	"biologie",
	"biochimie",
	"hématologie",
	"hematologie",
	"microbiologie",
	"immunologie",
	"parasitologie",
}

// IsLaboratory reports whether a medical_exams.category belongs to laboratory.
func IsLaboratory(category string) bool {
	normalized := strings.ToLower(strings.TrimSpace(category))
	for _, allowed := range Laboratory {
		if normalized == allowed {
			return true
		}
	}
	return false
}

// IsImaging reports whether a medical_exams.category belongs to imaging.
// Matches imaging Materialize filter (EqualFold "Imagerie").
func IsImaging(category string) bool {
	return strings.EqualFold(strings.TrimSpace(category), "Imagerie")
}
