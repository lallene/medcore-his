package patient_queue

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// ListNotificationIntentsAdmin — GET /api/appointment-notification-intents (LOT 23N-C1).
func (h *Handler) ListNotificationIntentsAdmin(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)

	f, err := parseNotificationAdminListFilter(c)
	if err != nil {
		fail(c, err)
		return
	}
	out, err := h.service.ListNotificationIntentsAdmin(f, a)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

// GetNotificationIntentAdmin — GET /api/appointment-notification-intents/:id
func (h *Handler) GetNotificationIntentAdmin(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	out, err := h.service.GetNotificationIntentAdmin(n, a)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

// ListNotificationAttemptsAdmin — GET /api/appointment-notification-intents/:id/attempts
func (h *Handler) ListNotificationAttemptsAdmin(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	out, err := h.service.ListNotificationAttemptsAdmin(n, a)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"items": out})
}

func parseNotificationAdminListFilter(c *gin.Context) (NotificationAdminListFilter, error) {
	page, err := parseNotificationAdminPageQuery(c.Query("page"))
	if err != nil {
		return NotificationAdminListFilter{}, err
	}
	limit, err := parseNotificationAdminLimitQuery(c.Query("limit"))
	if err != nil {
		return NotificationAdminListFilter{}, err
	}
	f := NotificationAdminListFilter{
		Status:  trimOptionalQuery(c.Query("status")),
		Kind:    trimOptionalQuery(c.Query("kind")),
		Channel: trimOptionalQuery(c.Query("channel")),
		Page:    page,
		Limit:   limit,
	}

	apptID, err := parseUintQuery(c.Query("appointmentId"))
	if err != nil {
		return f, err
	}
	f.AppointmentID = apptID

	if raw := trimOptionalQuery(c.Query("sendAfterFrom")); raw != "" {
		ts, err := parseRFC3339Query(raw, "sendAfterFrom")
		if err != nil {
			return f, err
		}
		f.SendAfterFrom = &ts
	}
	if raw := trimOptionalQuery(c.Query("sendAfterTo")); raw != "" {
		ts, err := parseRFC3339Query(raw, "sendAfterTo")
		if err != nil {
			return f, err
		}
		f.SendAfterTo = &ts
	}
	if raw := trimOptionalQuery(c.Query("createdAtFrom")); raw != "" {
		ts, err := parseRFC3339Query(raw, "createdAtFrom")
		if err != nil {
			return f, err
		}
		f.CreatedAtFrom = &ts
	}
	if raw := trimOptionalQuery(c.Query("createdAtTo")); raw != "" {
		ts, err := parseRFC3339Query(raw, "createdAtTo")
		if err != nil {
			return f, err
		}
		f.CreatedAtTo = &ts
	}
	return f, nil
}

func parseRFC3339Query(raw, field string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		ts, err = time.Parse(time.RFC3339Nano, raw)
	}
	if err != nil {
		return time.Time{}, coreerrors.BadRequest(field + " invalide (RFC3339)")
	}
	return ts.UTC(), nil
}

// parseNotificationAdminPageQuery — absent → 1; explicit malformed/out-of-range → 400 (no silent normalize).
func parseNotificationAdminPageQuery(raw string) (int, error) {
	raw = trimOptionalQuery(raw)
	if raw == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(raw)
	if err != nil {
		return 0, coreerrors.BadRequest("page invalide")
	}
	if page < 1 {
		return 0, coreerrors.BadRequest("page doit être ≥ 1")
	}
	return page, nil
}

// parseNotificationAdminLimitQuery — absent → 50; explicit malformed/out-of-range → 400 (no silent normalize).
func parseNotificationAdminLimitQuery(raw string) (int, error) {
	raw = trimOptionalQuery(raw)
	if raw == "" {
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, coreerrors.BadRequest("limit invalide")
	}
	if limit < 1 {
		return 0, coreerrors.BadRequest("limit doit être ≥ 1")
	}
	if limit > 100 {
		return 0, coreerrors.BadRequest("limit doit être ≤ 100")
	}
	return limit, nil
}
