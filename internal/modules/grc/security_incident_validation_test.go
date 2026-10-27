package grc

import (
	"strings"
	"testing"
	"time"
)

func TestSecurityIncidentValidation(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	detected := now.Add(-2 * time.Hour)
	base := func() SecurityIncidentRequest {
		return SecurityIncidentRequest{
			Reference:  "INC-1",
			Title:      "Panne d'accès",
			Severity:   SecurityIncidentSeverityHigh,
			DetectedAt: detected,
		}
	}

	t.Run("reference required", func(t *testing.T) {
		r := base()
		r.Reference = "   "
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)
	})

	t.Run("reference max 60", func(t *testing.T) {
		r := base()
		r.Reference = strings.Repeat("A", 60)
		normalizeSecurityIncidentRequest(&r)
		if err := validateSecurityIncidentRequest(r, now, true); err != nil {
			t.Fatal(err)
		}
		r.Reference = strings.Repeat("A", 61)
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)
	})

	t.Run("title required and max 220", func(t *testing.T) {
		r := base()
		r.Title = "  "
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)

		r = base()
		r.Title = strings.Repeat("é", 220)
		normalizeSecurityIncidentRequest(&r)
		if err := validateSecurityIncidentRequest(r, now, true); err != nil {
			t.Fatal(err)
		}
		r.Title = strings.Repeat("é", 221)
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)
	})

	t.Run("severity", func(t *testing.T) {
		for _, severity := range []string{"low", "MEDIUM", " high ", "CRITICAL"} {
			r := base()
			r.Severity = severity
			normalizeSecurityIncidentRequest(&r)
			if err := validateSecurityIncidentRequest(r, now, true); err != nil {
				t.Fatalf("%s: %v", severity, err)
			}
		}
		r := base()
		r.Severity = "URGENT"
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)
	})

	t.Run("detectedAt", func(t *testing.T) {
		r := base()
		r.DetectedAt = time.Time{}
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)

		r = base()
		r.DetectedAt = now
		normalizeSecurityIncidentRequest(&r)
		if err := validateSecurityIncidentRequest(r, now, true); err != nil {
			t.Fatal(err)
		}

		r.DetectedAt = now.Add(time.Second)
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)
	})

	t.Run("occurredAt", func(t *testing.T) {
		r := base()
		occurred := detected
		r.OccurredAt = &occurred
		normalizeSecurityIncidentRequest(&r)
		if err := validateSecurityIncidentRequest(r, now, true); err != nil {
			t.Fatal(err)
		}
		later := detected.Add(time.Second)
		r.OccurredAt = &later
		normalizeSecurityIncidentRequest(&r)
		requireServiceError(t, validateSecurityIncidentRequest(r, now, true), 400)
	})
}

func TestSecurityIncidentLifecycle(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	allowed := [][2]string{
		{SecurityIncidentStatusOpen, SecurityIncidentStatusOpen},
		{SecurityIncidentStatusOpen, SecurityIncidentStatusInvestigating},
		{SecurityIncidentStatusInvestigating, SecurityIncidentStatusInvestigating},
		{SecurityIncidentStatusInvestigating, SecurityIncidentStatusContained},
		{SecurityIncidentStatusInvestigating, SecurityIncidentStatusResolved},
		{SecurityIncidentStatusContained, SecurityIncidentStatusContained},
		{SecurityIncidentStatusContained, SecurityIncidentStatusInvestigating},
		{SecurityIncidentStatusContained, SecurityIncidentStatusResolved},
		{SecurityIncidentStatusResolved, SecurityIncidentStatusResolved},
		{SecurityIncidentStatusResolved, SecurityIncidentStatusInvestigating},
		{SecurityIncidentStatusResolved, SecurityIncidentStatusClosed},
		{SecurityIncidentStatusClosed, SecurityIncidentStatusClosed},
	}
	for _, pair := range allowed {
		if err := validateSecurityIncidentTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("%s -> %s: %v", pair[0], pair[1], err)
		}
	}

	denied := [][2]string{
		{SecurityIncidentStatusOpen, SecurityIncidentStatusContained},
		{SecurityIncidentStatusOpen, SecurityIncidentStatusResolved},
		{SecurityIncidentStatusOpen, SecurityIncidentStatusClosed},
		{SecurityIncidentStatusInvestigating, SecurityIncidentStatusOpen},
		{SecurityIncidentStatusInvestigating, SecurityIncidentStatusClosed},
		{SecurityIncidentStatusContained, SecurityIncidentStatusOpen},
		{SecurityIncidentStatusContained, SecurityIncidentStatusClosed},
		{SecurityIncidentStatusResolved, SecurityIncidentStatusOpen},
		{SecurityIncidentStatusResolved, SecurityIncidentStatusContained},
		{SecurityIncidentStatusClosed, SecurityIncidentStatusOpen},
		{SecurityIncidentStatusClosed, SecurityIncidentStatusInvestigating},
		{SecurityIncidentStatusClosed, SecurityIncidentStatusResolved},
	}
	for _, pair := range denied {
		requireServiceError(t, validateSecurityIncidentTransition(pair[0], pair[1]), 409)
	}

	resolved, closed := securityIncidentTimestamps(SecurityIncidentStatusInvestigating, SecurityIncidentStatusResolved, nil, now)
	if resolved == nil || !resolved.Equal(now) || closed != nil {
		t.Fatalf("first resolution: resolved=%v closed=%v", resolved, closed)
	}
	kept, closed := securityIncidentTimestamps(SecurityIncidentStatusResolved, SecurityIncidentStatusResolved, resolved, later)
	if kept == nil || !kept.Equal(now) || closed != nil {
		t.Fatalf("staying resolved must keep the instant: %v %v", kept, closed)
	}
	cleared, closed := securityIncidentTimestamps(SecurityIncidentStatusResolved, SecurityIncidentStatusInvestigating, resolved, later)
	if cleared != nil || closed != nil {
		t.Fatalf("reopen must clear timestamps: %v %v", cleared, closed)
	}
	again, closed := securityIncidentTimestamps(SecurityIncidentStatusInvestigating, SecurityIncidentStatusResolved, nil, later)
	if again == nil || !again.Equal(later) || closed != nil {
		t.Fatalf("new resolution must record the new instant: %v", again)
	}
	for _, status := range []string{SecurityIncidentStatusOpen, SecurityIncidentStatusContained} {
		resolvedAt, closedAt := securityIncidentTimestamps(SecurityIncidentStatusResolved, status, resolved, later)
		if resolvedAt != nil || closedAt != nil {
			t.Fatalf("%s must clear both timestamps", status)
		}
	}
}
