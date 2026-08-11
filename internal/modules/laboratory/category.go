package laboratory

import "strings"

var laboratoryCategories = []string{
	"laboratoire",
	"biologie",
	"biochimie",
	"hématologie",
	"hematologie",
	"microbiologie",
	"immunologie",
	"parasitologie",
}

func IsLaboratoryCategory(category string) bool {
	normalized := strings.ToLower(strings.TrimSpace(category))
	for _, allowed := range laboratoryCategories {
		if normalized == allowed {
			return true
		}
	}
	return false
}
