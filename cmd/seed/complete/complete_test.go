package complete

import (
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := ParseConfig(RawConfig{Anchor: "2026-03-15", Patients: "120", Days: "30", Seed: "42"}, time.UTC, time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestParseConfigDefaultsAndBounds(t *testing.T) {
	now := time.Date(2026, 10, 23, 15, 4, 0, 0, time.UTC)
	cfg, err := ParseConfig(RawConfig{}, time.UTC, now)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Patients != DefaultPatients || cfg.Days != DefaultDays || cfg.Seed != DefaultSeed {
		t.Fatalf("defaults: %+v", cfg)
	}
	if !cfg.Anchor.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default anchor = today 00:00, got %s", cfg.Anchor)
	}
	if got := cfg.FirstDay(); !got.Equal(cfg.Anchor.AddDate(0, 0, -364)) {
		t.Fatalf("365 days ending on the anchor: first day %s", got)
	}
	for name, raw := range map[string]RawConfig{
		"future anchor": {Anchor: "2026-10-24"},
		"bad anchor":    {Anchor: "23/10/2026"},
		"bad seed":      {Seed: "-1"},
		"too few":       {Patients: "10"},
		"too many":      {Patients: "50000"},
		"too short":     {Days: "3"},
		"not a number":  {Days: "abc"},
	} {
		if _, err := ParseConfig(raw, time.UTC, now); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	if _, err := ParseConfig(RawConfig{}, nil, now); err == nil {
		t.Fatal("missing timezone must be refused")
	}
}

func TestExplicitAnchorHasStableSimulationHorizonWhateverTheWallClock(t *testing.T) {
	raw := RawConfig{Anchor: "2026-10-23", Patients: "1500", Days: "365", Seed: "20260101"}
	want := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, wall := range []time.Time{
		time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC),  // exactly the snapshot
		time.Date(2026, 10, 23, 17, 31, 0, 0, time.UTC), // same day, afternoon
		time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC),   // next morning
		time.Date(2027, 1, 15, 3, 0, 0, 0, time.UTC),    // months later, at night
	} {
		cfg, err := ParseConfig(raw, time.UTC, wall)
		if err != nil {
			t.Fatalf("wall %s: %v", wall, err)
		}
		if !cfg.SimulationNow().Equal(want) || !cfg.Anchor.Equal(want.Truncate(24*time.Hour)) || cfg.FirstDay() != want.Truncate(24*time.Hour).AddDate(0, 0, -364) {
			t.Fatalf("wall %s: simulated present %s (want %s)", wall, cfg.SimulationNow(), want)
		}
	}
}

func TestPastAnchorSnapshotIsTheAnchorNoon(t *testing.T) {
	cfg, err := ParseConfig(RawConfig{Anchor: "2026-10-20"}, time.UTC, time.Date(2026, 10, 23, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC); !cfg.SimulationNow().Equal(want) {
		t.Fatalf("past anchor: simulated present %s, want %s", cfg.SimulationNow(), want)
	}
}

func TestAnchorOfTheDayBeforeTheSnapshotIsRefusedNotShifted(t *testing.T) {
	morning := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	if _, err := ParseConfig(RawConfig{Anchor: "2026-10-23"}, time.UTC, morning); err == nil {
		t.Fatal("an explicit anchor whose 12:00 snapshot is still in the future must be refused")
	}
	if _, err := ParseConfig(RawConfig{Anchor: "2026-10-24"}, time.UTC, morning); err == nil {
		t.Fatal("a future anchor must be refused")
	}
	// Default anchor: latest day whose snapshot is past.
	def, err := ParseConfig(RawConfig{}, time.UTC, morning)
	if err != nil {
		t.Fatal(err)
	}
	if !def.Anchor.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default anchor before noon = yesterday, got %s", def.Anchor)
	}
	def, _ = ParseConfig(RawConfig{}, time.UTC, time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC))
	if !def.Anchor.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("default anchor after noon = today, got %s", def.Anchor)
	}
}

func TestCashSnapshotRuleDependsOnSimulatedPresentOnly(t *testing.T) {
	cases := map[string]bool{"03:00": false, "06:29": false, "06:30": true, "12:00": true, "21:59": true, "22:00": false}
	for hm, want := range cases {
		at, _ := time.Parse("15:04", hm)
		if got := cashDeskOpenAt(at); got != want {
			t.Fatalf("desk open at %s = %v, want %v", hm, got, want)
		}
	}
	// Any anchor, past or present, snapshots at 12:00: open desks are expected.
	for _, a := range []string{"2025-01-05", "2026-10-23"} {
		cfg, err := ParseConfig(RawConfig{Anchor: a}, time.UTC, time.Date(2026, 10, 23, 18, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if !cashDeskOpenAt(cfg.SimulationNow()) {
			t.Fatalf("anchor %s: desks must be open at the simulated present", a)
		}
	}
}

func TestVoucherValidationUsesPersistentHistoryNotTheClock(t *testing.T) {
	for _, s := range voucherTerminalStatuses() {
		if s == "draft" || s == "submitted" || s == "controlled" {
			t.Fatalf("transient state %s must not be a critical current-state requirement", s)
		}
	}
	seen := map[string]bool{}
	for _, tr := range voucherCycle() {
		seen[tr[0]], seen[tr[1]] = true, true
	}
	for _, st := range []string{"draft", "submitted", "controlled", "validated", "rejected", "cancelled"} {
		if !seen[st] {
			t.Fatalf("workflow cycle check must cover %s", st)
		}
	}
}

func TestRNGIsDeterministicAndStreamsAreIndependent(t *testing.T) {
	a, b := newRNG(7, "x"), newRNG(7, "x")
	for i := 0; i < 50; i++ {
		if a.Intn(1000) != b.Intn(1000) {
			t.Fatal("same seed and stream must give the same sequence")
		}
	}
	c, d := newRNG(7, "x"), newRNG(8, "x")
	same := 0
	for i := 0; i < 50; i++ {
		if c.Intn(1000) == d.Intn(1000) {
			same++
		}
	}
	if same > 5 {
		t.Fatal("different seeds must give different sequences")
	}
	// Adding draws to one child stream never shifts a sibling stream derived before.
	root1, root2 := newRNG(1, "root"), newRNG(1, "root")
	s1, t1 := root1.Stream("a"), root1.Stream("b")
	s2, t2 := root2.Stream("a"), root2.Stream("b")
	for i := 0; i < 10; i++ {
		s1.Intn(10)
	}
	_ = s2
	if t1.Intn(1_000_000) != t2.Intn(1_000_000) {
		t.Fatal("streams must be independent")
	}
}

func TestCalendarCoversThePeriodWithRealisticLoad(t *testing.T) {
	cfg, err := ParseConfig(RawConfig{Anchor: "2026-10-23"}, time.UTC, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	cal := BuildCalendar(cfg, newRNG(cfg.Seed, "calendar"))
	again := BuildCalendar(cfg, newRNG(cfg.Seed, "calendar"))
	if len(cal) != 365 || !cal[0].Date.Equal(cfg.FirstDay()) || !cal[364].Date.Equal(cfg.Anchor) || !cal[364].IsToday {
		t.Fatalf("calendar bounds: %d days, %s → %s", len(cal), cal[0].Date, cal[len(cal)-1].Date)
	}
	kinds := map[string]int{}
	var weekday, sunday float64
	var nw, ns int
	for i, d := range cal {
		if d.Load != again[i].Load || d.Kind != again[i].Kind {
			t.Fatal("calendar must be deterministic")
		}
		kinds[d.Kind]++
		switch d.Date.Weekday() {
		case time.Sunday:
			sunday += d.Load
			ns++
		case time.Tuesday, time.Wednesday, time.Thursday:
			weekday += d.Load
			nw++
		}
	}
	for _, k := range []string{"NORMAL", "BUSY", "PEAK", "CALM", "WEEKEND", "TODAY"} {
		if kinds[k] == 0 {
			t.Fatalf("day kind %s missing: %v", k, kinds)
		}
	}
	if sunday/float64(ns) > 0.5*weekday/float64(nw) {
		t.Fatal("Sundays must be much calmer than weekdays")
	}
	if cal[364].Load < 3 {
		t.Fatal("the anchor day must be busy")
	}
}

func TestSchedulerOrdersStepsAndDropsTheFuture(t *testing.T) {
	horizon := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s := NewScheduler(horizon)
	var order []string
	add := func(at time.Time, name string) {
		s.At(at, name, func(*StepContext) error { order = append(order, name); return nil })
	}
	add(horizon.Add(-time.Hour), "b")
	add(horizon.Add(-2*time.Hour), "a")
	add(horizon.Add(-time.Hour), "c") // same instant as b: insertion order
	add(horizon.Add(time.Minute), "future")
	for {
		st, ok := s.popBefore(horizon.Add(24 * time.Hour))
		if !ok {
			break
		}
		_ = st.Run(nil)
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Fatalf("order %v", order)
	}
	if s.dropped["future"] != 1 {
		t.Fatal("a step after the horizon is never executed")
	}
}

func TestSortBillableActsBreaksEqualDateTiesDeterministically(t *testing.T) {
	want := []billing.BillableAct{
		{ActType: "IMAGING", ReferenceID: 7, BillableKey: "IMAGING:7", Date: "2026-01-15T10:30:00.900001Z"},
		{ActType: "LABORATORY", ReferenceID: 2, BillableKey: "LABORATORY:2", Date: "2026-01-15T10:30:00.100001Z"},
		{ActType: "LABORATORY", ReferenceID: 9, BillableKey: "LABORATORY:9", Date: "2026-01-15T10:30:00.500001Z"},
		{ActType: "PERFORMED_ACT", ReferenceID: 4, BillableKey: "PERFORMED_ACT:4", Date: "2026-01-15T10:30:00.000001Z"},
	}
	permutations := [][]billing.BillableAct{
		{want[3], want[1], want[0], want[2]},
		{want[2], want[0], want[3], want[1]},
	}
	for n, got := range permutations {
		sortBillableActs(got)
		for i := range want {
			if got[i].BillableKey != want[i].BillableKey {
				t.Fatalf("permutation %d index %d: got %s, want %s", n, i, got[i].BillableKey, want[i].BillableKey)
			}
		}
	}
}

func TestPopulationIsDeterministicAndDiverse(t *testing.T) {
	build := func() []*Profile {
		cfg := testConfig(t)
		cfg.Patients = 1500
		cfg.Days = 365
		cfg.Anchor = time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
		w := &World{Cfg: cfg, RNG: newRNG(cfg.Seed, "demo-complete"), People: &patientState{},
			Ref: &refState{Companies: []company.InsuranceCompany{{Code: "A"}, {Code: "B"}}, Guarantors: map[uint][]guarantor.InsuranceGuarantor{0: {{DefaultCoverageRate: 80}}}}}
		if err := (populationPlanner{}).Setup(w); err != nil {
			t.Fatal(err)
		}
		return w.People.Profiles
	}
	a, b := build(), build()
	if len(a) != 1500 {
		t.Fatalf("patients %d", len(a))
	}
	tiers := map[string]int{}
	var kids, elderly, insured, expired, secondary, old int
	names := map[string]int{}
	anchor := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for i, p := range a {
		q := b[i]
		if p.Ident != q.Ident || !p.Birth.Equal(q.Birth) || p.Tier != q.Tier || p.RegisterDay != q.RegisterDay {
			t.Fatal("population must be deterministic")
		}
		tiers[p.Tier]++
		switch age := p.AgeAt(anchor); {
		case age < 15:
			kids++
		case age >= 65:
			elderly++
		}
		if p.Insurance != nil {
			insured++
			if p.Insurance.Expired {
				expired++
			}
			if p.Insurance.Secondary {
				secondary++
			}
		}
		if p.RegisterDay <= 20 {
			old++
		}
		names[p.Ident.Last+"|"+p.Ident.First]++
	}
	homonyms := 0
	for _, n := range names {
		if n > 1 {
			homonyms++
		}
	}
	for _, tier := range []string{TierMinimal, TierStandard, TierRich, TierComplex} {
		if tiers[tier] < 30 {
			t.Fatalf("tier %s under-represented: %v", tier, tiers)
		}
	}
	if kids < 200 || elderly < 100 || insured < 600 || insured > 1000 || expired < 20 || secondary < 40 || homonyms < 5 || old < 600 || old > 1000 {
		t.Fatalf("distribution: kids=%d elderly=%d insured=%d expired=%d secondary=%d homonyms=%d old=%d", kids, elderly, insured, expired, secondary, homonyms, old)
	}
}

func TestReportVerdicts(t *testing.T) {
	r := &Report{Checks: []Check{{Domain: "A", Name: "x", Critical: true, OK: true}, {Domain: "A", Name: "y", Critical: false, OK: false}, {Domain: "B", Name: "z", Critical: true, OK: true}}}
	if !r.Passed() {
		t.Fatal("a failed non-critical check does not fail OVERALL")
	}
	_, ok := r.DomainVerdicts()
	if ok["A"] || !ok["B"] {
		t.Fatalf("domain verdicts %v", ok)
	}
	r.Checks = append(r.Checks, Check{Domain: "B", Name: "w", Critical: true, OK: false})
	if r.Passed() {
		t.Fatal("a failed critical check fails OVERALL")
	}
}

func TestPlannedColumnsAreExplicitInputs(t *testing.T) {
	for _, c := range []string{"patient_queue_appointments.scheduled_at", "patients.date_naissance", "patient_coverages.valid_to"} {
		if !plannedColumns[c] {
			t.Fatalf("%s must never be re-anchored", c)
		}
	}
	for _, c := range []string{"consultations.created_at", "consultations.sick_leave_start_date", "billing_payments.paid_at"} {
		if plannedColumns[c] {
			t.Fatalf("%s is written from now(): it must be re-anchored", c)
		}
	}
}
