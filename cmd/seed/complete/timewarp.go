package complete

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Time re-anchoring ("timewarp").
//
// MedCore services stamp every row with time.Now() and expose no injectable clock. To
// obtain one year of history WITHOUT bypassing the services, each step runs inside a
// window [rs, re) of real time associated with its simulated instant `sim`. After every
// simulated day, all temporal columns written during that day's windows are rewritten:
//
//	ts in [rs, re)              -> sim + (ts - rs)          (causal order preserved)
//	ts >= batch start, no window -> ts + offset of the window that wrote the row
//	                                (values computed from now(): due dates, SLA deadlines...)
//	ts < batch start            -> unchanged (already re-anchored, or explicit past input)
//
// DATE columns are shifted by the same number of days as the row's write instant.
// Columns that hold explicit planned values supplied by the seeder (appointment slots,
// validity periods, birth dates...) are never rewritten: they are given in simulated time.
// This is the only systematic direct write performed on business tables, and it never
// changes a status, an amount, a relation or a number.

// plannedColumns are inputs given in simulated time by the seeder: never re-anchored.
var plannedColumns = map[string]bool{
	"patients.date_naissance":                             true,
	"patient_queue_appointments.scheduled_at":             true,
	"patient_queue_appointments.scheduled_end_at":         true,
	"patient_queue_appointment_series.anchor_start_at":    true,
	"patient_queue_appointment_series.until":              true,
	"patient_queue_schedule_exceptions.start_at":          true,
	"patient_queue_schedule_exceptions.end_at":            true,
	"patient_queue_staff_working_schedules.valid_from":    true,
	"patient_queue_staff_working_schedules.valid_until":   true,
	"patient_coverages.valid_from":                        true,
	"patient_coverages.valid_to":                          true,
	"billing_tariffs.effective_from":                      true,
	"billing_tariffs.effective_to":                        true,
	"pharmacy_batches.expiration_date":                    true,
	"hospitalizations.expected_discharge_at":              true,
	"imaging_orders.scheduled_at":                         true,
	"insurance_submission_batches.period_from":            true,
	"insurance_submission_batches.period_to":              true,
	"vaccinations.vaccination_date":                       true,
	"vaccinations.next_booster_date":                      true,
	"medical_histories.start_date":                        true,
	"medical_histories.end_date":                          true,
	"surgical_histories.procedure_date":                   true,
	"medical_devices.implantation_date":                   true,
	"regular_treatments.start_date":                       true,
	"medical_documents.document_date":                     true,
	"patient_receivable_follow_ups.promised_payment_date": true,
	"insurance_authorizations.external_decision_date":     true,
	"vital_signs.measured_at":                             true,
}

type warpTable struct {
	name      string
	tsCols    []string
	dateCols  []string
	anchorCol string // updated_at, else created_at, else ""
}

// Timewarp records step windows and re-anchors the temporal columns.
type Timewarp struct {
	db      *gorm.DB
	loc     string
	tables  []warpTable
	windows []window
	lastRe  time.Time
	Rows    int64 // rows rewritten (statistics)
}

type window struct{ rs, re, sim time.Time }

// NewTimewarp discovers every temporal column of the current schema.
func NewTimewarp(db *gorm.DB, loc *time.Location) (*Timewarp, error) {
	type col struct{ TableName, ColumnName, DataType string }
	var cols []col
	if err := db.Raw(`SELECT table_name, column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND data_type IN ('date','timestamp with time zone','timestamp without time zone')
		ORDER BY table_name, column_name`).Scan(&cols).Error; err != nil {
		return nil, err
	}
	byTable := map[string]*warpTable{}
	all := map[string]map[string]bool{}
	for _, c := range cols {
		if all[c.TableName] == nil {
			all[c.TableName] = map[string]bool{}
		}
		all[c.TableName][c.ColumnName] = true
		if c.ColumnName == "deleted_at" || plannedColumns[c.TableName+"."+c.ColumnName] || strings.HasPrefix(c.TableName, "ui_help") {
			continue
		}
		t := byTable[c.TableName]
		if t == nil {
			t = &warpTable{name: c.TableName}
			byTable[c.TableName] = t
		}
		if c.DataType == "date" {
			t.dateCols = append(t.dateCols, c.ColumnName)
		} else {
			t.tsCols = append(t.tsCols, c.ColumnName)
		}
	}
	tw := &Timewarp{db: db, loc: loc.String()}
	names := make([]string, 0, len(byTable))
	for n := range byTable {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := byTable[n]
		switch {
		case all[n]["updated_at"]:
			t.anchorCol = "updated_at"
		case all[n]["created_at"]:
			t.anchorCol = "created_at"
		}
		tw.tables = append(tw.tables, *t)
	}
	return tw, nil
}

// Run executes fn inside a window bound to sim. Bounds are kept at PostgreSQL's
// microsecond resolution and strictly increasing; the next window starts exactly where the
// previous one ended.
func (tw *Timewarp) Run(sim time.Time, fn func() error) error {
	rs := time.Now().Truncate(time.Microsecond)
	if rs.Before(tw.lastRe) {
		rs = tw.lastRe
	}
	err := fn()
	re := time.Now().Truncate(time.Microsecond)
	if !re.After(rs) {
		re = rs.Add(time.Microsecond)
	}
	tw.lastRe = re
	tw.windows = append(tw.windows, window{rs, re, sim})
	return err
}

// Flush re-anchors everything written in the recorded windows, then forgets them.
func (tw *Timewarp) Flush() error {
	if len(tw.windows) == 0 {
		return nil
	}
	ws := tw.windows
	tw.windows = nil
	return tw.db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec(`CREATE TEMP TABLE IF NOT EXISTS dc_windows (rs timestamptz PRIMARY KEY, re timestamptz NOT NULL, sim timestamptz NOT NULL)`).Error; err != nil {
			return err
		}
		if err := conn.Exec(`TRUNCATE dc_windows`).Error; err != nil {
			return err
		}
		for i := 0; i < len(ws); i += 500 {
			end := i + 500
			if end > len(ws) {
				end = len(ws)
			}
			var sb strings.Builder
			args := make([]any, 0, (end-i)*3)
			sb.WriteString("INSERT INTO dc_windows (rs, re, sim) VALUES ")
			for k, w := range ws[i:end] {
				if k > 0 {
					sb.WriteString(",")
				}
				sb.WriteString("(?,?,?)")
				args = append(args, w.rs, w.re, w.sim)
			}
			if err := conn.Exec(sb.String(), args...).Error; err != nil {
				return err
			}
		}
		if err := conn.Exec(`CREATE OR REPLACE FUNCTION pg_temp.dc_ts(ts timestamptz, anchor timestamptz) RETURNS timestamptz LANGUAGE sql STABLE AS $$
			SELECT CASE WHEN ts IS NULL OR ts < (SELECT min(rs) FROM dc_windows) THEN ts
			ELSE COALESCE(
				(SELECT w.sim + (ts - w.rs) FROM dc_windows w WHERE w.rs <= ts AND ts < w.re ORDER BY w.rs DESC LIMIT 1),
				ts + COALESCE(
					(SELECT w.sim - w.rs FROM dc_windows w WHERE anchor IS NOT NULL AND w.rs <= anchor AND anchor < w.re ORDER BY w.rs DESC LIMIT 1),
					(SELECT w.sim - w.rs FROM dc_windows w WHERE w.rs <= ts ORDER BY w.rs DESC LIMIT 1),
					interval '0'))
			END $$`).Error; err != nil {
			return err
		}
		bstart := ws[0].rs
		for _, t := range tw.tables {
			if err := tw.flushTable(conn, t, bstart); err != nil {
				return fmt.Errorf("timewarp %s: %w", t.name, err)
			}
		}
		return nil
	})
}

func (tw *Timewarp) flushTable(conn *gorm.DB, t warpTable, bstart time.Time) error {
	var sets, conds []string
	anchor := "NULL::timestamptz"
	if t.anchorCol != "" {
		anchor = quoteIdent(t.anchorCol)
	}
	for _, c := range t.tsCols {
		q := quoteIdent(c)
		sets = append(sets, fmt.Sprintf("%s = pg_temp.dc_ts(%s, %s)", q, q, anchor))
		conds = append(conds, fmt.Sprintf("%s >= @b", q))
	}
	if t.anchorCol != "" {
		a := quoteIdent(t.anchorCol)
		for _, c := range t.dateCols {
			q := quoteIdent(c)
			sets = append(sets, fmt.Sprintf(`%s = CASE WHEN %s >= (@b AT TIME ZONE @tz)::date AND %s >= @b
				THEN %s + ((pg_temp.dc_ts(%s, %s) AT TIME ZONE @tz)::date - (%s AT TIME ZONE @tz)::date) ELSE %s END`,
				q, q, a, q, a, a, a, q))
			conds = append(conds, fmt.Sprintf("%s >= (@b AT TIME ZONE @tz)::date", q))
		}
	}
	if len(sets) == 0 {
		return nil
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", quoteIdent(t.name), strings.Join(sets, ", "), strings.Join(conds, " OR "))
	res := conn.Exec(stmt, sql.Named("b", bstart), sql.Named("tz", tw.loc))
	tw.Rows += res.RowsAffected
	return res.Error
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
