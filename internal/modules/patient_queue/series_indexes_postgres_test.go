package patient_queue

import (
	"strings"
	"testing"
	"time"
)

func TestSeriesPairCheckDefMatchesContract(t *testing.T) {
	good := `CHECK ((((series_id IS NULL) AND (series_occurrence_index IS NULL)) OR ((series_id IS NOT NULL) AND (series_occurrence_index IS NOT NULL) AND (series_occurrence_index > 0))))`
	if !seriesPairCheckDefMatchesContract(good) {
		t.Fatal("canonical def must match")
	}
	if !seriesPairCheckDefMatchesContract("  " + good + "\n") {
		t.Fatal("whitespace-insensitive match required")
	}
	weaker := []string{
		`CHECK (true)`,
		`CHECK (series_id IS NULL OR series_occurrence_index IS NULL)`,
		`CHECK ((series_id IS NULL AND series_occurrence_index IS NULL))`,                                                                              // missing both-set branch
		`CHECK ((series_id IS NOT NULL AND series_occurrence_index IS NOT NULL))`,                                                                      // missing >0 and both-null
		`CHECK ((((series_id IS NULL) AND (series_occurrence_index IS NULL)) OR ((series_id IS NOT NULL) AND (series_occurrence_index IS NOT NULL))))`, // missing > 0
	}
	for _, w := range weaker {
		if seriesPairCheckDefMatchesContract(w) {
			t.Fatalf("weaker def must not match: %s", w)
		}
	}
}

func TestPostgresEnsureAppointmentSeriesPairCheckContract23OA(t *testing.T) {
	db := queuePostgres(t)

	// Correct constraint from Ensure must pass semantic assert (not name-only).
	if err := assertAppointmentSeriesPairCheck(db); err != nil {
		t.Fatalf("fresh Ensure must install correct CHECK: %v", err)
	}
	ok, err := appointmentSeriesPairCheckMatchesContract(db)
	if err != nil || !ok {
		t.Fatalf("contract match after Ensure: ok=%v err=%v", ok, err)
	}

	// Missing constraint is recreated.
	if err := dropAppointmentSeriesPairCheck(db); err != nil {
		t.Fatal(err)
	}
	ok, err = appointmentSeriesPairCheckMatchesContract(db)
	if err != nil || ok {
		t.Fatalf("after drop want no contract match, ok=%v err=%v", ok, err)
	}
	if err := EnsureAppointmentSeriesIndexes(db); err != nil {
		t.Fatalf("Ensure must recreate missing CHECK: %v", err)
	}
	if err := assertAppointmentSeriesPairCheck(db); err != nil {
		t.Fatal(err)
	}

	// Deliberately wrong same-name constraint must be detected and replaced.
	if err := dropAppointmentSeriesPairCheck(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE patient_queue_appointments
		ADD CONSTRAINT chk_pq_appt_series_pair CHECK (true)`).Error; err != nil {
		t.Fatalf("seed weak CHECK: %v", err)
	}
	var weakDef string
	if err := db.Raw(appointmentSeriesPairCheckDefSQL).Scan(&weakDef).Error; err != nil {
		t.Fatal(err)
	}
	if seriesPairCheckDefMatchesContract(weakDef) {
		t.Fatalf("weak CHECK(true) must not satisfy contract: %s", weakDef)
	}
	ok, err = appointmentSeriesPairCheckMatchesContract(db)
	if err != nil || ok {
		t.Fatalf("name-present weak CHECK must fail contract, ok=%v err=%v", ok, err)
	}
	if err := EnsureAppointmentSeriesIndexes(db); err != nil {
		t.Fatalf("Ensure must replace weak same-name CHECK: %v", err)
	}
	if err := assertAppointmentSeriesPairCheck(db); err != nil {
		t.Fatalf("after replace: %v", err)
	}
	var fixedDef string
	if err := db.Raw(appointmentSeriesPairCheckDefSQL).Scan(&fixedDef).Error; err != nil {
		t.Fatal(err)
	}
	if !seriesPairCheckDefMatchesContract(fixedDef) {
		t.Fatalf("replaced def still wrong: %s", fixedDef)
	}

	// Invalid pair values rejected by PostgreSQL.
	now := time.Now().UTC()
	base := Appointment{
		PatientID: 1, ServiceID: 10, ScheduledAt: now, Status: ApptScheduled,
		CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	// both null OK
	okRow := base
	okRow.ID = 0
	if err := db.Create(&okRow).Error; err != nil {
		t.Fatalf("both-null must be allowed: %v", err)
	}

	rejectCases := []struct {
		name string
		mut  func(*Appointment)
	}{
		{"series_id only", func(a *Appointment) { sid := uint(1); a.SeriesID = &sid }},
		{"index only", func(a *Appointment) { idx := 1; a.SeriesOccurrenceIndex = &idx }},
		{"index zero", func(a *Appointment) {
			// Need a real series row for FK if series_id set — create series first.
		}},
	}
	for _, tc := range rejectCases {
		if tc.name == "index zero" {
			continue
		}
		row := base
		row.ID = 0
		tc.mut(&row)
		err := db.Create(&row).Error
		if err == nil {
			t.Fatalf("%s: expected CHECK rejection", tc.name)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "chk_pq_appt_series_pair") &&
			!strings.Contains(strings.ToLower(err.Error()), "check") {
			t.Fatalf("%s: want check violation, got %v", tc.name, err)
		}
	}

	// Valid series + index>0, then reject index=0 with series_id set.
	series := AppointmentSeries{
		PatientID: 1, ServiceID: 10, PractitionerID: 100, Freq: SeriesFreqWeekly,
		IntervalWeeks: 1, ByWeekdays: "[1]", Timezone: "UTC",
		AnchorStartAt: now, DurationMinutes: 30, Status: SeriesStatusActive,
		CreatedBy: 1, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&series).Error; err != nil {
		t.Fatalf("seed series: %v", err)
	}
	sid := series.ID
	idx1 := 1
	goodOcc := base
	goodOcc.ID = 0
	goodOcc.SeriesID = &sid
	goodOcc.SeriesOccurrenceIndex = &idx1
	if err := db.Create(&goodOcc).Error; err != nil {
		t.Fatalf("valid pair must be allowed: %v", err)
	}
	idx0 := 0
	badOcc := base
	badOcc.ID = 0
	badOcc.SeriesID = &sid
	badOcc.SeriesOccurrenceIndex = &idx0
	err = db.Create(&badOcc).Error
	if err == nil {
		t.Fatal("index 0 with series_id must be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "chk_pq_appt_series_pair") &&
		!strings.Contains(strings.ToLower(err.Error()), "check") {
		t.Fatalf("index 0: want check violation, got %v", err)
	}
}
