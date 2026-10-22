package help

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

//go:embed catalog/*.json
var catalogFS embed.FS

// CatalogEntry is the repository-authored form of a help item.
type CatalogEntry struct {
	Key                 string   `json:"key"`
	Module              string   `json:"module"`
	Page                string   `json:"page"`
	ElementType         string   `json:"elementType"`
	Label               string   `json:"label"`
	Short               string   `json:"short"`
	Detailed            string   `json:"detailed"`
	Action              string   `json:"action"`
	RequiredPermissions []string `json:"requiredPermissions"`
	SortOrder           int      `json:"sortOrder"`
}

// Modules documented by the catalog — the MedCore modules really present in the UI.
var Modules = map[string]bool{
	"patients": true, "agenda": true, "queue": true, "consultations": true, "laboratory": true,
	"imaging": true, "pharmacy": true, "hospitalizations": true, "performed_acts": true,
	"billing": true, "cash": true, "insurance": true, "insurance_receivables": true,
	"administration": true, "tickets": true,
}

var (
	helpKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+){1,4}$`)
	// Help is informative only: reject anything that looks like a credential, token,
	// connection string or personal contact data.
	forbiddenContent = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|mot de passe)\s*[:=]`),
		regexp.MustCompile(`(?i)bearer\s+[a-z0-9._-]{10,}`),
		regexp.MustCompile(`(?i)(postgres|postgresql|mysql)://`),
		regexp.MustCompile(`(?i)eyJ[a-z0-9_-]{10,}\.`),
		regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),
		regexp.MustCompile(`\+?\d[\d .-]{8,}\d`),
		regexp.MustCompile(`(?i)(secret|api[_-]?key|client_secret)\s*[:=]`),
	}
	elementTypes = map[string]bool{
		ElementField: true, ElementSelect: true, ElementAction: true, ElementStatus: true,
		ElementSection: true, ElementTableColumn: true, ElementPage: true,
	}
)

// routeOnlyPermissions are enforced by route middleware but absent from the RBAC
// administration catalog (granted through "*" only today). Listed explicitly so the
// help catalog cannot reference an arbitrary permission string.
var routeOnlyPermissions = []string{"insurance.coverage.read"}

const (
	maxShortHelpRunes = 240
	maxLabelRunes     = 200
)

// LoadCatalog reads and validates every embedded catalog file. The version is a
// content hash, stable across runs and used for synchronization and HTTP caching.
func LoadCatalog() ([]CatalogEntry, string, error) {
	files, err := catalogFS.ReadDir("catalog")
	if err != nil {
		return nil, "", err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)
	hash := sha256.New()
	var all []CatalogEntry
	for _, name := range names {
		raw, err := catalogFS.ReadFile(path.Join("catalog", name))
		if err != nil {
			return nil, "", err
		}
		hash.Write([]byte(name))
		hash.Write(raw)
		var entries []CatalogEntry
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&entries); err != nil {
			return nil, "", fmt.Errorf("help catalog %s: %w", name, err)
		}
		all = append(all, entries...)
	}
	if err := ValidateCatalog(all); err != nil {
		return nil, "", err
	}
	return all, hex.EncodeToString(hash.Sum(nil))[:16], nil
}

// ValidateCatalog enforces key format, uniqueness, required texts, known modules,
// element types and permissions, and the absence of secrets / personal data.
func ValidateCatalog(entries []CatalogEntry) error {
	known := map[string]bool{}
	for _, p := range rbac.KnownPermissionKeys() {
		known[p] = true
	}
	for _, p := range routeOnlyPermissions {
		known[p] = true
	}
	seen := map[string]bool{}
	for i, e := range entries {
		where := fmt.Sprintf("entry %d (%s)", i, e.Key)
		if !helpKeyPattern.MatchString(e.Key) {
			return fmt.Errorf("%s: help key invalide", where)
		}
		if seen[e.Key] {
			return fmt.Errorf("%s: help key dupliquée", where)
		}
		seen[e.Key] = true
		if !Modules[e.Module] {
			return fmt.Errorf("%s: module inconnu %q", where, e.Module)
		}
		if !strings.HasPrefix(e.Key, moduleKeyPrefix(e.Module)) {
			return fmt.Errorf("%s: la clé doit commencer par le module", where)
		}
		if !elementTypes[e.ElementType] {
			return fmt.Errorf("%s: element type invalide %q", where, e.ElementType)
		}
		if strings.TrimSpace(e.Page) == "" || !strings.HasPrefix(e.Page, "/") {
			return fmt.Errorf("%s: page obligatoire (route frontend)", where)
		}
		if strings.TrimSpace(e.Label) == "" || utf8.RuneCountInString(e.Label) > maxLabelRunes {
			return fmt.Errorf("%s: libellé obligatoire (%d caractères max)", where, maxLabelRunes)
		}
		if strings.TrimSpace(e.Short) == "" || utf8.RuneCountInString(e.Short) > maxShortHelpRunes {
			return fmt.Errorf("%s: aide courte obligatoire (%d caractères max)", where, maxShortHelpRunes)
		}
		for _, p := range e.RequiredPermissions {
			if !known[p] {
				return fmt.Errorf("%s: permission inconnue %q", where, p)
			}
		}
		for _, text := range []string{e.Label, e.Short, e.Detailed, e.Action} {
			for _, re := range forbiddenContent {
				if re.MatchString(text) {
					return fmt.Errorf("%s: contenu interdit (secret ou donnée personnelle)", where)
				}
			}
		}
	}
	return nil
}

// moduleKeyPrefix maps catalog modules to their key namespace.
func moduleKeyPrefix(module string) string {
	switch module {
	case "consultations":
		return "consultation."
	case "patients":
		return "patient."
	case "hospitalizations":
		return "hospitalization."
	case "performed_acts":
		return "performed_act."
	case "tickets":
		return "ticket."
	}
	return module + "."
}

func (c CatalogEntry) toEntry() Entry {
	perms := append([]string(nil), c.RequiredPermissions...)
	sort.Strings(perms)
	return Entry{
		HelpKey: c.Key, Module: c.Module, Page: c.Page, ElementType: c.ElementType,
		ElementLabel: strings.TrimSpace(c.Label), ShortHelp: strings.TrimSpace(c.Short),
		DetailedHelp: strings.TrimSpace(c.Detailed), ActionHelp: strings.TrimSpace(c.Action),
		RequiredPermissions: strings.Join(perms, ","), IsActive: true, SortOrder: c.SortOrder,
	}
}
