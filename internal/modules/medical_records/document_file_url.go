package medical_records

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// MedicalDocumentFileURLMaxLen matches the medical_documents.file_url column (varchar 500).
const MedicalDocumentFileURLMaxLen = 500

// MedicalDocumentLabelMaxLen matches the medical_documents.label column.
const MedicalDocumentLabelMaxLen = 255

// MedicalDocumentTypeMaxLen matches the medical_documents.type column.
const MedicalDocumentTypeMaxLen = 100

// ValidateMedicalDocumentFileURL enforces C2-A external-reference policy:
// absolute HTTPS URL, non-empty host, no userinfo/credentials, bounded length.
// Callers persist the validated input as-is (no aggressive rewrite).
// This function never performs network I/O.
func ValidateMedicalDocumentFileURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return invalid("document", "file_url est obligatoire")
	}
	if utf8.RuneCountInString(trimmed) > MedicalDocumentFileURLMaxLen {
		return invalid("document", "file_url trop long")
	}
	// Relative / scheme-relative references are not absolute external URLs.
	if !strings.Contains(trimmed, "://") {
		return invalid("document", "file_url doit être une URL HTTPS absolue")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return invalid("document", "file_url invalide")
	}
	// url.Parse normalizes scheme to lowercase.
	if parsed.Scheme != "https" {
		return invalid("document", "file_url doit utiliser HTTPS")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return invalid("document", "file_url doit contenir un hôte valide")
	}
	if parsed.User != nil {
		return invalid("document", "file_url ne doit pas contenir d'identifiants")
	}
	return nil
}

func validateDocumentFileURLWrite(item MedicalDocumentRequest, create bool) error {
	if create {
		if item.FileURL == nil {
			return invalid("document", "file_url est obligatoire")
		}
		return ValidateMedicalDocumentFileURL(*item.FileURL)
	}
	if item.FileURL == nil {
		return nil
	}
	return ValidateMedicalDocumentFileURL(*item.FileURL)
}

func validateDocumentLabelTypeWrite(item MedicalDocumentRequest, create bool) error {
	if create {
		if item.Label == nil || strings.TrimSpace(*item.Label) == "" {
			return invalid("document", "label et type sont obligatoires")
		}
		if item.Type == nil || strings.TrimSpace(*item.Type) == "" {
			return invalid("document", "label et type sont obligatoires")
		}
		if utf8.RuneCountInString(strings.TrimSpace(*item.Label)) > MedicalDocumentLabelMaxLen {
			return invalid("document", "label trop long")
		}
		if utf8.RuneCountInString(strings.TrimSpace(*item.Type)) > MedicalDocumentTypeMaxLen {
			return invalid("document", "type trop long")
		}
		return nil
	}
	if item.Label != nil {
		trimmed := strings.TrimSpace(*item.Label)
		if trimmed == "" {
			return invalid("document", "label invalide")
		}
		if utf8.RuneCountInString(trimmed) > MedicalDocumentLabelMaxLen {
			return invalid("document", "label trop long")
		}
	}
	if item.Type != nil {
		trimmed := strings.TrimSpace(*item.Type)
		if trimmed == "" {
			return invalid("document", "type invalide")
		}
		if utf8.RuneCountInString(trimmed) > MedicalDocumentTypeMaxLen {
			return invalid("document", "type trop long")
		}
	}
	return nil
}

func normalizeDocumentLabelType(item *MedicalDocumentRequest) {
	if item.Label != nil {
		trimmed := strings.TrimSpace(*item.Label)
		item.Label = &trimmed
	}
	if item.Type != nil {
		trimmed := strings.TrimSpace(*item.Type)
		item.Type = &trimmed
	}
	if item.FileURL != nil {
		trimmed := strings.TrimSpace(*item.FileURL)
		item.FileURL = &trimmed
	}
}
