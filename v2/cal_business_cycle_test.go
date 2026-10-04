// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"testing"
	"time"

	_ "time/tzdata"
)

func nyLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	return loc
}

// twoOnTwoOffCycle builds a calendar whose underlying rotation is two days on,
// two days off, anchored at anchor (index 0 = work).
func twoOnTwoOffCycle(anchor time.Time) *BusinessCalendar {
	c := NewBusinessCalendar()
	if err := c.SetWorkdayCycle(anchor, []bool{true, true, false, false}); err != nil {
		panic(err)
	}
	return c
}

func TestWorkdayCyclePhase(t *testing.T) {
	// Thursday 2024-03-07 anchors index 0 of a two-on/two-off rotation.
	anchor := d(2024, 3, 7)
	c := twoOnTwoOffCycle(anchor)

	tests := []struct {
		date time.Time
		want bool
	}{
		// leap day, short month, and negative (pre-anchor) indices
		{d(2024, 1, 31), true},  // -36 mod 4 = 0
		{d(2024, 2, 27), false}, // -9 mod 4 = 3
		{d(2024, 2, 28), true},  // -8 mod 4 = 0
		{d(2024, 2, 29), true},  // leap day, -7 mod 4 = 1
		{d(2024, 3, 1), false},  // -6 mod 4 = 2
		{d(2024, 3, 2), false},  // Saturday, -5 mod 4 = 3
		{d(2024, 3, 3), true},   // Sunday worked: rotation ignores weekends
		{d(2024, 3, 4), true},
		{d(2024, 3, 5), false},
		{d(2024, 3, 6), false}, // anchor minus one day: -1 mod 4 = 3

		// anchor and forward, crossing a long month boundary
		{d(2024, 3, 7), true},
		{d(2024, 3, 8), true},
		{d(2024, 3, 9), false},
		{d(2024, 3, 10), false},
		{d(2024, 3, 11), true},
		{d(2024, 3, 31), true}, // offset 24 mod 4 = 0
		{d(2024, 4, 1), true},
		{d(2024, 4, 2), false},

		// the cycle is N-day based, not weekly: both are Thursdays
		{d(2024, 3, 7), true},   // offset 0
		{d(2024, 3, 14), false}, // offset 7 mod 4 = 3
	}

	for _, test := range tests {
		if got := c.IsWorkday(test.date); got != test.want {
			t.Errorf("IsWorkday(%s) = %t; want %t", test.date.Format("2006-01-02"), got, test.want)
		}
	}
}

func TestWorkdayCycleMonthCounts(t *testing.T) {
	c := twoOnTwoOffCycle(d(2024, 3, 7))
	// March 2024: work pairs on 3-4, 7-8, 11-12, 15-16, 19-20, 23-24,
	// 27-28, plus 31 = 15 days.
	if got := c.WorkdaysInMonth(2024, time.March); got != 15 {
		t.Errorf("WorkdaysInMonth(March) = %d; want 15", got)
	}
}

func TestWorkdayCycleReplacesWeekdayFlags(t *testing.T) {
	c := twoOnTwoOffCycle(d(2024, 3, 7))
	// Sunday is a workday under the rotation even though the default flags
	// mark only Monday-Friday.
	if !c.IsWorkday(d(2024, 3, 3)) {
		t.Error("expected Sunday 2024-03-03 to be a rotation workday")
	}
}

func TestWorkdayCycleAcrossDST(t *testing.T) {
	ny := nyLoc(t)
	anchor := time.Date(2024, 3, 7, 12, 0, 0, 0, ny)
	c := twoOnTwoOffCycle(anchor)

	tests := []struct {
		name string
		date time.Time
		want bool
	}{
		{"day before spring-forward, rest", time.Date(2024, 3, 9, 12, 0, 0, 0, ny), false},
		{"spring-forward Sunday, rest", time.Date(2024, 3, 10, 15, 0, 0, 0, ny), false},
		// 2024-03-11 00:00 EDT is 95 hours after anchor noon, but four civil
		// dates later: index 0 must still resolve to a workday.
		{"after spring-forward counted by civil date", time.Date(2024, 3, 11, 0, 0, 0, 0, ny), true},
		{"same instant expressed in UTC", time.Date(2024, 3, 11, 4, 0, 0, 0, time.UTC), true},
		{"late evening UTC is still prior NY date", time.Date(2024, 3, 10, 4, 59, 59, 0, time.UTC), false},
		// fall-back day 2024-11-03 is 241 civil dates after the anchor
		// (241 mod 4 = 1); a 25/23 hour DST day must not shift the index.
		{"fall-back Sunday index", time.Date(2024, 11, 3, 12, 0, 0, 0, ny), true},
		{"day after fall-back", time.Date(2024, 11, 4, 12, 0, 0, 0, ny), false},
	}

	for _, test := range tests {
		if got := c.IsWorkday(test.date); got != test.want {
			t.Errorf("%s: IsWorkday(%s) = %t; want %t", test.name, test.date, got, test.want)
		}
	}
}

func TestWorkdayExceptionDoesNotShiftPhase(t *testing.T) {
	c := twoOnTwoOffCycle(d(2024, 3, 7))

	// Promote rest day 2024-03-09 (index 2) to a workday temporarily.
	c.SetWorkdayException(d(2024, 3, 9), true)
	if !c.IsWorkday(d(2024, 3, 9)) {
		t.Error("expected exception to make 2024-03-09 a workday")
	}
	// The next day must keep the original phase: index 3 is still rest.
	if c.IsWorkday(d(2024, 3, 10)) {
		t.Error("expected 2024-03-10 to remain a rest day per the original cycle")
	}
	if !c.IsWorkday(d(2024, 3, 11)) {
		t.Error("expected 2024-03-11 to remain a workday per the original cycle")
	}

	// Removing the exception restores the cycle verdict.
	c.RemoveWorkdayException(d(2024, 3, 9))
	if c.IsWorkday(d(2024, 3, 9)) {
		t.Error("expected 2024-03-09 to be a rest day after removing the exception")
	}

	// Grant an anchor workday off; the following day is unaffected.
	c.SetWorkdayException(d(2024, 3, 7), false)
	if c.IsWorkday(d(2024, 3, 7)) {
		t.Error("expected exception to make 2024-03-07 a rest day")
	}
	if !c.IsWorkday(d(2024, 3, 8)) {
		t.Error("expected 2024-03-08 to remain a workday")
	}

	c.ClearWorkdayExceptions()
	if !c.IsWorkday(d(2024, 3, 7)) || c.IsWorkday(d(2024, 3, 9)) {
		t.Error("expected ClearWorkdayExceptions to restore the cycle")
	}
}

func TestWorkdayExceptionUsesBindingCivilDate(t *testing.T) {
	ny := nyLoc(t)
	c := twoOnTwoOffCycle(time.Date(2024, 3, 7, 12, 0, 0, 0, ny))

	// 2024-03-10 04:00 UTC is 2024-03-09 23:00 in New York: the exception
	// must key the NY civil date (the 9th, a cycle rest day).
	c.SetWorkdayException(time.Date(2024, 3, 10, 4, 0, 0, 0, time.UTC), true)
	if !c.IsWorkday(time.Date(2024, 3, 9, 10, 0, 0, 0, ny)) {
		t.Error("expected NY 2024-03-09 to pick up the UTC-keyed exception")
	}
	// 2024-03-10 01:00 NY (06:00 UTC, still EST) is the next civil date and
	// has no exception.
	if c.IsWorkday(time.Date(2024, 3, 10, 1, 0, 0, 0, ny)) {
		t.Error("expected NY 2024-03-10 to be unaffected by the 9th's exception")
	}
}

func TestWorkdayExceptionOverridesHoliday(t *testing.T) {
	c := twoOnTwoOffCycle(d(2024, 3, 7))
	c.AddHoliday(&Holiday{
		Type:  ObservancePublic,
		Month: time.March,
		Day:   8,
		Func:  CalcDayOfMonth,
	})

	// Default policy suspends work on the holiday; the outermost exception
	// overrides even that.
	if c.IsWorkday(d(2024, 3, 8)) {
		t.Error("expected holiday 2024-03-08 to suspend work by default")
	}
	c.SetWorkdayException(d(2024, 3, 8), true)
	if !c.IsWorkday(d(2024, 3, 8)) {
		t.Error("expected exception to override the holiday suspension")
	}
}

func TestHolidaySuspendPolicy(t *testing.T) {
	hol8 := &Holiday{
		Type:  ObservancePublic,
		Month: time.March,
		Day:   8,
		Func:  CalcDayOfMonth,
	}
	hol9 := &Holiday{
		Type:  ObservancePublic,
		Month: time.March,
		Day:   9,
		Func:  CalcDayOfMonth,
	}

	suspend := twoOnTwoOffCycle(d(2024, 3, 7))
	suspend.AddHoliday(hol8, hol9)

	rotates := twoOnTwoOffCycle(d(2024, 3, 7))
	rotates.AddHoliday(hol8, hol9)
	rotates.SetHolidaysSuspendWork(false)

	if suspend.IsWorkday(d(2024, 3, 8)) {
		t.Error("suspend: holiday on a cycle workday must be off")
	}
	if suspend.IsWorkday(d(2024, 3, 9)) {
		t.Error("suspend: holiday on a cycle rest day must be off")
	}
	if !rotates.IsWorkday(d(2024, 3, 8)) {
		t.Error("rotate-through: holiday on a cycle workday must be worked")
	}
	if rotates.IsWorkday(d(2024, 3, 9)) {
		t.Error("rotate-through: a cycle rest day stays off even on a holiday")
	}
	// Neighboring dates keep their phase in both modes.
	if !suspend.IsWorkday(d(2024, 3, 11)) || !rotates.IsWorkday(d(2024, 3, 11)) {
		t.Error("expected 2024-03-11 to remain a workday regardless of policy")
	}
}

func TestSetWorkdayCycleRejectsEmpty(t *testing.T) {
	c := NewBusinessCalendar()
	if err := c.SetWorkdayCycle(d(2024, 3, 7), nil); !errors.Is(err, ErrEmptyWorkdayCycle) {
		t.Errorf("nil pattern: got %v; want ErrEmptyWorkdayCycle", err)
	}
	if err := c.SetWorkdayCycle(d(2024, 3, 7), []bool{}); !errors.Is(err, ErrEmptyWorkdayCycle) {
		t.Errorf("empty pattern: got %v; want ErrEmptyWorkdayCycle", err)
	}
	// A rejected configuration must leave the default week intact.
	if !c.IsWorkday(d(2024, 3, 7)) || c.IsWorkday(d(2024, 3, 9)) {
		t.Error("expected the default Mon-Fri flags to remain after a rejected cycle")
	}
}

func TestSetWorkdayCycleCopiesPattern(t *testing.T) {
	pattern := []bool{true, true, false, false}
	c := NewBusinessCalendar()
	if err := c.SetWorkdayCycle(d(2024, 3, 7), pattern); err != nil {
		t.Fatal(err)
	}

	// Mutate the caller's slice after creation; the calendar must not change.
	pattern[2] = true
	pattern[3] = true
	if c.IsWorkday(d(2024, 3, 9)) {
		t.Error("calendar changed after the caller mutated the pattern slice")
	}
	if c.IsWorkday(d(2024, 3, 10)) {
		t.Error("calendar changed after the caller mutated the pattern slice")
	}
}
