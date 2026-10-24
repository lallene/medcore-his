// Package period lit les filtres d'activité partagés par les indicateurs du
// tableau de bord : service et période civile demi-ouverte [from, to).
// Les dates sont des dates civiles YYYY-MM-DD, interprétées par PostgreSQL dans
// le fuseau métier de la session (MEDCORE_BUSINESS_TIMEZONE).
package period

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

type Filter struct {
	ServiceID *uint
	From      string
	To        string
}

// HasPeriod indique qu'au moins une borne de date est fournie.
func (f Filter) HasPeriod() bool {
	return f.From != "" || f.To != ""
}

// Parse lit serviceId, from et to ; tous sont optionnels.
func Parse(c *gin.Context) (Filter, error) {
	filter := Filter{}

	if raw := c.Query("serviceId"); raw != "" {
		serviceID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || serviceID == 0 {
			return Filter{}, coreerrors.BadRequest("serviceId invalide")
		}
		value := uint(serviceID)
		filter.ServiceID = &value
	}

	if raw := c.Query("from"); raw != "" {
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return Filter{}, coreerrors.BadRequest("from doit être au format YYYY-MM-DD")
		}
		filter.From = raw
	}

	if raw := c.Query("to"); raw != "" {
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return Filter{}, coreerrors.BadRequest("to doit être au format YYYY-MM-DD")
		}
		filter.To = raw
	}

	if filter.From != "" && filter.To != "" && filter.From >= filter.To {
		return Filter{}, coreerrors.BadRequest("from doit être antérieur à to")
	}

	return filter, nil
}

// Apply ajoute le service et la période civile [from,to) à une requête.
// Les colonnes sont des constantes internes, jamais des entrées utilisateur.
func (f Filter) Apply(query *gorm.DB, dateColumn, serviceColumn string) *gorm.DB {
	if f.ServiceID != nil {
		query = query.Where(serviceColumn+" = ?", *f.ServiceID)
	}
	if f.From != "" {
		query = query.Where(dateColumn+" >= ?::date", f.From)
	}
	if f.To != "" {
		query = query.Where(dateColumn+" < ?::date", f.To)
	}
	return query
}
