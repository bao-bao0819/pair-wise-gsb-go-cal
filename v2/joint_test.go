// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"testing"
	"time"
)

func utc(y, m, d, h, min int) time.Time {
	return time.Date(y, time.Month(m), d, h, min, 0, 0, time.UTC)
}

func fixedOffset(name string, offsetHours int) *time.Location {
	return time.FixedZone(name, offsetHours*3600)
}

func jointCal(id string, loc *time.Location) JointCalendar {
	return JointCalendar{ID: id, Calendar: NewBusinessCalendar(), Location: loc}
}

// mustLoc loads an IANA location, skipping the test on environments without
// time zone data.
func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("time zone %q not available: %v", name, err)
	}
	return loc
}

func TestJointAvailabilityFixedZones(t *testing.T) {
	// Two teams in fixed zones UTC+3 (09:00-17:00 = 06:00-14:00 UTC) and UTC
	// (09:00-17:00) overlap on 09:00-14:00 UTC. A holiday for team "a" on
	// 12 Jan removes that day's window; the remaining overlap days are 8, 9,
	// 13, 14, 15, 16.
	plus3 := fixedOffset("UTC+3", 3)
	a := jointCal("a", plus3)
	a.Calendar.AddHoliday(&Holiday{
		Name: "test holiday", Type: ObservancePublic,
		Month: time.January, Day: 12, Func: CalcDayOfMonth,
	})
	b := jointCal("b", time.UTC)

	from := utc(2026, 1, 8, 0, 0)
	to := utc(2026, 1, 17, 0, 0)

	res, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
		From: from, To: to, Duration: 90 * time.Minute,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := utc(2026, 1, 8, 9, 0); !res.Earliest.Start.Equal(want) {
		t.Errorf("earliest start: want %v, got %v", want, res.Earliest.Start)
	}
	// The window ends after the requested duration, not at the overlap end.
	if want := utc(2026, 1, 8, 14, 0); !res.Earliest.End.Equal(want) {
		t.Errorf("earliest end: want %v, got %v", want, res.Earliest.End)
	}

	wantParts := []string{"a", "b"}
	if !sameStrings(res.Earliest.Participants, wantParts) {
		t.Errorf("participants: want %v, got %v", wantParts, res.Earliest.Participants)
	}

	if len(res.Earliest.Boundaries) != 2 {
		t.Fatalf("boundaries: want 2, got %d", len(res.Earliest.Boundaries))
	}
	bd := res.Earliest.Boundaries[0]
	if bd.ID != "a" {
		t.Errorf("first boundary id: want a, got %q", bd.ID)
	}
	if bd.Start.Location() != plus3 || bd.Start.Hour() != 12 {
		t.Errorf("a local start: want 12:00 UTC+3, got %v", bd.Start)
	}
	if bd.End.Hour() != 17 {
		t.Errorf("a local end: want 17:00 UTC+3, got %v", bd.End)
	}
	if got := res.Earliest.Boundaries[1]; got.ID != "b" || got.Start.Hour() != 9 {
		t.Errorf("b local boundary: want 09:00 UTC, got %+v", got)
	}

	if &res.Windows[0] != res.Earliest {
		t.Error("Earliest must point at Windows[0]")
	}

	if len(res.Windows) != 6 {
		t.Errorf("windows: want 6, got %d", len(res.Windows))
	}
	if want := utc(2026, 1, 13, 9, 0); !res.Windows[2].Start.Equal(want) {
		t.Errorf("first window after holiday: want %v, got %v", want, res.Windows[2].Start)
	}
}

func TestJointAvailabilityAdjacentDaysNoStitching(t *testing.T) {
	// A works 09:00-10:00 in UTC+9 (00:00-01:00 UTC); B works 09:00-10:00
	// UTC. The intervals never overlap (they only share local dates that
	// straddle the range), so nothing may be assembled from the two sides.
	plus9 := fixedOffset("UTC+9", 9)
	a := jointCal("a", plus9)
	a.Calendar.SetWorkHours(9*time.Hour, 10*time.Hour)
	b := jointCal("b", time.UTC)
	b.Calendar.SetWorkHours(9*time.Hour, 10*time.Hour)

	_, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
		From:     utc(2026, 1, 8, 0, 0),
		To:       utc(2026, 1, 17, 0, 0),
		Duration: 30 * time.Minute,
	})
	if err != ErrJointNoWindow {
		t.Fatalf("want ErrJointNoWindow, got %v", err)
	}
}

func TestJointAvailabilityWeekendRules(t *testing.T) {
	// Same zone and hours, but team "a" works Sunday-Thursday while team "b"
	// keeps Monday-Friday. They only overlap Monday-Thursday.
	ny := mustLoc(t, "America/New_York")
	a := jointCal("a", ny)
	a.Calendar.SetWorkday(time.Friday, false)
	a.Calendar.SetWorkday(time.Saturday, false)
	a.Calendar.SetWorkday(time.Sunday, true)
	b := jointCal("b", ny)

	res, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
		From:     utc(2026, 1, 11, 0, 0), // Sunday
		To:       utc(2026, 1, 18, 0, 0), // next Sunday
		Duration: 3 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Monday 2026-01-12 14:00 UTC = 09:00 EST is the first common instant.
	if want := utc(2026, 1, 12, 14, 0); !res.Earliest.Start.Equal(want) {
		t.Errorf("earliest start: want %v, got %v", want, res.Earliest.Start)
	}
	if got := res.Earliest.Start.In(ny); got.Hour() != 9 || got.Weekday() != time.Monday {
		t.Errorf("local start: want Mon 09:00 NY, got %v", got)
	}
	// Mon-Thu are common; Fri is only b and Sunday only a.
	if len(res.Windows) != 4 {
		t.Errorf("windows: want 4, got %d", len(res.Windows))
	}
}

// threeTeamSetup returns three UTC calendars with staggered hours on Monday
// 2026-01-12:
//
//	a: 09:00-17:00 (default)
//	b: 11:00-13:00
//	c: 13:00-15:00
func threeTeamSetup() []JointCalendar {
	a := jointCal("a", time.UTC)
	b := jointCal("b", time.UTC)
	b.Calendar.SetWorkHours(11*time.Hour, 13*time.Hour)
	c := jointCal("c", time.UTC)
	c.Calendar.SetWorkHours(13*time.Hour, 15*time.Hour)
	return []JointCalendar{a, b, c}
}

func TestJointAvailabilityKSegmentation(t *testing.T) {
	cals := threeTeamSetup()
	from := utc(2026, 1, 12, 0, 0)
	to := utc(2026, 1, 13, 0, 0)

	t.Run("all", func(t *testing.T) {
		_, err := JointAvailability(cals, JointOptions{
			From: from, To: to, Duration: time.Hour,
		})
		// All three share only the single instant 13:00: zero length.
		if err != ErrJointNoWindow {
			t.Fatalf("want ErrJointNoWindow, got %v", err)
		}
	})

	t.Run("explicit k equals all", func(t *testing.T) {
		_, err := JointAvailability(cals, JointOptions{
			From: from, To: to, Duration: time.Hour, MinParticipants: 3,
		})
		if err != ErrJointNoWindow {
			t.Fatalf("want ErrJointNoWindow, got %v", err)
		}
	})

	t.Run("at least two", func(t *testing.T) {
		res, err := JointAvailability(cals, JointOptions{
			From: from, To: to, Duration: time.Hour, MinParticipants: 2,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Monday segments with >=2 participants:
		//   11:00-13:00 [a b], 13:00-15:00 [a c]
		want := []struct {
			start, end int
			parts      []string
		}{
			{11, 13, []string{"a", "b"}},
			{13, 15, []string{"a", "c"}},
		}
		if len(res.Windows) != len(want) {
			t.Fatalf("windows: want %d, got %d: %+v", len(want), len(res.Windows), res.Windows)
		}
		for i, w := range want {
			got := res.Windows[i]
			if !got.Start.Equal(utc(2026, 1, 12, w.start, 0)) ||
				!got.End.Equal(utc(2026, 1, 12, w.end, 0)) {
				t.Errorf("window %d bounds: want %d:00-%d:00, got %v-%v",
					i, w.start, w.end, got.Start, got.End)
			}
			if !sameStrings(got.Participants, w.parts) {
				t.Errorf("window %d participants: want %v, got %v",
					i, w.parts, got.Participants)
			}
		}
		if !res.Earliest.Start.Equal(utc(2026, 1, 12, 11, 0)) {
			t.Errorf("earliest: want 11:00, got %v", res.Earliest.Start)
		}
	})

	t.Run("at least one", func(t *testing.T) {
		res, err := JointAvailability(cals, JointOptions{
			From: from, To: to, Duration: time.Hour, MinParticipants: 1,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// [a] 09:00-11:00, [a b] 11:00-13:00, [a c] 13:00-15:00,
		// [a] 15:00-17:00
		if len(res.Windows) != 4 {
			t.Fatalf("windows: want 4, got %d: %+v", len(res.Windows), res.Windows)
		}
		if !sameStrings(res.Windows[0].Participants, []string{"a"}) ||
			!sameStrings(res.Windows[3].Participants, []string{"a"}) {
			t.Errorf("outer segments should be [a]: %+v", res.Windows)
		}
	})
}

func TestJointAvailabilityOrderIndependent(t *testing.T) {
	from := utc(2026, 1, 12, 0, 0)
	to := utc(2026, 1, 17, 0, 0)

	run := func(cals []JointCalendar) *JointResult {
		res, err := JointAvailability(cals, JointOptions{
			From: from, To: to, Duration: 90 * time.Minute, MinParticipants: 2,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return res
	}

	base := threeTeamSetup()
	perm := []JointCalendar{base[2], base[0], base[1]} // c, a, b

	r1 := run(base)
	r2 := run(perm)

	if len(r1.Windows) != len(r2.Windows) {
		t.Fatalf("window counts differ: %d vs %d", len(r1.Windows), len(r2.Windows))
	}
	for i := range r1.Windows {
		w1, w2 := r1.Windows[i], r2.Windows[i]
		if !w1.Start.Equal(w2.Start) || !w1.End.Equal(w2.End) {
			t.Errorf("window %d bounds differ: %v-%v vs %v-%v",
				i, w1.Start, w1.End, w2.Start, w2.End)
		}
		if !sameStrings(w1.Participants, w2.Participants) {
			t.Errorf("window %d participants differ: %v vs %v",
				i, w1.Participants, w2.Participants)
		}
	}
	if !r1.Earliest.Start.Equal(r2.Earliest.Start) {
		t.Errorf("earliest instant differs: %v vs %v", r1.Earliest.Start, r2.Earliest.Start)
	}
}

func TestJointAvailabilityErrors(t *testing.T) {
	good := jointCal("a", time.UTC)
	from := utc(2026, 1, 12, 0, 0)
	to := utc(2026, 1, 13, 0, 0)
	opts := func(o JointOptions) JointOptions {
		if o.From.IsZero() {
			o.From = from
		}
		if o.To.IsZero() {
			o.To = to
		}
		if o.Duration == 0 {
			o.Duration = time.Hour
		}
		return o
	}

	if _, err := JointAvailability(nil, opts(JointOptions{})); err != ErrJointNoCalendars {
		t.Errorf("empty: want ErrJointNoCalendars, got %v", err)
	}

	dup := []JointCalendar{jointCal("a", time.UTC), jointCal("a", time.UTC)}
	if _, err := JointAvailability(dup, opts(JointOptions{})); err != ErrJointDuplicateID {
		t.Errorf("duplicate id: want ErrJointDuplicateID, got %v", err)
	}

	bad := []JointCalendar{{ID: "a", Calendar: nil, Location: time.UTC}}
	if _, err := JointAvailability(bad, opts(JointOptions{})); err != ErrJointInvalidRange {
		t.Errorf("nil calendar: want ErrJointInvalidRange, got %v", err)
	}
	bad = []JointCalendar{{ID: "a", Calendar: NewBusinessCalendar(), Location: nil}}
	if _, err := JointAvailability(bad, opts(JointOptions{})); err != ErrJointInvalidRange {
		t.Errorf("nil location: want ErrJointInvalidRange, got %v", err)
	}
	bad = []JointCalendar{{ID: "", Calendar: NewBusinessCalendar(), Location: time.UTC}}
	if _, err := JointAvailability(bad, opts(JointOptions{})); err != ErrJointInvalidRange {
		t.Errorf("empty id: want ErrJointInvalidRange, got %v", err)
	}

	if _, err := JointAvailability([]JointCalendar{good}, opts(JointOptions{To: from})); err != ErrJointInvalidRange {
		t.Errorf("empty range: want ErrJointInvalidRange, got %v", err)
	}
	if _, err := JointAvailability([]JointCalendar{good}, opts(JointOptions{Duration: -time.Hour})); err != ErrJointInvalidRange {
		t.Errorf("negative duration: want ErrJointInvalidRange, got %v", err)
	}

	if _, err := JointAvailability([]JointCalendar{good}, opts(JointOptions{MinParticipants: -1})); err != ErrJointInvalidK {
		t.Errorf("negative k: want ErrJointInvalidK, got %v", err)
	}
	if _, err := JointAvailability([]JointCalendar{good}, opts(JointOptions{MinParticipants: 2})); err != ErrJointInvalidK {
		t.Errorf("k too large: want ErrJointInvalidK, got %v", err)
	}

	// Two calendars with no overlap in the range: separate from input errors.
	plus9 := jointCal("p9", fixedOffset("UTC+9", 9))
	plus9.Calendar.SetWorkHours(9*time.Hour, 10*time.Hour)
	minus9 := jointCal("m9", fixedOffset("UTC-9", -9))
	minus9.Calendar.SetWorkHours(9*time.Hour, 10*time.Hour)
	if _, err := JointAvailability([]JointCalendar{plus9, minus9}, opts(JointOptions{})); err != ErrJointNoWindow {
		t.Errorf("no window: want ErrJointNoWindow, got %v", err)
	}
}

func TestJointAvailabilityCrossTimezoneHoliday(t *testing.T) {
	// Monday 2026-01-12. Team UTC is off for its local holiday; team UTC+5
	// is at work. The earliest common window must move to Tuesday, which
	// proves the holiday is interpreted on the local calendar, not globally.
	plus5 := jointCal("a", fixedOffset("UTC+5", 5))
	utcCal := jointCal("b", time.UTC)
	utcCal.Calendar.AddHoliday(&Holiday{
		Name: "utc holiday", Type: ObservancePublic,
		Month: time.January, Day: 12, Func: CalcDayOfMonth,
	})

	res, err := JointAvailability([]JointCalendar{plus5, utcCal}, JointOptions{
		From:     utc(2026, 1, 11, 0, 0),
		To:       utc(2026, 1, 14, 0, 0),
		Duration: 2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Monday overlap would be 09:00-12:00 UTC; Tuesday starts 09:00 UTC.
	if want := utc(2026, 1, 13, 9, 0); !res.Earliest.Start.Equal(want) {
		t.Errorf("earliest: want %v, got %v", want, res.Earliest.Start)
	}
}

func TestJointAvailabilityDST(t *testing.T) {
	t.Run("US spring forward", func(t *testing.T) {
		// US springs forward on 2026-03-08. London stays GMT until March 29.
		ny := mustLoc(t, "America/New_York")
		ln := mustLoc(t, "Europe/London")

		preFri := time.Date(2026, 3, 6, 9, 0, 0, 0, ny)
		postMon := time.Date(2026, 3, 9, 9, 0, 0, 0, ny)
		if preFri.UTC().Hour() != 14 {
			t.Fatalf("pre-DST NY 09:00 should be 14:00 UTC, got %v", preFri.UTC())
		}
		if postMon.UTC().Hour() != 13 {
			t.Fatalf("post-DST NY 09:00 should be 13:00 UTC, got %v", postMon.UTC())
		}

		a := jointCal("ny", ny)
		b := jointCal("ln", ln)
		res, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
			From:     utc(2026, 3, 6, 0, 0),
			To:       utc(2026, 3, 13, 0, 0),
			Duration: 3 * time.Hour,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Friday overlap 14:00-17:00 UTC (3h); after the switch the overlap
		// is 13:00-17:00 UTC (4h). Earliest is Friday 14:00 UTC.
		if want := utc(2026, 3, 6, 14, 0); !res.Earliest.Start.Equal(want) {
			t.Errorf("earliest: want %v, got %v", want, res.Earliest.Start)
		}
		// Fri and Mon-Tue-Thu: 5 windows; Monday's window begins 13:00 UTC.
		if len(res.Windows) != 5 {
			t.Errorf("windows: want 5, got %d", len(res.Windows))
		}
		if want := utc(2026, 3, 9, 13, 0); !res.Windows[1].Start.Equal(want) {
			t.Errorf("post-DST window: want %v, got %v", want, res.Windows[1].Start)
		}
	})

	t.Run("US fall back", func(t *testing.T) {
		// US falls back on 2026-11-01 (Sunday). London stays GMT.
		ny := mustLoc(t, "America/New_York")
		ln := mustLoc(t, "Europe/London")

		preFri := time.Date(2026, 10, 30, 9, 0, 0, 0, ny)
		postMon := time.Date(2026, 11, 2, 9, 0, 0, 0, ny)
		if preFri.UTC().Hour() != 13 {
			t.Fatalf("pre-fall NY 09:00 should be 13:00 UTC, got %v", preFri.UTC())
		}
		if postMon.UTC().Hour() != 14 {
			t.Fatalf("post-fall NY 09:00 should be 14:00 UTC, got %v", postMon.UTC())
		}

		a := jointCal("ny", ny)
		b := jointCal("ln", ln)
		res, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
			From:     utc(2026, 10, 30, 0, 0),
			To:       utc(2026, 11, 6, 0, 0),
			Duration: 4 * time.Hour,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Before the fall: overlap 13:00-17:00 UTC (4h).
		if want := utc(2026, 10, 30, 13, 0); !res.Earliest.Start.Equal(want) {
			t.Errorf("earliest: want %v, got %v", want, res.Earliest.Start)
		}
		// After the fall the overlap is 14:00-17:00 UTC (3h), so a 4h
		// window only exists Fri Oct 30.
		if len(res.Windows) != 1 {
			t.Errorf("windows: want 1, got %d (%+v)", len(res.Windows), res.Windows)
		}
	})

	t.Run("EU spring forward", func(t *testing.T) {
		// EU springs forward on 2026-03-29 while NY is already on EDT.
		// Before the switch Berlin is CET (UTC+1): overlap 13:00-16:00 UTC
		// (3h). Afterwards Berlin is CEST (UTC+2): the overlap shrinks to
		// 13:00-15:00 UTC (2h).
		ny := mustLoc(t, "America/New_York")
		be := mustLoc(t, "Europe/Berlin")

		a := jointCal("ny", ny)
		b := jointCal("be", be)
		res, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
			From:     utc(2026, 3, 27, 0, 0),
			To:       utc(2026, 4, 1, 0, 0),
			Duration: 2 * time.Hour,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Both days qualify, but Friday's overlap is the earliest.
		if want := utc(2026, 3, 27, 13, 0); !res.Earliest.Start.Equal(want) {
			t.Errorf("earliest: want %v, got %v", want, res.Earliest.Start)
		}
		if len(res.Windows) != 3 {
			t.Errorf("windows: want 3, got %d (%+v)", len(res.Windows), res.Windows)
		}
		if want := utc(2026, 3, 30, 13, 0); !res.Windows[1].Start.Equal(want) {
			t.Errorf("post-switch window: want %v, got %v", want, res.Windows[1].Start)
		}
	})
}

func TestJointAvailabilityRangeClipping(t *testing.T) {
	a := jointCal("a", time.UTC)
	b := jointCal("b", fixedOffset("UTC+3", 3))

	t.Run("start clips window", func(t *testing.T) {
		res, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
			// Overlap is 12:00-14:00 UTC; start the range at 12:30 UTC.
			From:     utc(2026, 1, 8, 12, 30),
			To:       utc(2026, 1, 9, 0, 0),
			Duration: time.Hour,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := utc(2026, 1, 8, 12, 30); !res.Earliest.Start.Equal(want) {
			t.Errorf("start: want %v, got %v", want, res.Earliest.Start)
		}
		if want := utc(2026, 1, 8, 14, 0); !res.Earliest.End.Equal(want) {
			t.Errorf("end: want %v, got %v", want, res.Earliest.End)
		}
	})

	t.Run("end clips window to exactly duration", func(t *testing.T) {
		_, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
			// Only 90 minutes remain inside the range.
			From:     utc(2026, 1, 8, 12, 30),
			To:       utc(2026, 1, 8, 14, 0),
			Duration: 90 * time.Minute,
		})
		if err != nil {
			t.Fatalf("exact-length window should qualify: %v", err)
		}
	})

	t.Run("end leaves insufficient overlap", func(t *testing.T) {
		_, err := JointAvailability([]JointCalendar{a, b}, JointOptions{
			From:     utc(2026, 1, 8, 12, 30),
			To:       utc(2026, 1, 8, 13, 0),
			Duration: 90 * time.Minute,
		})
		if err != ErrJointNoWindow {
			t.Fatalf("want ErrJointNoWindow, got %v", err)
		}
	})
}

func TestJointAvailabilityDoesNotTouchDefaultLoc(t *testing.T) {
	orig := DefaultLoc
	marker := fixedOffset("marker", 0)
	DefaultLoc = marker
	defer func() { DefaultLoc = orig }()

	a := jointCal("a", fixedOffset("UTC+5", 5))
	b := jointCal("b", fixedOffset("UTC-3", -3))
	_, _ = JointAvailability([]JointCalendar{a, b}, JointOptions{
		From:     utc(2026, 1, 8, 0, 0),
		To:       utc(2026, 1, 10, 0, 0),
		Duration: time.Hour,
	})
	if DefaultLoc != marker {
		t.Fatalf("DefaultLoc was modified")
	}
}
