// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"testing"
	"time"

	// embed the IANA time zone database so tests do not depend on the host OS
	_ "time/tzdata"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

// at builds a time in the given location.
func at(loc *time.Location, y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, loc)
}

func intervalsEqual(t *testing.T, got []WorkInterval, want []WorkInterval) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("interval count: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if !got[i].Start.Equal(want[i].Start) || !got[i].End.Equal(want[i].End) {
			t.Errorf("interval %d: got [%s, %s), want [%s, %s)",
				i, got[i].Start, got[i].End, want[i].Start, want[i].End)
		}
		if !got[i].Start.Before(got[i].End) {
			t.Errorf("interval %d is not positive length: [%s, %s)", i, got[i].Start, got[i].End)
		}
	}
}

func TestLunchBreakWindows(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)
	// Mon-Fri: morning and afternoon sessions separated by a lunch break.
	for _, day := range []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday,
	} {
		if err := c.SetWeeklyWindows(day,
			LocalWindow{Start: 9 * time.Hour, End: 12 * time.Hour},
			LocalWindow{Start: 13 * time.Hour, End: 18 * time.Hour},
		); err != nil {
			t.Fatal(err)
		}
	}

	ivs, err := c.WorkIntervals(at(loc, 2024, time.May, 15, 0, 0), at(loc, 2024, time.May, 16, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.May, 15, 9, 0), End: at(loc, 2024, time.May, 15, 12, 0)},
		{Start: at(loc, 2024, time.May, 15, 13, 0), End: at(loc, 2024, time.May, 15, 18, 0)},
	})

	// a query strictly inside the lunch break returns no work time
	ivs, err = c.WorkIntervals(at(loc, 2024, time.May, 15, 12, 30), at(loc, 2024, time.May, 15, 12, 45))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, nil)

	// half-open clipping at the range boundaries
	ivs, err = c.WorkIntervals(at(loc, 2024, time.May, 15, 11, 0), at(loc, 2024, time.May, 15, 14, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.May, 15, 11, 0), End: at(loc, 2024, time.May, 15, 12, 0)},
		{Start: at(loc, 2024, time.May, 15, 13, 0), End: at(loc, 2024, time.May, 15, 14, 0)},
	})

	// Every interval corresponds to a concrete instant: Shanghai is UTC+8 with
	// no DST, so the morning session is 01:00-04:00 UTC.
	ivs, err = c.WorkIntervals(at(loc, 2024, time.May, 15, 0, 0), at(loc, 2024, time.May, 16, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2024, time.May, 15, 1, 0, 0, 0, time.UTC)
	if got := ivs[0].Start.UTC(); !got.Equal(wantStart) {
		t.Errorf("first start in UTC: got %s, want %s", got, wantStart)
	}
}

func TestFridayNightShiftSurvivesSaturdayHoliday(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)
	// Friday overnight shift runs 22:00 Friday through 06:00 Saturday.
	if err := c.SetWeeklyWindows(time.Friday,
		LocalWindow{Start: 22 * time.Hour, End: 6 * time.Hour, EndDayOffset: 1},
	); err != nil {
		t.Fatal(err)
	}
	c.AddHoliday(&Holiday{
		Type:  ObservancePublic,
		Month: time.May,
		Day:   18, // Saturday is a holiday
		Func:  CalcDayOfMonth,
	})

	ivs, err := c.WorkIntervals(at(loc, 2024, time.May, 17, 0, 0), at(loc, 2024, time.May, 19, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	// The crossing-midnight portion on the holiday Saturday is preserved.
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.May, 17, 22, 0), End: at(loc, 2024, time.May, 18, 6, 0)},
	})

	// The holiday Saturday only contains the tail carried over from Friday; no
	// new shift starts on Saturday.
	ivs, err = c.WorkIntervals(at(loc, 2024, time.May, 18, 6, 0), at(loc, 2024, time.May, 19, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, nil)
}

func TestNightShiftAcrossYearBoundary(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)
	if err := c.SetWeeklyWindows(time.Friday,
		LocalWindow{Start: 22 * time.Hour, End: 6 * time.Hour, EndDayOffset: 1},
	); err != nil {
		t.Fatal(err)
	}
	c.AddHoliday(&Holiday{
		Type:  ObservancePublic,
		Month: time.January,
		Day:   1, // New Year's Day (Saturday) is a holiday
		Func:  CalcDayOfMonth,
	})

	ivs, err := c.WorkIntervals(at(loc, 2021, time.December, 31, 0, 0), at(loc, 2022, time.January, 2, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2021, time.December, 31, 22, 0), End: at(loc, 2022, time.January, 1, 6, 0)},
	})

	// The interval corresponds to 8 real hours regardless of the date rollover.
	if d := ivs[0].End.Sub(ivs[0].Start); d != 8*time.Hour {
		t.Errorf("duration: got %s, want 8h", d)
	}
}

func TestDateExceptionOverridesWeeklyRule(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)
	for _, day := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		if err := c.SetWeeklyWindows(day, LocalWindow{Start: 9 * time.Hour, End: 17 * time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	c.AddHoliday(&Holiday{
		Type:  ObservancePublic,
		Month: time.May,
		Day:   15,
		Func:  CalcDayOfMonth,
	})

	// Holiday Wednesday: weekly rule is suppressed.
	ivs, err := c.WorkIntervals(at(loc, 2024, time.May, 15, 0, 0), at(loc, 2024, time.May, 16, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, nil)

	// A date exception on the holiday replaces the weekly rule and is honored.
	if err := c.SetDateWindows(at(loc, 2024, time.May, 15, 0, 0),
		LocalWindow{Start: 20 * time.Hour, End: 23 * time.Hour},
	); err != nil {
		t.Fatal(err)
	}
	ivs, err = c.WorkIntervals(at(loc, 2024, time.May, 15, 0, 0), at(loc, 2024, time.May, 16, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.May, 15, 20, 0), End: at(loc, 2024, time.May, 15, 23, 0)},
	})

	// Clearing the exception restores the (suppressed) weekly behavior.
	if err := c.SetDateWindows(at(loc, 2024, time.May, 15, 0, 0)); err != nil {
		t.Fatal(err)
	}
	ivs, err = c.WorkIntervals(at(loc, 2024, time.May, 15, 0, 0), at(loc, 2024, time.May, 16, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, nil)
}

func TestMergesOverlappingWindows(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)
	if err := c.SetWeeklyWindows(time.Wednesday,
		LocalWindow{Start: 9 * time.Hour, End: 12 * time.Hour},
		LocalWindow{Start: 11 * time.Hour, End: 14 * time.Hour}, // overlaps
		LocalWindow{Start: 14 * time.Hour, End: 18 * time.Hour}, // touches
	); err != nil {
		t.Fatal(err)
	}

	ivs, err := c.WorkIntervals(at(loc, 2024, time.May, 15, 0, 0), at(loc, 2024, time.May, 16, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.May, 15, 9, 0), End: at(loc, 2024, time.May, 15, 18, 0)},
	})
}

func TestRejectsZeroLengthWindow(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)

	if err := c.SetWeeklyWindows(time.Monday, LocalWindow{Start: 9 * time.Hour, End: 9 * time.Hour}); !errors.Is(err, ErrZeroWindow) {
		t.Fatalf("same-day zero length: got %v, want ErrZeroWindow", err)
	}
	if err := c.SetWeeklyWindows(time.Monday, LocalWindow{Start: 9 * time.Hour, End: 8 * time.Hour}); !errors.Is(err, ErrZeroWindow) {
		t.Fatalf("reversed same-day: got %v, want ErrZeroWindow", err)
	}
	if err := c.SetDateWindows(at(loc, 2024, time.May, 15, 0, 0),
		LocalWindow{Start: 0, End: 0, EndDayOffset: 1},
	); !errors.Is(err, ErrZeroWindow) {
		t.Fatalf("overnight midnight-to-midnight: got %v, want ErrZeroWindow", err)
	}
}

func TestRequiresLocation(t *testing.T) {
	c := NewBusinessCalendar()
	if err := c.SetWeeklyWindows(time.Monday, LocalWindow{Start: 9 * time.Hour, End: 17 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	_, err := c.WorkIntervals(at(time.UTC, 2024, time.May, 13, 0, 0), at(time.UTC, 2024, time.May, 14, 0, 0))
	if !errors.Is(err, ErrNoLocation) {
		t.Fatalf("got %v, want ErrNoLocation", err)
	}
}

func TestLegacySetWorkHoursViaIntervals(t *testing.T) {
	loc := mustLoc(t, "Asia/Shanghai")
	c := NewBusinessCalendarIn(loc)
	c.SetWorkHours(9*time.Hour, 17*time.Hour)

	ivs, err := c.WorkIntervals(at(loc, 2024, time.May, 13, 0, 0), at(loc, 2024, time.May, 14, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.May, 13, 9, 0), End: at(loc, 2024, time.May, 13, 17, 0)},
	})

	// WorkHoursInRange keeps working and reports real elapsed time.
	got := c.WorkHoursInRange(at(loc, 2024, time.May, 13, 9, 0), at(loc, 2024, time.May, 13, 17, 0))
	if got != 8*time.Hour {
		t.Fatalf("legacy hours: got %s, want 8h", got)
	}
}

func TestSpringForwardSkipsMissingHour(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	c := NewBusinessCalendarIn(loc)
	// 2024-03-10: clocks jump 02:00 -> 03:00. A shift starting at the
	// non-existent 02:30 has an ambiguous start boundary. A date exception
	// keeps the test independent of weekday rules.
	if err := c.SetDateWindows(at(loc, 2024, time.March, 10, 0, 0),
		LocalWindow{Start: 150 * time.Minute, End: 5 * time.Hour},
	); err != nil {
		t.Fatal(err)
	}

	// Default policy: ambiguity is an error.
	_, err := c.WorkIntervals(at(loc, 2024, time.March, 10, 0, 0), at(loc, 2024, time.March, 11, 0, 0))
	if !errors.Is(err, ErrAmbiguousTime) {
		t.Fatalf("default policy: got %v, want ErrAmbiguousTime", err)
	}

	// Skip policy drops the window entirely.
	c.SetDSTPolicy(AmbiguousSkip)
	ivs, err := c.WorkIntervals(at(loc, 2024, time.March, 10, 0, 0), at(loc, 2024, time.March, 11, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, nil)

	// Earlier/Later collapse the gap boundary to the 03:00 transition instant.
	c.SetDSTPolicy(AmbiguousEarlier)
	ivs, err = c.WorkIntervals(at(loc, 2024, time.March, 10, 0, 0), at(loc, 2024, time.March, 11, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.March, 10, 3, 0), End: at(loc, 2024, time.March, 10, 5, 0)},
	})
}

func TestFallBackFoldPolicy(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	c := NewBusinessCalendarIn(loc)
	// 2024-11-03: 01:00-02:00 repeats. Start at the first 01:30 (EDT), end at
	// 03:00 which is unambiguous.
	if err := c.SetDateWindows(at(loc, 2024, time.November, 3, 0, 0),
		LocalWindow{Start: 90 * time.Minute, End: 3 * time.Hour},
	); err != nil {
		t.Fatal(err)
	}

	_, err := c.WorkIntervals(at(loc, 2024, time.November, 3, 0, 0), at(loc, 2024, time.November, 4, 0, 0))
	if !errors.Is(err, ErrAmbiguousTime) {
		t.Fatalf("default policy: got %v, want ErrAmbiguousTime", err)
	}

	c.SetDSTPolicy(AmbiguousEarlier)
	ivs, err := c.WorkIntervals(at(loc, 2024, time.November, 3, 0, 0), at(loc, 2024, time.November, 4, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	// First 01:30 EDT (05:30 UTC) to 03:00 EST (08:00 UTC) = 2h30m.
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.November, 3, 1, 30), End: at(loc, 2024, time.November, 3, 3, 0)},
	})
	if d := ivs[0].End.Sub(ivs[0].Start); d != 150*time.Minute {
		t.Errorf("earlier duration: got %s, want 2h30m", d)
	}

	c.SetDSTPolicy(AmbiguousLater)
	ivs, err = c.WorkIntervals(at(loc, 2024, time.November, 3, 0, 0), at(loc, 2024, time.November, 4, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Second 01:30 EST (06:30 UTC) to 03:00 = 1h30m.
	if d := ivs[0].End.Sub(ivs[0].Start); d != 90*time.Minute {
		t.Errorf("later duration: got %s, want 1h30m", d)
	}
}

func TestSpringForwardNightShiftDuration(t *testing.T) {
	loc := mustLoc(t, "America/New_York")
	c := NewBusinessCalendarIn(loc)
	c.SetDSTPolicy(AmbiguousLater)
	// Overnight shift 2024-03-09 23:00 -> 2024-03-10 03:30. The nominal 4.5
	// hour shift loses the skipped 02:00-03:00 hour, leaving 3.5 real hours.
	if err := c.SetDateWindows(at(loc, 2024, time.March, 9, 0, 0),
		LocalWindow{Start: 23 * time.Hour, End: 210 * time.Minute, EndDayOffset: 1},
	); err != nil {
		t.Fatal(err)
	}

	ivs, err := c.WorkIntervals(at(loc, 2024, time.March, 9, 0, 0), at(loc, 2024, time.March, 11, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	intervalsEqual(t, ivs, []WorkInterval{
		{Start: at(loc, 2024, time.March, 9, 23, 0), End: at(loc, 2024, time.March, 10, 3, 30)},
	})
	if d := ivs[0].End.Sub(ivs[0].Start); d != 210*time.Minute {
		t.Errorf("duration: got %s, want 3h30m", d)
	}
}
