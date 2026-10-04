// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"testing"
	"time"
)

func TestSetShiftRotationEmpty(t *testing.T) {
	c := NewBusinessCalendar()
	if err := c.SetShiftRotation(d(2021, 1, 1), nil, false); !errors.Is(err, ErrEmptyRotation) {
		t.Errorf("got: %v; want: %v", err, ErrEmptyRotation)
	}
	if err := c.SetShiftRotation(d(2021, 1, 1), []bool{}, false); !errors.Is(err, ErrEmptyRotation) {
		t.Errorf("got: %v; want: %v", err, ErrEmptyRotation)
	}
	if c.rotation != nil {
		t.Error("rotation should not be configured after a rejected pattern")
	}
	// the calendar still falls back to the standard weekday flags
	if !c.IsWorkday(d(2021, 1, 1)) || c.IsWorkday(d(2021, 1, 2)) {
		t.Error("weekday flags should still apply when no rotation is configured")
	}
}

func TestShiftRotationPhase(t *testing.T) {
	c := NewBusinessCalendar()
	// two on, two off; anchor is a Saturday to prove the rotation replaces
	// the weekday flags
	if err := c.SetShiftRotation(d(2021, 1, 30), []bool{true, true, false, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		d    time.Time
		want bool
	}{
		// dates before the anchor use the non-negative remainder
		{d(2021, 1, 26), true},  // index 0
		{d(2021, 1, 27), true},  // index 1
		{d(2021, 1, 28), false}, // index 2
		{d(2021, 1, 29), false}, // index 3
		// anchor is index 0, even though it is a Saturday
		{d(2021, 1, 30), true},
		{d(2021, 1, 31), true},
		// long month boundary: January 31 -> February 1
		{d(2021, 2, 1), false},
		{d(2021, 2, 2), false},
		{d(2021, 2, 3), true},
		{d(2021, 2, 4), true},
	}

	for _, test := range tests {
		got := c.IsWorkday(test.d)
		if got != test.want {
			t.Errorf("got: %t; want: %t (%s)", got, test.want, test.d)
		}
	}
}

func TestShiftRotationMonthLengths(t *testing.T) {
	april := NewBusinessCalendar()
	if err := april.SetShiftRotation(d(2021, 4, 29), []bool{true, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	december := NewBusinessCalendar()
	if err := december.SetShiftRotation(d(2021, 12, 30), []bool{true, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		c    *BusinessCalendar
		d    time.Time
		want bool
	}{
		// short month boundary: April 30 -> May 1
		{april, d(2021, 4, 29), true},
		{april, d(2021, 4, 30), false},
		{april, d(2021, 5, 1), true},
		{april, d(2021, 5, 2), false},
		// year boundary: December 31 -> January 1
		{december, d(2021, 12, 30), true},
		{december, d(2021, 12, 31), false},
		{december, d(2022, 1, 1), true},
		{december, d(2022, 1, 2), false},
	}

	for _, test := range tests {
		got := test.c.IsWorkday(test.d)
		if got != test.want {
			t.Errorf("got: %t; want: %t (%s)", got, test.want, test.d)
		}
	}
}

func TestShiftRotationLeapDay(t *testing.T) {
	leap := NewBusinessCalendar()
	if err := leap.SetShiftRotation(d(2020, 2, 27), []bool{true, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plain := NewBusinessCalendar()
	if err := plain.SetShiftRotation(d(2021, 2, 27), []bool{true, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		c    *BusinessCalendar
		d    time.Time
		want bool
	}{
		// leap year: February 29 counts as a cycle day
		{leap, d(2020, 2, 27), true},
		{leap, d(2020, 2, 28), false},
		{leap, d(2020, 2, 29), true},
		{leap, d(2020, 3, 1), false},
		{leap, d(2020, 3, 2), true},
		// non-leap year: February 28 flows directly into March 1
		{plain, d(2021, 2, 27), true},
		{plain, d(2021, 2, 28), false},
		{plain, d(2021, 3, 1), true},
		{plain, d(2021, 3, 2), false},
	}

	for _, test := range tests {
		got := test.c.IsWorkday(test.d)
		if got != test.want {
			t.Errorf("got: %t; want: %t (%s)", got, test.want, test.d)
		}
	}
}

func TestShiftRotationDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("cannot load location: %v", err)
	}
	at := func(y, m, day, h, min int) time.Time {
		return time.Date(y, time.Month(m), day, h, min, 0, 0, loc)
	}

	spring := NewBusinessCalendar()
	// DST starts 2021-03-14; that day has only 23 hours
	if err := spring.SetShiftRotation(at(2021, 3, 13, 12, 0), []bool{true, true, false, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fall := NewBusinessCalendar()
	// DST ends 2021-11-07; that day has 25 hours
	if err := fall.SetShiftRotation(at(2021, 11, 6, 12, 0), []bool{true, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		c    *BusinessCalendar
		d    time.Time
		want bool
	}{
		{spring, at(2021, 3, 13, 0, 0), true},
		{spring, at(2021, 3, 14, 0, 0), true},
		{spring, at(2021, 3, 15, 0, 0), false},
		{spring, at(2021, 3, 16, 0, 0), false},
		{spring, at(2021, 3, 17, 0, 0), true},

		{fall, at(2021, 11, 6, 0, 0), true},
		{fall, at(2021, 11, 7, 0, 0), false},
		{fall, at(2021, 11, 8, 0, 0), true},
		{fall, at(2021, 11, 9, 0, 0), false},

		// instants from other timezones are converted to the bound
		// timezone before the phase is computed
		{spring, time.Date(2021, 3, 14, 4, 0, 0, 0, time.UTC), true},  // still Mar 13 in New York
		{spring, time.Date(2021, 3, 14, 5, 0, 0, 0, time.UTC), true},  // Mar 14 in New York
		{spring, time.Date(2021, 3, 13, 4, 0, 0, 0, time.UTC), false}, // still Mar 12 in New York
	}

	for _, test := range tests {
		got := test.c.IsWorkday(test.d)
		if got != test.want {
			t.Errorf("got: %t; want: %t (%s)", got, test.want, test.d)
		}
	}
}

func TestShiftRotationHolidays(t *testing.T) {
	hol := &Holiday{
		Type:  ObservancePublic,
		Month: time.July,
		Day:   4,
		Func:  CalcDayOfMonth,
	}

	override := NewBusinessCalendar()
	override.AddHoliday(hol)
	if err := override.SetShiftRotation(d(2021, 7, 4), []bool{true, false}, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotate := NewBusinessCalendar()
	rotate.AddHoliday(hol)
	if err := rotate.SetShiftRotation(d(2021, 7, 4), []bool{true, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		c    *BusinessCalendar
		d    time.Time
		want bool
	}{
		// the holiday stops work even though the pattern says work
		{override, d(2021, 7, 3), false},
		{override, d(2021, 7, 4), false},
		{override, d(2021, 7, 5), false},
		{override, d(2021, 7, 6), true},
		// the rotation continues as usual on the holiday
		{rotate, d(2021, 7, 3), false},
		{rotate, d(2021, 7, 4), true},
		{rotate, d(2021, 7, 5), false},
		{rotate, d(2021, 7, 6), true},
	}

	for _, test := range tests {
		got := test.c.IsWorkday(test.d)
		if got != test.want {
			t.Errorf("got: %t; want: %t (%s)", got, test.want, test.d)
		}
	}
}

func TestShiftException(t *testing.T) {
	c := NewBusinessCalendar()
	if err := c.SetShiftRotation(d(2021, 6, 1), []bool{true, true, false, false}, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// June 3 is a rest day (index 2); temporarily make it a workday
	if err := c.SetShiftException(d(2021, 6, 3), true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// June 6 is a workday (index 1); temporarily make it a rest day
	if err := c.SetShiftException(d(2021, 6, 6), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		d    time.Time
		want bool
	}{
		{d(2021, 6, 1), true},
		{d(2021, 6, 2), true},
		{d(2021, 6, 3), true},  // overridden rest day
		{d(2021, 6, 4), false}, // phase is not moved by the exception
		{d(2021, 6, 5), true},
		{d(2021, 6, 6), false}, // overridden workday
		{d(2021, 6, 7), false}, // phase is still intact
		{d(2021, 6, 8), false},
		{d(2021, 6, 9), true},
	}

	for _, test := range tests {
		got := c.IsWorkday(test.d)
		if got != test.want {
			t.Errorf("got: %t; want: %t (%s)", got, test.want, test.d)
		}
	}

	// clearing the exception restores the original cycle result
	c.ClearShiftException(d(2021, 6, 3))
	if c.IsWorkday(d(2021, 6, 3)) {
		t.Error("cleared exception should restore the cycle result")
	}
	if !c.IsWorkday(d(2021, 6, 9)) {
		t.Error("clearing an exception should not move the cycle phase")
	}
}

func TestShiftExceptionOverridesHoliday(t *testing.T) {
	c := NewBusinessCalendar()
	c.AddHoliday(&Holiday{
		Type:  ObservancePublic,
		Month: time.June,
		Day:   1,
		Func:  CalcDayOfMonth,
	})
	if err := c.SetShiftRotation(d(2021, 6, 1), []bool{true, false}, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// the holiday stops work on a rotation workday
	if c.IsWorkday(d(2021, 6, 1)) {
		t.Error("holiday should stop work when holidaysOverride is true")
	}
	// a temporary exception is the outermost layer and wins over the holiday
	if err := c.SetShiftException(d(2021, 6, 1), true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.IsWorkday(d(2021, 6, 1)) {
		t.Error("exception should override holiday handling")
	}
	if c.IsWorkday(d(2021, 6, 2)) {
		t.Error("exception should not move the cycle phase of later dates")
	}
}

func TestShiftExceptionWithoutRotation(t *testing.T) {
	c := NewBusinessCalendar()
	if err := c.SetShiftException(d(2021, 6, 3), true); !errors.Is(err, ErrNoRotation) {
		t.Errorf("got: %v; want: %v", err, ErrNoRotation)
	}
	// clearing without a rotation must not panic
	c.ClearShiftException(d(2021, 6, 3))
}

func TestShiftRotationCopiesPattern(t *testing.T) {
	pattern := []bool{true, false}
	c := NewBusinessCalendar()
	if err := c.SetShiftRotation(d(2021, 1, 1), pattern, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// mutating the caller's slice must not change the calendar
	pattern[0] = false
	pattern[1] = true

	if !c.IsWorkday(d(2021, 1, 1)) {
		t.Error("anchor day should remain a workday after the input slice was mutated")
	}
	if c.IsWorkday(d(2021, 1, 2)) {
		t.Error("the day after the anchor should remain a rest day after the input slice was mutated")
	}
}
