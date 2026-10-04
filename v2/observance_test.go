// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"reflect"
	"testing"
	"time"
)

func testDateHoliday(month time.Month, day int) *Holiday {
	return &Holiday{Month: month, Day: day, Func: CalcDayOfMonth}
}

func testDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, DefaultLoc)
}

func resultByID(results []ObservanceResult, id string) *ObservanceResult {
	for i := range results {
		if results[i].ID == id {
			return &results[i]
		}
	}
	return nil
}

func TestObservancePlanChainedCollisions(t *testing.T) {
	// three holidays with the same actual day (Monday 2026-06-01) must
	// cascade to consecutive workdays in priority order
	p := NewObservancePlanner()
	results := p.Plan(2026,
		ObservanceRequest{ID: "third", Holiday: testDateHoliday(6, 1), Priority: 3, Direction: SearchForward},
		ObservanceRequest{ID: "first", Holiday: testDateHoliday(6, 1), Priority: 1, Direction: SearchForward},
		ObservanceRequest{ID: "second", Holiday: testDateHoliday(6, 1), Priority: 2, Direction: SearchForward},
	)

	want := map[string]time.Time{
		"first":  testDate(2026, 6, 1),
		"second": testDate(2026, 6, 2),
		"third":  testDate(2026, 6, 3),
	}
	for id, obs := range want {
		r := resultByID(results, id)
		if r == nil {
			t.Fatalf("missing result for %q", id)
		}
		if !r.Actual.Equal(testDate(2026, 6, 1)) {
			t.Errorf("%s: actual = %v, want 2026-06-01", id, r.Actual)
		}
		if !r.Observed.Equal(obs) {
			t.Errorf("%s: observed = %v, want %v", id, r.Observed, obs)
		}
		if r.Exhausted {
			t.Errorf("%s: unexpectedly exhausted", id)
		}
	}

	// the last holiday in the chain must record the full reason chain
	third := resultByID(results, "third")
	wantReasons := []ObservanceReason{
		{Date: testDate(2026, 6, 1), Code: RejectOccupiedObserved, By: "first"},
		{Date: testDate(2026, 6, 2), Code: RejectOccupiedObserved, By: "second"},
	}
	if !reflect.DeepEqual(third.Reasons, wantReasons) {
		t.Errorf("third reasons = %+v, want %+v", third.Reasons, wantReasons)
	}
	if !third.Moved() {
		t.Errorf("third should report Moved")
	}
}

func TestObservancePlanSamePrioritySortsByID(t *testing.T) {
	// equal priorities are ordered by ID, so registration order must not
	// change the outcome
	reqs := []ObservanceRequest{
		{ID: "beta", Holiday: testDateHoliday(6, 1), Priority: 5, Direction: SearchForward},
		{ID: "alpha", Holiday: testDateHoliday(6, 1), Priority: 5, Direction: SearchForward},
	}

	forward := NewObservancePlanner().Plan(2026, reqs...)
	reversed := NewObservancePlanner().Plan(2026, reqs[1], reqs[0])

	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("registration order changed the plan:\nforward:  %+v\nreversed: %+v", forward, reversed)
	}

	alpha := resultByID(forward, "alpha")
	beta := resultByID(forward, "beta")
	if !alpha.Observed.Equal(testDate(2026, 6, 1)) {
		t.Errorf("alpha observed = %v, want 2026-06-01", alpha.Observed)
	}
	if !beta.Observed.Equal(testDate(2026, 6, 2)) {
		t.Errorf("beta observed = %v, want 2026-06-02", beta.Observed)
	}
}

func TestObservancePlanDeterministicOrder(t *testing.T) {
	reqs := []ObservanceRequest{
		{ID: "e", Holiday: testDateHoliday(1, 1), Priority: 2, Direction: SearchBackward},
		{ID: "d", Holiday: testDateHoliday(12, 25), Priority: 1, Direction: SearchForward},
		{ID: "c", Holiday: testDateHoliday(7, 4), Priority: 1, Direction: SearchForward},
		{ID: "b", Holiday: testDateHoliday(12, 25), Priority: 1, Direction: SearchBackward},
		{ID: "a", Holiday: testDateHoliday(7, 4), Priority: 1, Direction: SearchForward, ProtectActual: true},
	}

	base := NewObservancePlanner().Plan(2022, reqs...)

	reversed := make([]ObservanceRequest, len(reqs))
	for i, r := range reqs {
		reversed[len(reqs)-1-i] = r
	}
	alt := NewObservancePlanner().Plan(2022, reversed...)

	if !reflect.DeepEqual(base, alt) {
		t.Errorf("registration order changed the plan:\nbase: %+v\nalt:  %+v", base, alt)
	}
}

func TestObservancePlanProtectedActual(t *testing.T) {
	// a protected actual day cannot be occupied, even by a holiday with
	// higher scheduling priority
	p := NewObservancePlanner()
	results := p.Plan(2026,
		ObservanceRequest{ID: "mover", Holiday: testDateHoliday(6, 1), Priority: 1, Direction: SearchForward},
		ObservanceRequest{ID: "fixed", Holiday: testDateHoliday(6, 1), Priority: 2, Direction: SearchForward, ProtectActual: true},
	)

	fixed := resultByID(results, "fixed")
	mover := resultByID(results, "mover")
	if !fixed.Observed.Equal(testDate(2026, 6, 1)) {
		t.Errorf("fixed observed = %v, want 2026-06-01", fixed.Observed)
	}
	if !mover.Observed.Equal(testDate(2026, 6, 2)) {
		t.Errorf("mover observed = %v, want 2026-06-02", mover.Observed)
	}
	wantReasons := []ObservanceReason{
		{Date: testDate(2026, 6, 1), Code: RejectProtectedActual, By: "fixed"},
	}
	if !reflect.DeepEqual(mover.Reasons, wantReasons) {
		t.Errorf("mover reasons = %+v, want %+v", mover.Reasons, wantReasons)
	}
}

func TestObservancePlanYearBoundaries(t *testing.T) {
	// New Year's Day 2022 falls on a Saturday and is observed backwards on
	// Friday 2021-12-31, pushing the year-end holiday out of 2021
	newYear := ObservanceRequest{ID: "new-year", Holiday: testDateHoliday(1, 1), Priority: 1, Direction: SearchBackward}
	yearEnd := ObservanceRequest{ID: "year-end", Holiday: testDateHoliday(12, 31), Priority: 2, Direction: SearchForward}

	res2021 := NewObservancePlanner().Plan(2021, newYear, yearEnd)
	ye := resultByID(res2021, "year-end")
	if ye == nil || ye.Year != 2021 {
		t.Fatalf("missing year-end result for 2021: %+v", res2021)
	}
	if !ye.Actual.Equal(testDate(2021, 12, 31)) {
		t.Errorf("year-end actual = %v, want 2021-12-31", ye.Actual)
	}
	if !ye.Observed.Equal(testDate(2022, 1, 3)) {
		t.Errorf("year-end observed = %v, want 2022-01-03", ye.Observed)
	}
	wantReasons := []ObservanceReason{
		{Date: testDate(2021, 12, 31), Code: RejectOccupiedObserved, By: "new-year"},
		{Date: testDate(2022, 1, 1), Code: RejectNotWorkday},
		{Date: testDate(2022, 1, 2), Code: RejectNotWorkday},
	}
	if !reflect.DeepEqual(ye.Reasons, wantReasons) {
		t.Errorf("year-end reasons = %+v, want %+v", ye.Reasons, wantReasons)
	}

	// planning 2022 must place the New Year's observance in 2021
	res2022 := NewObservancePlanner().Plan(2022, newYear, yearEnd)
	ny := resultByID(res2022, "new-year")
	if ny == nil || ny.Year != 2022 {
		t.Fatalf("missing new-year result for 2022: %+v", res2022)
	}
	if !ny.Actual.Equal(testDate(2022, 1, 1)) {
		t.Errorf("new-year actual = %v, want 2022-01-01", ny.Actual)
	}
	if !ny.Observed.Equal(testDate(2021, 12, 31)) {
		t.Errorf("new-year observed = %v, want 2021-12-31", ny.Observed)
	}
}

func TestObservancePlanExhausted(t *testing.T) {
	// no workdays at all: the search must terminate and report exhaustion
	p := NewObservancePlanner()
	for d := time.Sunday; d <= time.Saturday; d++ {
		p.SetWorkday(d, false)
	}
	results := p.Plan(2026,
		ObservanceRequest{ID: "stuck", Holiday: testDateHoliday(6, 1), Priority: 1, Direction: SearchForward},
	)
	r := resultByID(results, "stuck")
	if r == nil {
		t.Fatal("missing result for stuck")
	}
	if !r.Exhausted {
		t.Error("expected Exhausted to be set")
	}
	if !r.Observed.IsZero() {
		t.Errorf("observed = %v, want zero time", r.Observed)
	}
	if len(r.Reasons) != DefaultObservanceSearchLimit+1 {
		t.Errorf("got %d reasons, want %d", len(r.Reasons), DefaultObservanceSearchLimit+1)
	}

	// a small search limit with all candidates blocked must also terminate
	p2 := NewObservancePlanner()
	p2.MaxSearch = 2
	results = p2.Plan(2026,
		ObservanceRequest{ID: "block-1", Holiday: testDateHoliday(6, 1), Priority: 1, Direction: SearchForward, ProtectActual: true},
		ObservanceRequest{ID: "block-2", Holiday: testDateHoliday(6, 2), Priority: 1, Direction: SearchForward, ProtectActual: true},
		ObservanceRequest{ID: "block-3", Holiday: testDateHoliday(6, 3), Priority: 1, Direction: SearchForward, ProtectActual: true},
		ObservanceRequest{ID: "stuck", Holiday: testDateHoliday(6, 1), Priority: 2, Direction: SearchForward},
	)
	r = resultByID(results, "stuck")
	if r == nil {
		t.Fatal("missing result for stuck")
	}
	if !r.Exhausted {
		t.Error("expected Exhausted to be set")
	}
	if !r.Observed.IsZero() {
		t.Errorf("observed = %v, want zero time", r.Observed)
	}
	if len(r.Reasons) != 3 {
		t.Errorf("got %d reasons, want 3", len(r.Reasons))
	}
	for _, reason := range r.Reasons {
		if reason.Code != RejectProtectedActual {
			t.Errorf("reason = %v, want %v", reason.Code, RejectProtectedActual)
		}
	}
}

func TestObservancePlanDoesNotModifyHolidays(t *testing.T) {
	h := &Holiday{
		Month:    1,
		Day:      1,
		Observed: []AltDay{{Day: time.Saturday, Offset: -1}, {Day: time.Sunday, Offset: 1}},
		Func:     CalcDayOfMonth,
	}
	actBefore, obsBefore := h.Calc(2022)
	observedBefore := h.Observed

	p := NewObservancePlanner()
	results := p.Plan(2022,
		ObservanceRequest{ID: "new-year", Holiday: h, Priority: 1, Direction: SearchBackward},
	)

	actAfter, obsAfter := h.Calc(2022)
	if !actBefore.Equal(actAfter) || !obsBefore.Equal(obsAfter) {
		t.Errorf("Calc behavior changed: (%v, %v) -> (%v, %v)", actBefore, obsBefore, actAfter, obsAfter)
	}
	if len(h.Observed) != 2 || &h.Observed[0] != &observedBefore[0] {
		t.Errorf("holiday Observed rules were modified: %+v", h.Observed)
	}

	// the plan preserves the actual day and only determines the observed day
	r := resultByID(results, "new-year")
	if r == nil {
		t.Fatal("missing result for new-year")
	}
	if !r.Actual.Equal(testDate(2022, 1, 1)) {
		t.Errorf("actual = %v, want 2022-01-01", r.Actual)
	}
	if !r.Observed.Equal(testDate(2021, 12, 31)) {
		t.Errorf("observed = %v, want 2021-12-31", r.Observed)
	}
}
