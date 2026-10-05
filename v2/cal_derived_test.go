// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, DefaultLoc)
}

// testDayHoliday creates an artificial holiday on a fixed day of the year.
// If count is non-nil, it is incremented each time the holiday function is
// evaluated.
func testDayHoliday(name string, month time.Month, day int, count *int) *Holiday {
	return &Holiday{
		Name:  name,
		Month: month,
		Day:   day,
		Func: func(h *Holiday, year int) time.Time {
			if count != nil {
				*count++
			}
			return time.Date(year, h.Month, h.Day, 0, 0, 0, 0, DefaultLoc)
		},
	}
}

// newDerivedTestSet builds a set of three artificial leaf holidays with
// derived rules forming a diamond over the shared "ny" leaf:
//
//	ny --> ny-minus-2 ------+
//	                        +--> year-end (latest)
//	ny --> ny-work-minus-3 -+
func newDerivedTestSet(t *testing.T, base *BusinessCalendar) (*DerivedSet, *int) {
	t.Helper()
	count := 0
	s, err := NewDerivedSet(
		&DerivedRule{Name: "ny", Kind: RuleLeaf, Holiday: testDayHoliday("New Year", time.January, 1, &count)},
		&DerivedRule{Name: "founding", Kind: RuleLeaf, Holiday: testDayHoliday("Founding Day", time.May, 10, nil)},
		&DerivedRule{Name: "harvest", Kind: RuleLeaf, Holiday: testDayHoliday("Harvest Day", time.September, 20, nil)},
		&DerivedRule{Name: "ny-minus-2", Kind: RuleDayOffset, Source: Ref{Name: "ny", Kind: DateActual}, Offset: -2},
		&DerivedRule{Name: "ny-work-minus-3", Kind: RuleWorkdayOffset, Source: Ref{Name: "ny", Kind: DateActual}, Offset: -3, Base: base},
		&DerivedRule{Name: "ny-work-plus-2", Kind: RuleWorkdayOffset, Source: Ref{Name: "ny", Kind: DateActual}, Offset: 2, Base: base},
		&DerivedRule{Name: "ny-work-0", Kind: RuleWorkdayOffset, Source: Ref{Name: "ny", Kind: DateActual}, Offset: 0, Base: base},
		&DerivedRule{Name: "year-end", Kind: RuleLatest, Sources: []Ref{
			{Name: "ny-minus-2", Kind: DateActual},
			{Name: "ny-work-minus-3", Kind: DateActual},
		}},
		&DerivedRule{Name: "first-half", Kind: RuleEarliest, Sources: []Ref{
			{Name: "founding", Kind: DateActual},
			{Name: "harvest", Kind: DateActual},
		}},
		&DerivedRule{Name: "second-half", Kind: RuleLatest, Sources: []Ref{
			{Name: "founding", Kind: DateActual},
			{Name: "harvest", Kind: DateActual},
		}},
		&DerivedRule{Name: "earliest-replace", Kind: RuleEarliest, Sources: []Ref{
			{Name: "harvest", Kind: DateActual},
			{Name: "founding", Kind: DateActual},
		}},
	)
	if err != nil {
		t.Fatalf("NewDerivedSet: %v", err)
	}
	return s, &count
}

func checkOccurrences(t *testing.T, occs, want []Occurrence) {
	t.Helper()
	if len(occs) != len(want) {
		t.Fatalf("got %d occurrences %v; want %d %v", len(occs), occs, len(want), want)
	}
	for i := range want {
		if !occs[i].Date.Equal(want[i].Date) {
			t.Errorf("occurrence %d date: got %s; want %s", i, occs[i].Date, want[i].Date)
		}
		if !reflect.DeepEqual(occs[i].Chain, want[i].Chain) {
			t.Errorf("occurrence %d chain: got %v; want %v", i, occs[i].Chain, want[i].Chain)
		}
	}
}

func TestNewDerivedSetValidation(t *testing.T) {
	leaf := func(name string) *DerivedRule {
		return &DerivedRule{Name: name, Kind: RuleLeaf, Holiday: testDayHoliday(name, time.January, 1, nil)}
	}
	dayOffset := func(name, src string) *DerivedRule {
		return &DerivedRule{Name: name, Kind: RuleDayOffset, Source: Ref{Name: src, Kind: DateActual}, Offset: 1}
	}

	tests := []struct {
		name  string
		rules []*DerivedRule
	}{
		{"nil rule", []*DerivedRule{nil}},
		{"empty name", []*DerivedRule{{Kind: RuleLeaf, Holiday: testDayHoliday("x", time.January, 1, nil)}}},
		{"duplicate name", []*DerivedRule{leaf("a"), leaf("a")}},
		{"leaf without holiday", []*DerivedRule{{Name: "a", Kind: RuleLeaf}}},
		{"day offset without source", []*DerivedRule{{Name: "a", Kind: RuleDayOffset, Offset: 1}}},
		{"workday offset without source", []*DerivedRule{{Name: "a", Kind: RuleWorkdayOffset, Offset: 1, Base: NewBusinessCalendar()}}},
		{"workday offset without base", []*DerivedRule{
			{Name: "a", Kind: RuleWorkdayOffset, Source: Ref{Name: "b", Kind: DateActual}, Offset: 1},
			leaf("b"),
		}},
		{"earliest without sources", []*DerivedRule{{Name: "a", Kind: RuleEarliest}}},
		{"latest without sources", []*DerivedRule{{Name: "a", Kind: RuleLatest}}},
		{"unknown rule kind", []*DerivedRule{{Name: "a", Kind: RuleKind(99)}}},
		{"invalid date kind", []*DerivedRule{
			leaf("a"),
			{Name: "b", Kind: RuleDayOffset, Source: Ref{Name: "a", Kind: DateKind(7)}, Offset: 1},
		}},
		{"missing reference", []*DerivedRule{dayOffset("a", "ghost")}},
		{"self cycle", []*DerivedRule{dayOffset("a", "a")}},
		{"cycle", []*DerivedRule{dayOffset("a", "b"), dayOffset("b", "a")}},
	}
	for _, test := range tests {
		if _, err := NewDerivedSet(test.rules...); err == nil {
			t.Errorf("%s: expected error", test.name)
		}
	}
}

func TestDerivedSetCalc(t *testing.T) {
	s, _ := newDerivedTestSet(t, NewBusinessCalendar())

	tests := []struct {
		rule string
		year int
		want []Occurrence
	}{
		{"ny", 2024, []Occurrence{
			{Date: testDate(2024, time.January, 1), Chain: []string{"ny"}},
		}},
		{"ny-minus-2", 2023, []Occurrence{
			{Date: testDate(2023, time.December, 30), Chain: []string{"ny", "ny-minus-2"}},
		}},
		{"ny-minus-2", 2024, []Occurrence{
			{Date: testDate(2024, time.December, 30), Chain: []string{"ny", "ny-minus-2"}},
		}},
		{"ny-work-minus-3", 2023, []Occurrence{
			{Date: testDate(2023, time.December, 27), Chain: []string{"ny", "ny-work-minus-3"}},
		}},
		{"ny-work-plus-2", 2024, []Occurrence{
			{Date: testDate(2024, time.January, 3), Chain: []string{"ny", "ny-work-plus-2"}},
		}},
		{"ny-work-0", 2024, []Occurrence{
			{Date: testDate(2024, time.January, 1), Chain: []string{"ny", "ny-work-0"}},
		}},
		{"year-end", 2023, []Occurrence{
			{Date: testDate(2023, time.December, 30), Chain: []string{"ny", "ny-minus-2", "year-end"}},
		}},
		{"year-end", 2024, []Occurrence{
			{Date: testDate(2024, time.December, 30), Chain: []string{"ny", "ny-minus-2", "year-end"}},
		}},
		{"first-half", 2024, []Occurrence{
			{Date: testDate(2024, time.May, 10), Chain: []string{"founding", "first-half"}},
		}},
		{"second-half", 2024, []Occurrence{
			{Date: testDate(2024, time.September, 20), Chain: []string{"harvest", "second-half"}},
		}},
		{"earliest-replace", 2024, []Occurrence{
			{Date: testDate(2024, time.May, 10), Chain: []string{"founding", "earliest-replace"}},
		}},
	}
	for _, test := range tests {
		occs, err := s.Calc(test.rule, test.year)
		if err != nil {
			t.Errorf("%s %d: %v", test.rule, test.year, err)
			continue
		}
		checkOccurrences(t, occs, test.want)
	}
}

func TestDerivedSetSharedLeafEvaluatedOnce(t *testing.T) {
	s, count := newDerivedTestSet(t, NewBusinessCalendar())

	if _, err := s.Calc("year-end", 2024); err != nil {
		t.Fatal(err)
	}
	// the shared leaf is evaluated once per source year (2023, 2024, 2025)
	// rather than once per dependent rule in the diamond
	if *count != 3 {
		t.Errorf("leaf evaluated %d times; want 3", *count)
	}
}

func TestDerivedSetUnknownRule(t *testing.T) {
	s, _ := newDerivedTestSet(t, NewBusinessCalendar())

	if _, err := s.Calc("nope", 2024); err == nil {
		t.Error("Calc: expected error for unknown rule")
	}
	if _, err := s.Holiday("nope"); err == nil {
		t.Error("Holiday: expected error for unknown rule")
	}
}

func TestDerivedSetDriftingLeaf(t *testing.T) {
	drift := &Holiday{
		Name: "Drifter",
		Func: func(h *Holiday, year int) time.Time {
			return time.Date(year, time.January, 1, 0, 0, 0, 0, DefaultLoc).AddDate(0, 0, (2024-year)*200)
		},
	}
	s, err := NewDerivedSet(&DerivedRule{Name: "drift", Kind: RuleLeaf, Holiday: drift})
	if err != nil {
		t.Fatal(err)
	}

	occs, err := s.Calc("drift", 2024)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2024, time.January, 1), Chain: []string{"drift"}},
		{Date: testDate(2024, time.June, 15), Chain: []string{"drift"}},
	})
}

func TestDerivedSetActualVsObserved(t *testing.T) {
	// New Year's Day 2022 fell on a Saturday
	h := &Holiday{
		Name:     "New Year",
		Month:    time.January,
		Day:      1,
		Observed: []AltDay{{Day: time.Saturday, Offset: -1}},
		Func:     CalcDayOfMonth,
	}
	s, err := NewDerivedSet(
		&DerivedRule{Name: "ny", Kind: RuleLeaf, Holiday: h},
		&DerivedRule{Name: "after-actual", Kind: RuleDayOffset, Source: Ref{Name: "ny", Kind: DateActual}, Offset: 2},
		&DerivedRule{Name: "after-observed", Kind: RuleDayOffset, Source: Ref{Name: "ny", Kind: DateObserved}, Offset: 2},
	)
	if err != nil {
		t.Fatal(err)
	}

	occs, err := s.Calc("after-actual", 2022)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2022, time.January, 3), Chain: []string{"ny", "after-actual"}},
	})

	occs, err = s.Calc("after-observed", 2022)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2022, time.January, 2), Chain: []string{"ny", "after-observed"}},
	})
}

func TestDerivedSetLeafShift(t *testing.T) {
	h := &Holiday{
		Name:       "Shifty",
		Month:      time.January,
		Day:        1,
		CalcOffset: 1,
		Observed: []AltDay{
			{Day: time.Sunday, Offset: -2},
			{Day: time.Monday, Offset: 0},
		},
		Func: CalcDayOfMonth,
	}
	s, err := NewDerivedSet(
		&DerivedRule{Name: "l", Kind: RuleLeaf, Holiday: h},
		&DerivedRule{Name: "lo", Kind: RuleDayOffset, Source: Ref{Name: "l", Kind: DateObserved}, Offset: 1},
	)
	if err != nil {
		t.Fatal(err)
	}

	// 2022-01-01 + 1 calc offset = 2022-01-02 (Sunday), observed 2 days earlier
	occs, err := s.Calc("l", 2022)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2022, time.January, 2), Chain: []string{"l"}},
	})

	occs, err = s.Calc("lo", 2022)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2022, time.January, 1), Chain: []string{"l", "lo"}},
	})
}

func TestDerivedSetLimitedLeaf(t *testing.T) {
	lim := &Holiday{
		Name:      "Limited",
		Month:     time.March,
		Day:       15,
		StartYear: 2024,
		Func:      CalcDayOfMonth,
	}
	s, err := NewDerivedSet(
		&DerivedRule{Name: "lim", Kind: RuleLeaf, Holiday: lim},
		&DerivedRule{Name: "lim-off", Kind: RuleDayOffset, Source: Ref{Name: "lim", Kind: DateActual}, Offset: 5},
		&DerivedRule{Name: "lim-wd", Kind: RuleWorkdayOffset, Source: Ref{Name: "lim", Kind: DateActual},
			Offset: 1, Base: NewBusinessCalendar()},
		&DerivedRule{Name: "lim-e", Kind: RuleEarliest, Sources: []Ref{
			{Name: "lim", Kind: DateActual},
			{Name: "lim", Kind: DateActual},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}

	occs, err := s.Calc("lim", 2023)
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 0 {
		t.Errorf("got %v; want no occurrences before the start year", occs)
	}

	occs, err = s.Calc("lim-off", 2024)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2024, time.March, 20), Chain: []string{"lim", "lim-off"}},
	})

	// 2024-03-15 is a Friday; the next workday is Monday 2024-03-18
	occs, err = s.Calc("lim-wd", 2024)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2024, time.March, 18), Chain: []string{"lim", "lim-wd"}},
	})

	occs, err = s.Calc("lim-e", 2024)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2024, time.March, 15), Chain: []string{"lim", "lim-e"}},
	})

	occs, err = s.Calc("lim-e", 2023)
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 0 {
		t.Errorf("got %v; want no occurrences when all sources are empty", occs)
	}
}

func TestDerivedSetWorkdaySearchExhausted(t *testing.T) {
	neverBase := NewBusinessCalendar()
	neverBase.WorkdayFunc = func(date time.Time) bool { return false }

	s, err := NewDerivedSet(
		&DerivedRule{Name: "ny", Kind: RuleLeaf, Holiday: testDayHoliday("New Year", time.January, 1, nil)},
		&DerivedRule{Name: "never", Kind: RuleWorkdayOffset, Source: Ref{Name: "ny", Kind: DateActual},
			Offset: 1, Base: neverBase, SearchDays: 30},
		&DerivedRule{Name: "never-day", Kind: RuleDayOffset, Source: Ref{Name: "never", Kind: DateActual}, Offset: 1},
		&DerivedRule{Name: "never-work", Kind: RuleWorkdayOffset, Source: Ref{Name: "never", Kind: DateActual},
			Offset: 2, Base: NewBusinessCalendar()},
		&DerivedRule{Name: "never-latest", Kind: RuleLatest, Sources: []Ref{
			{Name: "never", Kind: DateActual},
			{Name: "ny", Kind: DateActual},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, rule := range []string{"never", "never-day", "never-work", "never-latest"} {
		if _, err := s.Calc(rule, 2024); !errors.Is(err, ErrWorkdaySearchExhausted) {
			t.Errorf("%s: got %v; want ErrWorkdaySearchExhausted", rule, err)
		}
	}

	// the Holiday adapter reports calculation errors as the zero time
	h, err := s.Holiday("never")
	if err != nil {
		t.Fatal(err)
	}
	if act, _ := h.Calc(2024); !act.IsZero() {
		t.Errorf("got %s; want zero time", act)
	}
}

func TestDerivedSetBaseCalendarCycle(t *testing.T) {
	base := NewBusinessCalendar()
	s, err := NewDerivedSet(
		&DerivedRule{Name: "ny", Kind: RuleLeaf, Holiday: testDayHoliday("New Year", time.January, 1, nil)},
		&DerivedRule{Name: "wd", Kind: RuleWorkdayOffset, Source: Ref{Name: "ny", Kind: DateActual}, Offset: -1, Base: base},
	)
	if err != nil {
		t.Fatal(err)
	}

	h, err := s.Holiday("ny")
	if err != nil {
		t.Fatal(err)
	}
	base.AddHoliday(h)

	if _, err := s.Calc("wd", 2024); err == nil || !strings.Contains(err.Error(), "derived from this set") {
		t.Errorf("got %v; want hidden cycle error", err)
	}
}

func TestDerivedSetRecursion(t *testing.T) {
	var s *DerivedSet
	evil := &Holiday{
		Name: "Evil",
		Func: func(h *Holiday, year int) time.Time {
			if _, err := s.Calc("evil", year); err == nil {
				t.Error("Calc: expected recursion error")
			}
			if _, err := s.evalBaseYear("evil", year); err == nil {
				t.Error("evalBaseYear: expected recursion error")
			}
			return time.Date(year, time.June, 1, 0, 0, 0, 0, DefaultLoc)
		},
	}
	var err error
	s, err = NewDerivedSet(&DerivedRule{Name: "evil", Kind: RuleLeaf, Holiday: evil})
	if err != nil {
		t.Fatal(err)
	}

	occs, err := s.Calc("evil", 2024)
	if err != nil {
		t.Fatal(err)
	}
	checkOccurrences(t, occs, []Occurrence{
		{Date: testDate(2024, time.June, 1), Chain: []string{"evil"}},
	})
}

func TestDerivedSetUnknownRuleKind(t *testing.T) {
	s := &DerivedSet{
		rules:     map[string]*DerivedRule{"bad": {Name: "bad", Kind: RuleKind(99)}},
		shift:     map[string]int{},
		generated: map[*Holiday]string{},
	}
	if _, err := s.Calc("bad", 2024); err == nil {
		t.Error("expected unknown rule kind error")
	}
}

func TestDerivedSetHolidayAdapter(t *testing.T) {
	s, _ := newDerivedTestSet(t, NewBusinessCalendar())

	h, err := s.Holiday("ny-minus-2")
	if err != nil {
		t.Fatal(err)
	}
	act, obs := h.Calc(2024)
	want := testDate(2023, time.December, 30)
	if !act.Equal(want) {
		t.Errorf("actual: got %s; want %s", act, want)
	}
	if !obs.Equal(want) {
		t.Errorf("observed: got %s; want %s", obs, want)
	}
}
