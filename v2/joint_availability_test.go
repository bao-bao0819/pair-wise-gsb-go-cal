// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"testing"
	"time"
)

func jointCalendar(id string, loc *time.Location, workHours func(*BusinessCalendar)) JointCalendar {
	calendar := NewBusinessCalendar()
	if workHours != nil {
		workHours(calendar)
	}
	return JointCalendar{ID: id, Calendar: calendar, Location: loc}
}

func utcTime(year, month, day, hour, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
}

func TestEarliestJointAvailabilityTimezonesAndLocalBoundaries(t *testing.T) {
	tokyo := time.FixedZone("Tokyo", 9*60*60)
	newYork := time.FixedZone("New York", -5*60*60)

	start := utcTime(2026, 1, 5, 0, 0)
	end := utcTime(2026, 1, 7, 0, 0)

	window, err := EarliestJointAvailability([]JointCalendar{
		jointCalendar("tyo", tokyo, func(calendar *BusinessCalendar) {
			calendar.SetWorkHours(7*time.Hour, 24*time.Hour)
		}),
		jointCalendar("nyc", newYork, nil),
	}, start, end, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantStart := utcTime(2026, 1, 5, 14, 0)
	if !window.Start.Equal(wantStart) {
		t.Fatalf("got start %s; want %s", window.Start, wantStart)
	}
	if window.End.Sub(window.Start) != time.Hour {
		t.Fatalf("got duration %s; want 1h", window.End.Sub(window.Start))
	}
	if len(window.Participants) != 2 || window.Participants[0] != "nyc" || window.Participants[1] != "tyo" {
		t.Fatalf("got participants %v; want nyc and tyo", window.Participants)
	}

	boundaries := map[string]LocalBoundary{}
	for _, boundary := range window.LocalBoundaries {
		boundaries[boundary.CalendarID] = boundary
	}

	tyoStart := boundaries["tyo"].Start
	if tyoStart.Hour() != 23 || tyoStart.Minute() != 0 || tyoStart.Location() != tokyo {
		t.Fatalf("got Tokyo start %v; want 23:00 Tokyo", tyoStart)
	}
	nycStart := boundaries["nyc"].Start
	if nycStart.Hour() != 9 || nycStart.Minute() != 0 || nycStart.Location() != newYork {
		t.Fatalf("got New York start %v; want 09:00 New York", nycStart)
	}
}

func TestAvailabilityWindowsWeekendHolidayAndOrder(t *testing.T) {
	locA := time.FixedZone("A", 0)
	locB := time.FixedZone("B", 2*60*60)

	saturdayWorkday := func(calendar *BusinessCalendar) {
		calendar.SetWorkday(time.Saturday, true)
		calendar.SetWorkday(time.Sunday, false)
		calendar.SetWorkHours(9*time.Hour, 15*time.Hour)
	}

	withHoliday := func(calendar *BusinessCalendar) {
		calendar.AddHoliday(&Holiday{
			Type:  ObservancePublic,
			Month: time.January,
			Day:   5,
			Func:  CalcDayOfMonth,
		})
	}

	calendars := []JointCalendar{
		jointCalendar("saturday", locA, saturdayWorkday),
		jointCalendar("holiday", locB, withHoliday),
	}
	start := utcTime(2026, 1, 3, 0, 0)
	end := utcTime(2026, 1, 11, 0, 0)

	windows, err := AvailabilityWindows(calendars, start, end, 2*time.Hour, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(windows) == 0 {
		t.Fatal("expected availability after weekend and holiday")
	}

	wantStart := utcTime(2026, 1, 6, 9, 0)
	if !windows[0].Start.Equal(wantStart) {
		t.Fatalf("got start %s; want %s", windows[0].Start, wantStart)
	}

	reversed := []JointCalendar{calendars[1], calendars[0]}
	reversedWindows, err := AvailabilityWindows(reversed, start, end, 2*time.Hour, len(reversed))
	if err != nil {
		t.Fatalf("unexpected reversed error: %v", err)
	}
	if !reversedWindows[0].Start.Equal(windows[0].Start) {
		t.Fatalf("reversed start %s differs from %s", reversedWindows[0].Start, windows[0].Start)
	}
}

func TestAvailabilityWindowsDoesNotCombineAcrossLunchBreak(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	calendar := NewBusinessCalendar()
	calendar.WorkdayIntervalsFunc = func(date time.Time) []WorkTimeInterval {
		return []WorkTimeInterval{
			{Start: time.Date(date.Year(), date.Month(), date.Day(), 9, 0, 0, 0, loc), End: time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, loc)},
			{Start: time.Date(date.Year(), date.Month(), date.Day(), 13, 0, 0, 0, loc), End: time.Date(date.Year(), date.Month(), date.Day(), 17, 0, 0, 0, loc)},
		}
	}

	start := utcTime(2026, 1, 5, 0, 0)
	end := utcTime(2026, 1, 6, 0, 0)
	calendars := []JointCalendar{{ID: "lunch", Calendar: calendar, Location: loc}}

	windows, err := AvailabilityWindows(calendars, start, end, 4*time.Hour, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(windows) != 1 || !windows[0].Start.Equal(utcTime(2026, 1, 5, 13, 0)) {
		t.Fatalf("got windows %v; want only the continuous afternoon window", windows)
	}

	_, err = EarliestAvailability(calendars, start, end, 5*time.Hour, 1)
	if !errors.Is(err, ErrNoJointAvailability) {
		t.Fatalf("got error %v; want ErrNoJointAvailability", err)
	}
}

func TestAvailabilityWindowsSegmentsByParticipants(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	early := jointCalendar("early", loc, func(calendar *BusinessCalendar) {
		calendar.SetWorkHours(8*time.Hour, 11*time.Hour)
	})
	late := jointCalendar("late", loc, func(calendar *BusinessCalendar) {
		calendar.SetWorkHours(10*time.Hour, 13*time.Hour)
	})

	start := utcTime(2026, 1, 5, 0, 0)
	end := utcTime(2026, 1, 5, 14, 0)
	windows, err := AvailabilityWindows([]JointCalendar{late, early}, start, end, time.Hour, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(windows) != 3 {
		t.Fatalf("got %d windows; want 3 participant-set segments", len(windows))
	}

	wantParticipants := [][]string{{"early"}, {"early", "late"}, {"late"}}
	for index, want := range wantParticipants {
		if len(windows[index].Participants) != len(want) {
			t.Fatalf("segment %d got %v; want %v", index, windows[index].Participants, want)
		}
		for participantIndex, participant := range want {
			if windows[index].Participants[participantIndex] != participant {
				t.Fatalf("segment %d got %v; want %v", index, windows[index].Participants, want)
			}
		}
	}

	earliest, err := EarliestAvailability([]JointCalendar{late, early}, start, end, time.Hour, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !earliest.Start.Equal(utcTime(2026, 1, 5, 8, 0)) ||
		!earliest.End.Equal(utcTime(2026, 1, 5, 9, 0)) {
		t.Fatalf("got earliest %s-%s; want 08:00-09:00", earliest.Start, earliest.End)
	}
}

func TestAvailabilityDST(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("America/New_York timezone unavailable: %v", err)
	}

	calendars := []JointCalendar{
		jointCalendar("nyc", newYork, func(calendar *BusinessCalendar) {
			calendar.SetWorkHours(9*time.Hour, 17*time.Hour)
		}),
	}
	start := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	window, err := EarliestJointAvailability(calendars, start, end, 7*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	localStart := window.Start.In(newYork)
	localEnd := window.End.In(newYork)
	if localStart.Hour() != 9 || localEnd.Hour() != 16 {
		t.Fatalf("got local window %s-%s; want 09:00-16:00 wall time after the skipped hour", localStart, localEnd)
	}
	if window.End.Sub(window.Start) != 7*time.Hour {
		t.Fatalf("got absolute duration %s; want 7h on spring-forward day", window.End.Sub(window.Start))
	}
}

func TestAvailabilityDoesNotChangeDefaultLocation(t *testing.T) {
	original := DefaultLoc
	loc := time.FixedZone("UTC", 0)
	_, _ = EarliestJointAvailability(
		[]JointCalendar{jointCalendar("utc", loc, nil)},
		utcTime(2026, 1, 5, 0, 0), utcTime(2026, 1, 5, 12, 0),
		time.Hour,
	)
	if DefaultLoc != original {
		t.Fatal("DefaultLoc was changed by availability query")
	}
}

func TestAvailabilityInputErrors(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	calendar := jointCalendar("a", loc, nil)
	start := utcTime(2026, 1, 5, 0, 0)
	end := utcTime(2026, 1, 6, 0, 0)

	tests := []struct {
		name            string
		calendars       []JointCalendar
		start           time.Time
		end             time.Time
		duration        time.Duration
		minParticipants int
		want            error
	}{
		{"empty calendars", nil, start, end, time.Hour, 0, ErrNoCalendars},
		{"empty id", []JointCalendar{{ID: "", Calendar: NewBusinessCalendar(), Location: loc}}, start, end, time.Hour, 1, ErrEmptyCalendarID},
		{"duplicate id", []JointCalendar{calendar, calendar}, start, end, time.Hour, 2, ErrDuplicateCalendarID},
		{"nil calendar", []JointCalendar{{ID: "bad", Location: loc}}, start, end, time.Hour, 1, ErrNilCalendar},
		{"nil location", []JointCalendar{{ID: "bad", Calendar: NewBusinessCalendar()}}, start, end, time.Hour, 1, ErrNilLocation},
		{"invalid range", []JointCalendar{calendar}, end, start, time.Hour, 1, ErrInvalidTimeRange},
		{"zero duration", []JointCalendar{calendar}, start, end, 0, 1, ErrInvalidDuration},
		{"k too high", []JointCalendar{calendar}, start, end, time.Hour, 2, ErrInvalidMinimumParticipants},
		{"k negative", []JointCalendar{calendar}, start, end, time.Hour, -1, ErrInvalidMinimumParticipants},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := AvailabilityWindows(test.calendars, test.start, test.end, test.duration, test.minParticipants)
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v; want %v", err, test.want)
			}
		})
	}
}

func TestAvailabilityInvalidWorkIntervals(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	calendar := NewBusinessCalendar()
	calendar.WorkdayIntervalsFunc = func(date time.Time) []WorkTimeInterval {
		return []WorkTimeInterval{
			{Start: time.Date(2026, time.January, 5, 13, 0, 0, 0, loc), End: time.Date(2026, time.January, 5, 12, 0, 0, 0, loc)},
		}
	}

	_, err := AvailabilityWindows(
		[]JointCalendar{{ID: "bad", Calendar: calendar, Location: loc}},
		utcTime(2026, 1, 5, 0, 0), utcTime(2026, 1, 6, 0, 0),
		time.Hour, 1,
	)
	if !errors.Is(err, ErrInvalidWorkInterval) {
		t.Fatalf("got %v; want ErrInvalidWorkInterval", err)
	}
}

func TestAvailabilityNoEvents(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	calendar := NewBusinessCalendar()
	for day := time.Monday; day <= time.Friday; day++ {
		calendar.SetWorkday(day, false)
	}
	calendar.SetWorkday(time.Saturday, false)
	calendar.SetWorkday(time.Sunday, true)

	windows, err := AvailabilityWindows(
		[]JointCalendar{{ID: "sunday-only", Calendar: calendar, Location: loc}},
		utcTime(2026, 1, 5, 0, 0), utcTime(2026, 1, 10, 0, 0),
		time.Hour, 1,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if windows != nil {
		t.Fatalf("got windows %v; want nil when no intervals exist", windows)
	}
}

func TestWorkdayIntervalsEndOfDay(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	calendar := NewBusinessCalendar()
	calendar.SetWorkHours(6*time.Hour, 24*time.Hour)

	date := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
	intervals := calendar.WorkdayIntervals(date)
	if len(intervals) != 1 {
		t.Fatalf("got %d intervals; want 1", len(intervals))
	}
	wantEnd := time.Date(2026, time.January, 6, 0, 0, 0, 0, loc)
	if !intervals[0].End.Equal(wantEnd) {
		t.Fatalf("got end %s; want midnight %s", intervals[0].End, wantEnd)
	}
}

func TestWorkdayIntervalsCustomFunctionsAndInvalidRanges(t *testing.T) {
	loc := time.FixedZone("UTC", 0)

	t.Run("custom start and end functions", func(t *testing.T) {
		calendar := NewBusinessCalendar()
		calendar.WorkdayStartFunc = func(date time.Time) time.Time {
			return time.Date(date.Year(), date.Month(), date.Day(), 10, 15, 30, 0, loc)
		}
		calendar.WorkdayEndFunc = func(date time.Time) time.Time {
			return time.Date(date.Year(), date.Month(), date.Day(), 15, 45, 30, 0, loc)
		}

		date := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
		intervals := calendar.WorkdayIntervals(date)
		if len(intervals) != 1 || intervals[0].Start.Hour() != 10 || intervals[0].End.Hour() != 15 {
			t.Fatalf("got intervals %v; want custom 10:15-15:45", intervals)
		}
	})

	t.Run("partial custom functions", func(t *testing.T) {
		calendar := NewBusinessCalendar()
		calendar.WorkdayStartFunc = func(date time.Time) time.Time {
			return time.Date(date.Year(), date.Month(), date.Day(), 10, 0, 0, 0, loc)
		}
		date := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
		intervals := calendar.WorkdayIntervals(date)
		if len(intervals) != 1 || intervals[0].Start.Hour() != 10 || intervals[0].End.Hour() != 17 {
			t.Fatalf("got intervals %v; want 10:00-17:00", intervals)
		}

		calendar.WorkdayStartFunc = nil
		calendar.WorkdayEndFunc = func(date time.Time) time.Time {
			return time.Date(date.Year(), date.Month(), date.Day(), 16, 0, 0, 0, loc)
		}
		intervals = calendar.WorkdayIntervals(date)
		if len(intervals) != 1 || intervals[0].Start.Hour() != 9 || intervals[0].End.Hour() != 16 {
			t.Fatalf("got intervals %v; want 09:00-16:00", intervals)
		}
	})

	t.Run("invalid default interval", func(t *testing.T) {
		calendar := NewBusinessCalendar()
		calendar.SetWorkHours(12*time.Hour, 11*time.Hour)
		date := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
		if intervals := calendar.WorkdayIntervals(date); intervals != nil {
			t.Fatalf("got intervals %v; want nil", intervals)
		}
	})

	t.Run("non-workday and empty custom intervals", func(t *testing.T) {
		calendar := NewBusinessCalendar()
		sunday := time.Date(2026, time.January, 4, 0, 0, 0, 0, loc)
		if intervals := calendar.WorkdayIntervals(sunday); intervals != nil {
			t.Fatalf("got intervals %v; want nil on Sunday", intervals)
		}

		monday := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
		calendar.WorkdayIntervalsFunc = func(date time.Time) []WorkTimeInterval {
			return nil
		}
		if intervals := calendar.WorkdayIntervals(monday); intervals != nil {
			t.Fatalf("got intervals %v; want nil for empty custom interval list", intervals)
		}
	})

	t.Run("unsorted and overlapping custom intervals", func(t *testing.T) {
		calendar := NewBusinessCalendar()
		calendar.WorkdayIntervalsFunc = func(date time.Time) []WorkTimeInterval {
			return []WorkTimeInterval{
				{Start: time.Date(2026, 1, 5, 9, 0, 0, 0, loc), End: time.Date(2026, 1, 5, 11, 0, 0, 0, loc)},
				{Start: time.Date(2026, 1, 5, 14, 0, 0, 0, loc), End: time.Date(2026, 1, 5, 15, 0, 0, 0, loc)},
				{Start: time.Date(2026, 1, 5, 9, 0, 0, 0, loc), End: time.Date(2026, 1, 5, 12, 0, 0, 0, loc)},
			}
		}
		date := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
		intervals := calendar.WorkdayIntervals(date)
		if len(intervals) != 3 || intervals[0].End.Hour() != 11 || intervals[1].End.Hour() != 12 || intervals[2].Start.Hour() != 14 {
			t.Fatalf("got intervals %v; want chronological intervals", intervals)
		}

		calendar.WorkdayIntervalsFunc = func(date time.Time) []WorkTimeInterval {
			return []WorkTimeInterval{
				{Start: time.Date(2026, 1, 5, 9, 0, 0, 0, loc), End: time.Date(2026, 1, 5, 12, 0, 0, 0, loc)},
				{Start: time.Date(2026, 1, 5, 11, 0, 0, 0, loc), End: time.Date(2026, 1, 5, 13, 0, 0, 0, loc)},
			}
		}
		_, err := EarliestJointAvailability(
			[]JointCalendar{{ID: "bad", Calendar: calendar, Location: loc}},
			utcTime(2026, 1, 5, 0, 0), utcTime(2026, 1, 6, 0, 0),
			time.Hour,
		)
		if !errors.Is(err, ErrInvalidWorkInterval) {
			t.Fatalf("got error %v; want ErrInvalidWorkInterval", err)
		}
	})
}

func TestAvailabilityExplicitLocationAndWorkdayFunction(t *testing.T) {
	configLocation := time.FixedZone("configured", 3*60*60)
	explicitLocation := time.FixedZone("explicit", 0)

	calendar := NewBusinessCalendar()
	calendar.Locations = []*time.Location{configLocation}
	calendar.WorkdayFunc = func(date time.Time) bool {
		return date.Weekday() == time.Sunday
	}
	calendar.AddHoliday(&Holiday{
		Type:  ObservancePublic,
		Month: time.January,
		Day:   4,
		Func:  CalcDayOfMonth,
	})

	window, err := EarliestJointAvailability(
		[]JointCalendar{{ID: "shifted", Calendar: calendar, Location: explicitLocation}},
		utcTime(2026, 1, 3, 0, 0), utcTime(2026, 1, 12, 0, 0),
		time.Hour,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !window.Start.Equal(utcTime(2026, 1, 11, 9, 0)) {
		t.Fatalf("got start %s; want next non-holiday Sunday after skipping configured location", window.Start)
	}
}
