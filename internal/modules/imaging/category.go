package imaging

import "strings"

func IsImagingCategory(category string) bool {
	return strings.EqualFold(strings.TrimSpace(category), "Imagerie")
}

func modalityForExamCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch {
	case strings.Contains(code, "ULTRASOUND"):
		return "ULTRASOUND"
	case strings.Contains(code, "XRAY") || strings.HasPrefix(code, "XR_"):
		return "XRAY"
	case strings.Contains(code, "CT_SCAN") || strings.HasPrefix(code, "CT_"):
		return "CT"
	case code == "MRI" || strings.HasPrefix(code, "MRI_"):
		return "MRI"
	case strings.Contains(code, "MAMMO"):
		return "MAMMOGRAPHY"
	default:
		return "OTHER"
	}
}
