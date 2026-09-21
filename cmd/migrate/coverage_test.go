package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
)

func TestSchemaModelsIncludeAppointmentSeries(t *testing.T) {
	t.Parallel()
	found := false
	for _, m := range schemaModels() {
		if _, ok := m.(*patient_queue.AppointmentSeries); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("schemaModels must include *patient_queue.AppointmentSeries")
	}
}

func TestSchemaModelsIncludeNotificationTables(t *testing.T) {
	t.Parallel()
	var intent, attempt bool
	for _, m := range schemaModels() {
		switch m.(type) {
		case *patient_queue.AppointmentNotificationIntent:
			intent = true
		case *patient_queue.AppointmentNotificationAttempt:
			attempt = true
		}
	}
	if !intent || !attempt {
		t.Fatalf("notification models missing: intent=%v attempt=%v", intent, attempt)
	}
}

func TestSchemaModelsAppointmentSeriesBeforeAppointment(t *testing.T) {
	t.Parallel()
	seriesIdx, apptIdx := -1, -1
	for i, m := range schemaModels() {
		switch m.(type) {
		case *patient_queue.AppointmentSeries:
			seriesIdx = i
		case *patient_queue.Appointment:
			if apptIdx < 0 {
				apptIdx = i
			}
		}
	}
	if seriesIdx < 0 || apptIdx < 0 {
		t.Fatal("AppointmentSeries and Appointment must both be in schemaModels")
	}
	if seriesIdx > apptIdx {
		t.Fatalf("AppointmentSeries index %d must precede Appointment index %d", seriesIdx, apptIdx)
	}
}

func TestApplyMigrationsIncludesRequiredEnsureHelpers(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(file), "schema.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	for _, name := range []string{
		"EnsureAppointmentIndexes",
		"EnsureScheduleIndexes",
		"EnsureAppointmentSeriesIndexes",
		"EnsureTicketIndexes",
		"EnsureNotificationIndexes",
	} {
		if !strings.Contains(body, "patient_queue."+name) {
			t.Fatalf("applyMigrations must call patient_queue.%s", name)
		}
	}
	// Fail-closed: EnsureTicketIndexes errors must be returned (not swallowed).
	if !strings.Contains(body, `return fmt.Errorf("EnsureTicketIndexes: %w", err)`) {
		t.Fatal("applyMigrations must propagate EnsureTicketIndexes failures")
	}
}

func TestMainFailsClosedOnApplyMigrationsError(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(file), "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "applyMigrations(db)") {
		t.Fatal("main must call applyMigrations")
	}
	if !strings.Contains(body, "log.Fatal") {
		t.Fatal("main must fail closed via log.Fatal on migration error")
	}
}
