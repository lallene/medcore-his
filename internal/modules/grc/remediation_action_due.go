package grc

import (
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// Remediation action due state — the only definition, shared by the action
// registry and the GRC dashboard. It is derived at read time from the
// status, the due date and one instant; it is never stored:
//   - CLOSED: terminal status (DONE or CANCELLED), whatever the due date;
//   - NO_DUE_DATE: open action without due date;
//   - OVERDUE: open and due_at < now (a due date exactly at now is not
//     overdue yet, see RemediationActionOverdue);
//   - DUE_SOON: open and now <= due_at <= now + window;
//   - UPCOMING: open and due_at > now + window.
//
// Open statuses are OPEN, IN_PROGRESS and BLOCKED. The dashboard figures
// map onto these states: open = every non-CLOSED action, overdue = OVERDUE,
// dueSoon = DUE_SOON.
const (
	RemediationActionDueClosed    = "CLOSED"
	RemediationActionDueNoDueDate = "NO_DUE_DATE"
	RemediationActionDueOverdue   = "OVERDUE"
	RemediationActionDueSoon      = "DUE_SOON"
	RemediationActionDueUpcoming  = "UPCOMING"
)

var remediationActionDueStates = []string{
	RemediationActionDueOverdue, RemediationActionDueSoon, RemediationActionDueUpcoming,
	RemediationActionDueNoDueDate, RemediationActionDueClosed,
}

// remediationActionDueSoonSQL mirrors DUE_SOON; bind (now, now + window,
// terminalActionStatuses).
const remediationActionDueSoonSQL = "(due_at IS NOT NULL AND due_at >= ? AND due_at <= ? AND status NOT IN ?)"

func isTerminalActionStatus(status string) bool {
	return status == RemediationActionStatusDone || status == RemediationActionStatusCancelled
}

// RemediationActionDueState classifies one action at now with the window end.
func RemediationActionDueState(status string, dueAt *time.Time, now, soonLimit time.Time) string {
	switch {
	case isTerminalActionStatus(status):
		return RemediationActionDueClosed
	case dueAt == nil:
		return RemediationActionDueNoDueDate
	case RemediationActionOverdue(status, dueAt, now):
		return RemediationActionDueOverdue
	case !dueAt.After(soonLimit):
		return RemediationActionDueSoon
	default:
		return RemediationActionDueUpcoming
	}
}

// remediationActionDueCondition returns the SQL condition of one state, with
// the same boundaries as RemediationActionDueState.
func remediationActionDueCondition(state string, now, soonLimit time.Time) (string, []any, error) {
	switch state {
	case RemediationActionDueClosed:
		return "status IN ?", []any{terminalActionStatuses}, nil
	case RemediationActionDueNoDueDate:
		return "(due_at IS NULL AND status NOT IN ?)", []any{terminalActionStatuses}, nil
	case RemediationActionDueOverdue:
		return remediationActionOverdueSQL, []any{now, RemediationActionStatusDone, RemediationActionStatusCancelled}, nil
	case RemediationActionDueSoon:
		return remediationActionDueSoonSQL, []any{now, soonLimit, terminalActionStatuses}, nil
	case RemediationActionDueUpcoming:
		return "(due_at IS NOT NULL AND due_at > ? AND status NOT IN ?)", []any{soonLimit, terminalActionStatuses}, nil
	default:
		return "", nil, coreerrors.BadRequest("État d'échéance invalide")
	}
}

// actionSoonLimit validates the window (same bounds as the dashboard) and
// returns its end.
func actionSoonLimit(now time.Time, days int) (time.Time, error) {
	if err := validateDashboardWindow(days, "dueSoonDays"); err != nil {
		return time.Time{}, err
	}
	return now.AddDate(0, 0, days), nil
}

func fillRemediationActionComputed(a *RemediationAction, now, soonLimit time.Time) {
	a.Overdue = RemediationActionOverdue(a.Status, a.DueAt, now)
	a.DueState = RemediationActionDueState(a.Status, a.DueAt, now, soonLimit)
}
