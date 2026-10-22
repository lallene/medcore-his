package help

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
)

// PublicEntry is the API projection: no internal id, no permission list.
type PublicEntry struct {
	HelpKey      string `json:"helpKey"`
	Module       string `json:"module"`
	Page         string `json:"page"`
	ElementType  string `json:"elementType"`
	ElementLabel string `json:"elementLabel"`
	ShortHelp    string `json:"shortHelp"`
	DetailedHelp string `json:"detailedHelp,omitempty"`
	ActionHelp   string `json:"actionHelp,omitempty"`
	SortOrder    int    `json:"sortOrder"`
}

// CatalogResponse is the whole visible catalog, loaded once by the frontend.
type CatalogResponse struct {
	Version string        `json:"version"`
	Entries []PublicEntry `json:"entries"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// Visible returns active entries the caller may see: no required permission, or any-of
// the caller's effective permissions (backend RBAC authority; "*" sees everything).
func (s *Service) Visible(permissions []string) (*CatalogResponse, error) {
	var rows []Entry
	if err := s.db.Where("is_active").Order("module, sort_order, help_key").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := &CatalogResponse{Entries: make([]PublicEntry, 0, len(rows))}
	digest := sha256.New()
	for _, r := range rows {
		required := r.Permissions()
		if len(required) > 0 && !rbac.HasAnyPermission(permissions, required...) {
			continue
		}
		out.Entries = append(out.Entries, PublicEntry{
			HelpKey: r.HelpKey, Module: r.Module, Page: r.Page, ElementType: r.ElementType,
			ElementLabel: r.ElementLabel, ShortHelp: r.ShortHelp, DetailedHelp: r.DetailedHelp,
			ActionHelp: r.ActionHelp, SortOrder: r.SortOrder,
		})
		digest.Write([]byte(r.HelpKey))
		digest.Write([]byte{0})
		digest.Write([]byte(r.UpdatedAt.UTC().Format("20060102150405.000000")))
	}
	perms := append([]string(nil), permissions...)
	sort.Strings(perms)
	digest.Write([]byte(strings.Join(perms, ",")))
	out.Version = hex.EncodeToString(digest.Sum(nil))[:20]
	return out, nil
}
