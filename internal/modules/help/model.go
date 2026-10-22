package help

import (
	"strings"
	"time"
)

// Element types documented by the help catalog (LOT AIDE).
const (
	ElementField       = "field"
	ElementSelect      = "select"
	ElementAction      = "action"
	ElementStatus      = "status"
	ElementSection     = "section"
	ElementTableColumn = "table_column"
	ElementPage        = "page"
)

// Entry is one versioned help item. Content is owned by the repository catalog
// (catalog/*.json) and synchronized by cmd/migrate; rows are never deleted, only
// deactivated when a key leaves the catalog.
type Entry struct {
	ID           uint   `gorm:"primaryKey" json:"-"`
	HelpKey      string `gorm:"size:150;not null;uniqueIndex" json:"helpKey"`
	Module       string `gorm:"size:60;not null;index" json:"module"`
	Page         string `gorm:"size:120;not null" json:"page"`
	ElementType  string `gorm:"size:30;not null;check:ui_help_entries_element_type_valid,element_type IN ('field','select','action','status','section','table_column','page')" json:"elementType"`
	ElementLabel string `gorm:"size:200;not null" json:"elementLabel"`
	ShortHelp    string `gorm:"size:240;not null" json:"shortHelp"`
	DetailedHelp string `gorm:"type:text;not null;default:''" json:"detailedHelp,omitempty"`
	ActionHelp   string `gorm:"type:text;not null;default:''" json:"actionHelp,omitempty"`
	// RequiredPermissions is a comma-separated any-of list; empty = every authenticated user.
	RequiredPermissions string    `gorm:"type:text;not null;default:''" json:"-"`
	IsActive            bool      `gorm:"not null;default:true;index" json:"-"`
	SortOrder           int       `gorm:"not null;default:0" json:"sortOrder"`
	CreatedAt           time.Time `json:"-"`
	UpdatedAt           time.Time `json:"-"`
}

func (Entry) TableName() string { return "ui_help_entries" }

// Permissions returns the any-of permission list stored on the row.
func (e Entry) Permissions() []string {
	if strings.TrimSpace(e.RequiredPermissions) == "" {
		return nil
	}
	parts := strings.Split(e.RequiredPermissions, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
