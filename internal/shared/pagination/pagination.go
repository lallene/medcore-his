package pagination

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

type Pagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

type Meta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
}

func FromContext(c *gin.Context) Pagination {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	if page < 1 {
		page = 1
	}

	if limit < 1 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	return Pagination{
		Page:  page,
		Limit: limit,
	}
}

func (p Pagination) Offset() int {
	return (p.Page - 1) * p.Limit
}

func BuildMeta(p Pagination, total int64) Meta {
	totalPages := int((total + int64(p.Limit) - 1) / int64(p.Limit))

	return Meta{
		Page:       p.Page,
		Limit:      p.Limit,
		Total:      total,
		TotalPages: totalPages,
	}
}
