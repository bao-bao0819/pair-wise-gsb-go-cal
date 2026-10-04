package cal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func planDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, DefaultLoc)
}

func planTestHolidays() (christmas, boxing, extra *Holiday) {
	christmas = &Holiday{
		Name: "Christmas Day", Month: time.December, Day: 25, Func: CalcDayOfMonth,
		Observed: []AltDay{{Day: time.Saturday, Offset: 2}, {Day: time.Sunday, Offset: 1}},
	}
	boxing = &Holiday{
		Name: "Boxing Day", Month: time.December, Day: 26, Func: CalcDayOfMonth,
		Observed: []AltDay{{Day: time.Saturday, Offset: 2}, {Day: time.Sunday, Offset: 1}},
	}
	extra = &Holiday{
		Name: "Extra Day", Month: time.December, Day: 27, Func: CalcDayOfMonth,
	}
	return christmas, boxing, extra
}

func checkObserved(t *testing.T, plan *ObservancePlan, id string, want time.Time, wantShifted bool) {
	t.Helper()
	o := plan.Observation(id)
	if o == nil {
		t.Fatalf("no observation for %q", id)
	}
	if !o.Observed.Equal(want) {
		t.Errorf("%s observed: got %s; want %s", id, o.Observed, want)
	}
	if o.Shifted != wantShifted {
		t.Errorf("%s shifted: got %t; want %t", id, o.Shifted, wantShifted)
	}
	if wantShifted && len(o.Reasons) == 0 {
		t.Errorf("%s: expected a reason chain for the shift", id)
	}
	if !wantShifted && len(o.Reasons) != 0 {
		t.Errorf("%s: expected no reasons, got %v", id, o.Reasons)
	}
}

func TestPlanObservancesChainedCollisions(t *testing.T) {
	// in 2021, Dec 25 is a Saturday and Dec 26 is a Sunday
	christmas, boxing, extra := planTestHolidays()
	entries := []PlanEntry{
		{ID: "extra", Holiday: extra, Priority: 3},
		{ID: "boxing", Holiday: boxing, Priority: 2},
		{ID: "christmas", Holiday: christmas, Priority: 1},
	}

	plan, err := PlanObservances(2021, entries, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// christmas takes Mon 27; boxing is pushed to Tue 28; extra is pushed
	// past both to Wed 29
	checkObserved(t, plan, "christmas", planDate(2021, 12, 27), false)
	checkObserved(t, plan, "boxing", planDate(2021, 12, 28), true)
	checkObserved(t, plan, "extra", planDate(2021, 12, 29), true)

	// actual dates are preserved
	if got := plan.Observation("christmas").Actual; !got.Equal(planDate(2021, 12, 25)) {
		t.Errorf("christmas actual: got %s; want 2021-12-25", got)
	}
	if got := plan.Observation("boxing").Actual; !got.Equal(planDate(2021, 12, 26)) {
		t.Errorf("boxing actual: got %s; want 2021-12-26", got)
	}
}

func TestPlanObservancesSamePriorityIDOrder(t *testing.T) {
	newEntry := func(id string) PlanEntry {
		return PlanEntry{
			ID: id, Priority: 1,
			Holiday: &Holiday{
				Name: id, Month: time.December, Day: 25, Func: CalcDayOfMonth,
				Observed: []AltDay{{Day: time.Saturday, Offset: 2}},
			},
		}
	}

	// both holidays default to Mon 2021-12-27; with equal priority the
	// smaller ID wins the day
	plan1, err := PlanObservances(2021, []PlanEntry{newEntry("alpha"), newEntry("beta")}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan1, "alpha", planDate(2021, 12, 27), false)
	checkObserved(t, plan1, "beta", planDate(2021, 12, 28), true)

	// registration order must not change the result
	plan2, err := PlanObservances(2021, []PlanEntry{newEntry("beta"), newEntry("alpha")}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan2, "alpha", planDate(2021, 12, 27), false)
	checkObserved(t, plan2, "beta", planDate(2021, 12, 28), true)
}

func TestPlanObservancesYearBoundary(t *testing.T) {
	newYear := &Holiday{
		Name: "New Year's Day", Month: time.January, Day: 1, Func: CalcDayOfMonth,
		Observed: []AltDay{{Day: time.Saturday, Offset: -1}, {Day: time.Sunday, Offset: 1}},
	}
	nye := &Holiday{
		Name: "New Year's Eve", Month: time.December, Day: 31, Func: CalcDayOfMonth,
	}
	entries := []PlanEntry{
		{ID: "newyear", Holiday: newYear, Priority: 1},
		{ID: "nye", Holiday: nye, Priority: 2},
	}

	// Jan 1 2022 is a Saturday, observed Fri Dec 31 2021; that observation
	// belongs to the 2022 occurrence but must block the 2021 plan
	plan2021, err := PlanObservances(2021, entries, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan2021, "newyear", planDate(2021, 1, 1), false)
	// nye is pushed into the next year, skipping the weekend
	checkObserved(t, plan2021, "nye", planDate(2022, 1, 3), true)

	// planning 2022 must resolve the same boundary consistently
	plan2022, err := PlanObservances(2022, entries, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan2022, "newyear", planDate(2021, 12, 31), false)
	// Dec 31 2022 is a Saturday; nye moves forward past the weekend and
	// past Mon Jan 2 2023, which is taken by the 2023 New Year observation
	checkObserved(t, plan2022, "nye", planDate(2023, 1, 3), true)
}

func TestPlanObservancesProtected(t *testing.T) {
	christmas, _, _ := planTestHolidays()
	fixed := &Holiday{
		Name: "Fixed Day", Month: time.December, Day: 27, Func: CalcDayOfMonth,
	}
	entries := []PlanEntry{
		{ID: "christmas", Holiday: christmas, Priority: 1},
		{ID: "fixed", Holiday: fixed, Priority: 5, Protected: true},
	}

	// the protected actual day is reserved even though its priority is lower
	plan, err := PlanObservances(2021, entries, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan, "fixed", planDate(2021, 12, 27), false)
	checkObserved(t, plan, "christmas", planDate(2021, 12, 28), true)

	reasons := plan.Observation("christmas").Reasons
	if !strings.Contains(reasons[0], "protected actual day") {
		t.Errorf("expected protected conflict reason, got %v", reasons)
	}
}

func TestPlanObservancesSearchDirections(t *testing.T) {
	sat := &Holiday{
		Name: "Saturday Holiday", Month: time.December, Day: 25, Func: CalcDayOfMonth,
	}

	plan, err := PlanObservances(2021, []PlanEntry{{ID: "fwd", Holiday: sat, Direction: SearchForward}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan, "fwd", planDate(2021, 12, 27), true)

	plan, err = PlanObservances(2021, []PlanEntry{{ID: "back", Holiday: sat, Direction: SearchBackward}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan, "back", planDate(2021, 12, 24), true)

	// nearest searches earlier dates first
	blocker := &Holiday{Name: "Blocker", Month: time.December, Day: 29, Func: CalcDayOfMonth}
	target := &Holiday{Name: "Target", Month: time.December, Day: 29, Func: CalcDayOfMonth}
	plan, err = PlanObservances(2021, []PlanEntry{
		{ID: "blocker", Holiday: blocker, Priority: 1, Protected: true},
		{ID: "target", Holiday: target, Priority: 2, Direction: SearchNearest},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkObserved(t, plan, "target", planDate(2021, 12, 28), true)
}

func TestPlanObservancesNoAvailableDay(t *testing.T) {
	fridays := [7]bool{time.Friday: true}
	h1 := &Holiday{Name: "First", Month: time.June, Day: 4, Func: CalcDayOfMonth}
	h2 := &Holiday{Name: "Second", Month: time.June, Day: 4, Func: CalcDayOfMonth}

	// Jun 4 2021 is a Friday; the next Friday is 7 days away, beyond the
	// search limit of 5
	_, err := PlanObservances(2021, []PlanEntry{
		{ID: "first", Holiday: h1, Priority: 1},
		{ID: "second", Holiday: h2, Priority: 2},
	}, &PlanOptions{WorkWeek: fridays, MaxSearch: 5})

	var perr *PlanError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PlanError, got %v", err)
	}
	if perr.ID != "second" {
		t.Errorf("PlanError ID: got %q; want %q", perr.ID, "second")
	}
	if perr.Limit != 5 {
		t.Errorf("PlanError Limit: got %d; want 5", perr.Limit)
	}
	// default day plus 5 searched days must all be reported
	if len(perr.Reasons) != 6 {
		t.Errorf("PlanError Reasons: got %d; want 6 (%v)", len(perr.Reasons), perr.Reasons)
	}
}

func TestPlanObservancesRegistrationOrder(t *testing.T) {
	christmas, boxing, extra := planTestHolidays()
	newYear := &Holiday{
		Name: "New Year's Day", Month: time.January, Day: 1, Func: CalcDayOfMonth,
		Observed: []AltDay{{Day: time.Saturday, Offset: -1}, {Day: time.Sunday, Offset: 1}},
	}
	base := []PlanEntry{
		{ID: "christmas", Holiday: christmas, Priority: 1},
		{ID: "boxing", Holiday: boxing, Priority: 2},
		{ID: "extra", Holiday: extra, Priority: 3},
		{ID: "newyear", Holiday: newYear, Priority: 1},
	}

	signature := func(entries []PlanEntry) map[string]time.Time {
		plan, err := PlanObservances(2021, entries, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sig := make(map[string]time.Time)
		for _, o := range plan.Observations {
			sig[o.ID] = o.Observed
		}
		return sig
	}

	want := signature(base)

	// try every permutation of the entries
	perm := make([]PlanEntry, len(base))
	copy(perm, base)
	var permute func(i int)
	permute = func(i int) {
		if i == len(perm) {
			got := signature(perm)
			for id, obs := range want {
				if !got[id].Equal(obs) {
					t.Errorf("permutation %v: %s observed: got %s; want %s", perm, id, got[id], obs)
				}
			}
			return
		}
		for j := i; j < len(perm); j++ {
			perm[i], perm[j] = perm[j], perm[i]
			permute(i + 1)
			perm[i], perm[j] = perm[j], perm[i]
		}
	}
	permute(0)
}

func TestPlanObservancesDoesNotModifyHolidays(t *testing.T) {
	christmas, boxing, _ := planTestHolidays()
	actBefore, obsBefore := christmas.Calc(2021)

	_, err := PlanObservances(2021, []PlanEntry{
		{ID: "christmas", Holiday: christmas, Priority: 1},
		{ID: "boxing", Holiday: boxing, Priority: 2},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	actAfter, obsAfter := christmas.Calc(2021)
	if !actAfter.Equal(actBefore) || !obsAfter.Equal(obsBefore) {
		t.Errorf("Holiday.Calc results changed after planning: (%s, %s) -> (%s, %s)",
			actBefore, obsBefore, actAfter, obsAfter)
	}
	if len(christmas.Observed) != 2 || christmas.Day != 25 || christmas.Month != time.December {
		t.Errorf("holiday definition was modified: %+v", christmas)
	}
}

func TestPlanObservancesValidation(t *testing.T) {
	h := &Holiday{Name: "H", Month: time.January, Day: 1, Func: CalcDayOfMonth}

	if _, err := PlanObservances(2021, []PlanEntry{{Holiday: h}}, nil); err == nil {
		t.Error("expected error for empty ID")
	}
	if _, err := PlanObservances(2021, []PlanEntry{
		{ID: "dup", Holiday: h}, {ID: "dup", Holiday: h},
	}, nil); err == nil {
		t.Error("expected error for duplicate ID")
	}
	if _, err := PlanObservances(2021, []PlanEntry{{ID: "nil"}}, nil); err == nil {
		t.Error("expected error for nil Holiday")
	}

	// a holiday that does not occur in the plan year is skipped
	rare := &Holiday{
		Name: "Rare", Month: time.January, Day: 1, Func: CalcDayOfMonth,
		StartYear: 2030,
	}
	plan, err := PlanObservances(2021, []PlanEntry{{ID: "rare", Holiday: rare}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Observations) != 0 {
		t.Errorf("expected no observations, got %v", plan.Observations)
	}
}
