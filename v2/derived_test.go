package cal

import (
	"strings"
	"testing"
	"time"
)

// newDerivedTestBase returns an artificial business calendar with Monday
// through Friday as workdays and no holidays.
func newDerivedTestBase() *BusinessCalendar {
	return NewBusinessCalendar()
}

func derivedTestDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, DefaultLoc)
}

func findDerivedResults(results []*DerivedResult, name string) []*DerivedResult {
	var out []*DerivedResult
	for _, r := range results {
		if r.Rule.Name == name {
			out = append(out, r)
		}
	}
	return out
}

// TestDerivedSetEval builds a diamond dependency from three custom
// holidays: leaf "mid" is shared by "mid-day" (civil day offset) and
// "mid-work" (workday offset), and "mid-last" joins both branches. A
// separate rule offsets New Year's Day backwards into the previous year.
func TestDerivedSetEval(t *testing.T) {
	counts := map[int]int{}
	mid := &Holiday{
		Func: func(h *Holiday, year int) time.Time {
			counts[year]++
			return derivedTestDate(year, time.June, 15)
		},
	}
	newYear := &Holiday{Month: time.January, Day: 1, Func: CalcDayOfMonth}

	set, err := NewDerivedSet(newDerivedTestBase(),
		LeafRule("mid", mid),
		DayOffsetRule("mid-day", ActualRef("mid"), 2),
		WorkdayOffsetRule("mid-work", ActualRef("mid"), 3),
		LatestRule("mid-last", ActualRef("mid-day"), ActualRef("mid-work")),
		LeafRule("new-year", newYear),
		DayOffsetRule("year-end", ActualRef("new-year"), -1),
	)
	if err != nil {
		t.Fatalf("unexpected construction error: %v", err)
	}

	results, err := set.Eval(2026)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}

	// all results are clipped to the requested year
	for _, r := range results {
		if r.Date.Year() != 2026 {
			t.Errorf("result %q (%d) out of range: %s", r.Rule.Name, r.Year, r.Date)
		}
	}

	// 2026-06-15 is a Monday
	want := map[string]time.Time{
		"mid":      derivedTestDate(2026, time.June, 15),
		"mid-day":  derivedTestDate(2026, time.June, 17),
		"mid-work": derivedTestDate(2026, time.June, 18),
		"mid-last": derivedTestDate(2026, time.June, 18),
		"new-year": derivedTestDate(2026, time.January, 1),
	}
	for name, wantDate := range want {
		got := findDerivedResults(results, name)
		if len(got) != 1 || !got[0].Date.Equal(wantDate) {
			t.Errorf("%s: got %v, want single result %s", name, got, wantDate)
		}
	}

	// the workday offset respects the artificial base calendar
	if got := findDerivedResults(results, "mid-work"); len(got) == 1 {
		if wd := got[0].Date.Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("mid-work landed on a weekend: %s", got[0].Date)
		}
	}

	// New Year's Day 2027 minus one day lands on 2026-12-31 and keeps its
	// source chain across the year boundary
	var yearEnd *DerivedResult
	for _, r := range findDerivedResults(results, "year-end") {
		if r.Year == 2027 {
			yearEnd = r
		}
	}
	if yearEnd == nil {
		t.Fatalf("missing cross-year result for year-end")
	}
	if !yearEnd.Date.Equal(derivedTestDate(2026, time.December, 31)) {
		t.Errorf("year-end: got %s, want 2026-12-31", yearEnd.Date)
	}
	if len(yearEnd.Trace) != 2 ||
		yearEnd.Trace[0].Name != "new-year" || yearEnd.Trace[0].Year != 2027 ||
		!yearEnd.Trace[0].Date.Equal(derivedTestDate(2027, time.January, 1)) ||
		yearEnd.Trace[1].Name != "year-end" || yearEnd.Trace[1].Year != 2027 {
		t.Errorf("year-end: bad source chain: %+v", yearEnd.Trace)
	}

	// the shared leaf of the diamond is evaluated once per source year
	if len(counts) != 3 {
		t.Errorf("leaf evaluated for %d years, want 3", len(counts))
	}
	for year, n := range counts {
		if n != 1 {
			t.Errorf("leaf evaluated %d times for %d, want 1", n, year)
		}
	}
}
func TestDerivedSetActualVsObserved(t *testing.T) {
	// 2026-07-04 is a Saturday, observed on Friday 2026-07-03
	leaf := &Holiday{
		Month:    time.July,
		Day:      4,
		Func:     CalcDayOfMonth,
		Observed: []AltDay{{Day: time.Saturday, Offset: -1}},
	}

	set, err := NewDerivedSet(nil,
		LeafRule("leaf", leaf),
		DayOffsetRule("after-actual", ActualRef("leaf"), 1),
		DayOffsetRule("after-observed", ObservedRef("leaf"), 1),
		EarliestRule("first", ActualRef("after-actual"), ObservedRef("leaf")),
	)
	if err != nil {
		t.Fatalf("unexpected construction error: %v", err)
	}

	results, err := set.Eval(2026)
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}

	want := map[string]time.Time{
		"leaf":           derivedTestDate(2026, time.July, 4),
		"after-actual":   derivedTestDate(2026, time.July, 5),
		"after-observed": derivedTestDate(2026, time.July, 4),
		"first":          derivedTestDate(2026, time.July, 3),
	}
	for name, wantDate := range want {
		got := findDerivedResults(results, name)
		if len(got) != 1 || !got[0].Date.Equal(wantDate) {
			t.Errorf("%s: got %v, want single result %s", name, got, wantDate)
		}
	}
}

func TestDerivedSetValidation(t *testing.T) {
	leaf := LeafRule("a", &Holiday{Func: CalcDayOfMonth})

	tests := []struct {
		name  string
		base  *BusinessCalendar
		rules []DerivedRule
		want  string
	}{
		{
			name:  "missing reference",
			rules: []DerivedRule{leaf, DayOffsetRule("b", ActualRef("nope"), 1)},
			want:  "unknown rule",
		},
		{
			name: "cycle",
			rules: []DerivedRule{
				DayOffsetRule("a", ActualRef("b"), 1),
				DayOffsetRule("b", ActualRef("c"), 1),
				DayOffsetRule("c", ActualRef("a"), 1),
			},
			want: "cycle",
		},
		{
			name:  "self cycle",
			rules: []DerivedRule{DayOffsetRule("a", ActualRef("a"), 1)},
			want:  "cycle",
		},
		{
			name:  "duplicate name",
			rules: []DerivedRule{leaf, leaf},
			want:  "duplicate",
		},
		{
			name:  "nil leaf holiday",
			rules: []DerivedRule{LeafRule("a", nil)},
			want:  "requires a holiday",
		},
		{
			name:  "workday offset without base calendar",
			rules: []DerivedRule{leaf, WorkdayOffsetRule("b", ActualRef("a"), 1)},
			want:  "base calendar",
		},
		{
			name:  "empty sources",
			rules: []DerivedRule{leaf, EarliestRule("b")},
			want:  "at least one source",
		},
		{
			name:  "invalid source kind",
			rules: []DerivedRule{leaf, DayOffsetRule("b", DerivedRef{Name: "a"}, 1)},
			want:  "invalid source kind",
		},
	}

	for _, test := range tests {
		_, err := NewDerivedSet(test.base, test.rules...)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: got error %v, want substring %q", test.name, err, test.want)
		}
	}
}

func TestDerivedSetSearchLimit(t *testing.T) {
	// artificial calendar with no workdays at all
	base := NewBusinessCalendar()
	for d := time.Sunday; d <= time.Saturday; d++ {
		base.SetWorkday(d, false)
	}

	set, err := NewDerivedSet(base,
		LeafRule("a", &Holiday{Month: time.June, Day: 15, Func: CalcDayOfMonth}),
		WorkdayOffsetRule("b", ActualRef("a"), 1),
	)
	if err != nil {
		t.Fatalf("unexpected construction error: %v", err)
	}
	set.SearchLimit = 10

	_, err = set.Eval(2026)
	if err == nil || !strings.Contains(err.Error(), "search limit") {
		t.Errorf("got error %v, want search limit reason", err)
	}
}

func TestDerivedSetHiddenCycle(t *testing.T) {
	base := NewBusinessCalendar()
	set, err := NewDerivedSet(base,
		LeafRule("a", &Holiday{Month: time.June, Day: 15, Func: CalcDayOfMonth}),
		WorkdayOffsetRule("b", ActualRef("a"), 1),
	)
	if err != nil {
		t.Fatalf("unexpected construction error: %v", err)
	}

	// the base calendar references the derived set, forming a hidden cycle
	var innerErr error
	base.AddHoliday(&Holiday{
		Func: func(h *Holiday, year int) time.Time {
			_, innerErr = set.Eval(year)
			return time.Time{}
		},
	})

	if _, err = set.Eval(2026); err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if innerErr == nil || !strings.Contains(innerErr.Error(), "recursively") {
		t.Errorf("got inner error %v, want recursion reason", innerErr)
	}
}
