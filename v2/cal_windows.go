// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"sort"
	"time"
)

// AmbiguousTimePolicy selects how local wall-clock boundaries that fall on a
// Daylight Saving Time transition are converted to instants.
//
// A spring-forward transition makes one range of local times non-existent
// (a gap); a fall-back transition makes one range of local times repeat (a
// fold). The same policy is offered for both situations so callers can choose
// the semantics that match their scheduling system.
type AmbiguousTimePolicy uint8

const (
	// AmbiguousError refuses to convert a boundary that lands in a DST gap
	// or fold. This is the default.
	AmbiguousError AmbiguousTimePolicy = iota

	// AmbiguousEarlier resolves gap boundaries to the last instant before the
	// transition (standard time for a gap) and fold boundaries to the first
	// occurrence (daylight time for a fold).
	AmbiguousEarlier

	// AmbiguousLater resolves gap boundaries to the first instant after the
	// transition (daylight time for a gap) and fold boundaries to the second
	// occurrence (standard time for a fold).
	AmbiguousLater

	// AmbiguousSkip drops the entire window whenever either of its boundaries
	// lands in a DST gap or fold.
	AmbiguousSkip
)

// ErrAmbiguousTime is returned when a local time boundary falls in a DST gap
// or fold and the calendar's AmbiguousTimePolicy is AmbiguousError.
var ErrAmbiguousTime = errors.New("cal: local time is ambiguous due to a daylight saving transition")

// ErrZeroWindow is returned when a work window has no positive length, either
// because its start and end local times are identical or because DST
// resolution collapses it.
var ErrZeroWindow = errors.New("cal: work window must have a positive length")

// ErrNoLocation is returned by the window-based APIs when the calendar has not
// been bound to a time.Location.
var ErrNoLocation = errors.New("cal: calendar has no location; use NewBusinessCalendarIn or SetLocation")

// Clock is a local wall-clock time of day measured from midnight, as a
// time.Duration. Valid values range from 0 (00:00) up to but not including
// 24h.
type Clock = time.Duration

// LocalWindow is a single work interval expressed in local wall-clock time,
// attributed to the day on which the shift starts.
//
// Start is the time of day the window begins. End is the time of day it ends
// on EndDayOffset days after the start date: 0 means the same calendar day,
// 1 means the following calendar day, which allows night shifts to cross
// midnight. Windows that cross midnight are never truncated merely because
// the following date is a holiday or otherwise a non-workday; attribution is
// decided solely by the start date.
type LocalWindow struct {
	Start        Clock // local time of day the window starts, in [0,24h)
	End          Clock // local time of day the window ends
	EndDayOffset int   // 0 ends the same day; 1 ends the following day
}

// WorkInterval is a resolved, half-open [Start, End) interval of actual
// instants. Every returned interval has a positive duration.
type WorkInterval struct {
	Start time.Time // inclusive start instant
	End   time.Time // exclusive end instant
}

// workWindowDate identifies a local calendar date without carrying a location.
type workWindowDate struct {
	year  int
	month time.Month
	day   int
}

// SetLocation binds the calendar to the given time.Location. The multi-window
// rules are expressed and interpreted in this location, and intervals returned
// by WorkIntervals are located here. It does not change the configured rules.
func (c *BusinessCalendar) SetLocation(loc *time.Location) {
	c.loc = loc
}

// Location returns the location the calendar is bound to, or nil if none has
// been set explicitly.
func (c *BusinessCalendar) Location() *time.Location {
	return c.loc
}

// SetDSTPolicy selects how local boundaries that fall on a DST transition are
// converted to instants. The default is AmbiguousError.
func (c *BusinessCalendar) SetDSTPolicy(p AmbiguousTimePolicy) {
	c.dstPolicy = p
}

// DSTPolicy returns the calendar's DST ambiguity policy.
func (c *BusinessCalendar) DSTPolicy() AmbiguousTimePolicy {
	return c.dstPolicy
}

// SetWeeklyWindows replaces the recurring work windows for the given weekday.
// Windows are attributed to dates with that weekday and may cross midnight via
// EndDayOffset. Providing no windows removes the rule for that weekday.
//
// Zero-length windows are rejected.
func (c *BusinessCalendar) SetWeeklyWindows(day time.Weekday, windows ...LocalWindow) error {
	for _, w := range windows {
		if err := validateWindow(w); err != nil {
			return err
		}
	}
	c.weeklyWindows[day] = append([]LocalWindow(nil), windows...)
	c.windows = true
	return nil
}

// AddWeeklyWindows appends recurring work windows for the given weekday.
// Overlapping windows are merged when intervals are queried.
//
// Zero-length windows are rejected.
func (c *BusinessCalendar) AddWeeklyWindows(day time.Weekday, windows ...LocalWindow) error {
	for _, w := range windows {
		if err := validateWindow(w); err != nil {
			return err
		}
	}
	c.weeklyWindows[day] = append(c.weeklyWindows[day], windows...)
	c.windows = true
	return nil
}

// SetDateWindows replaces the one-off work windows attributed to the given
// date. A date exception completely replaces the weekly rule for shifts that
// start on that date: the date need not be a configured workday and is not
// suppressed by a holiday. Providing no windows removes the exception.
//
// Windows may cross midnight via EndDayOffset. Zero-length windows are
// rejected.
func (c *BusinessCalendar) SetDateWindows(date time.Time, windows ...LocalWindow) error {
	for _, w := range windows {
		if err := validateWindow(w); err != nil {
			return err
		}
	}
	if c.dateWindows == nil {
		c.dateWindows = make(map[workWindowDate][]LocalWindow)
	}
	key := workWindowDateOf(date)
	if len(windows) == 0 {
		delete(c.dateWindows, key)
	} else {
		c.dateWindows[key] = append([]LocalWindow(nil), windows...)
	}
	c.windows = true
	return nil
}

// AddDateWindows appends one-off work windows attributed to the given date.
//
// Zero-length windows are rejected.
func (c *BusinessCalendar) AddDateWindows(date time.Time, windows ...LocalWindow) error {
	for _, w := range windows {
		if err := validateWindow(w); err != nil {
			return err
		}
	}
	if c.dateWindows == nil {
		c.dateWindows = make(map[workWindowDate][]LocalWindow)
	}
	key := workWindowDateOf(date)
	c.dateWindows[key] = append(c.dateWindows[key], windows...)
	c.windows = true
	return nil
}

// WindowsFor reports the configured local windows attributed to the given
// date and whether a date exception (rather than the weekly rule) supplied
// them. A weekly rule on a non-workday still reports its windows here; the
// workday/holiday gate is applied when intervals are queried.
func (c *BusinessCalendar) WindowsFor(date time.Time) (windows []LocalWindow, exception bool) {
	if w, ok := c.dateWindows[workWindowDateOf(date)]; ok {
		return w, true
	}
	return c.weeklyWindows[date.Weekday()], false
}

func validateWindow(w LocalWindow) error {
	if w.Start < 0 || w.Start >= 24*time.Hour {
		return errors.New("cal: window start must be within [00:00,24:00)")
	}
	if w.EndDayOffset != 0 && w.EndDayOffset != 1 {
		return errors.New("cal: window end day offset must be 0 or 1")
	}
	if w.EndDayOffset == 0 {
		if w.End < 0 || w.End > 24*time.Hour {
			return errors.New("cal: same-day window end must be within [00:00,24:00]")
		}
		if w.End <= w.Start {
			return ErrZeroWindow
		}
	} else {
		if w.End < 0 || w.End >= 24*time.Hour {
			return errors.New("cal: overnight window end must be within [00:00,24:00)")
		}
		if w.End == 0 {
			return ErrZeroWindow
		}
	}
	return nil
}

func workWindowDateOf(t time.Time) workWindowDate {
	y, m, d := t.Date()
	return workWindowDate{year: y, month: m, day: d}
}

// WorkIntervals returns the merged, half-open work intervals that overlap the
// half-open range [start, end). Intervals are returned in chronological order
// in the calendar's location, and overlapping or touching intervals are
// merged.
//
// Windows are attributed to their shift start date: a night shift that crosses
// midnight survives even when the following date is a holiday. Date exceptions
// replace the weekly rule for their start date.
//
// The calendar must have been bound to a location. Boundaries landing in a DST
// gap or fold are handled according to the calendar's AmbiguousTimePolicy.
func (c *BusinessCalendar) WorkIntervals(start, end time.Time) ([]WorkInterval, error) {
	if c.loc == nil {
		return nil, ErrNoLocation
	}
	if !start.Before(end) {
		return nil, nil
	}
	start = start.In(c.loc)
	end = end.In(c.loc)

	var intervals []WorkInterval

	// Begin one day early so overnight windows started the previous local day
	// can reach into the range.
	day := localMidnight(start).AddDate(0, 0, -1)
	for !day.After(localMidnight(end)) {
		dayIntervals, err := c.intervalsForDay(day, start, end)
		if err != nil {
			return nil, err
		}
		intervals = append(intervals, dayIntervals...)
		day = day.AddDate(0, 0, 1)
	}

	return mergeIntervals(intervals), nil
}

// intervalsForDay resolves every window attributed to the given local date that
// overlaps the requested range. Date exceptions bypass the workday gate.
func (c *BusinessCalendar) intervalsForDay(day, rangeStart, rangeEnd time.Time) ([]WorkInterval, error) {
	windows, exception := c.WindowsFor(day)
	if !exception {
		if !c.IsWorkday(day) {
			return nil, nil
		}
		if len(windows) == 0 {
			if c.windows {
				return nil, nil
			}
			w, ok := c.legacyWindow()
			if !ok {
				return nil, nil
			}
			windows = []LocalWindow{w}
		}
	}

	var result []WorkInterval
	for _, w := range windows {
		iv, skipped, err := c.resolveWindow(day, w)
		if err != nil {
			return nil, err
		}
		if skipped {
			continue
		}
		if iv.End.Before(rangeStart) || iv.Start.After(rangeEnd) ||
			iv.Start.Equal(rangeEnd) || iv.End.Equal(rangeStart) {
			continue
		}
		if iv.Start.Before(rangeStart) {
			iv.Start = rangeStart
		}
		if iv.End.After(rangeEnd) {
			iv.End = rangeEnd
		}
		result = append(result, iv)
	}
	return result, nil
}

// legacyWindow converts the single legacy start/end work hours into a local
// window. A 24h end means the following midnight.
func (c *BusinessCalendar) legacyWindow() (LocalWindow, bool) {
	if c.WorkdayStartFunc != nil || c.WorkdayEndFunc != nil {
		// Per-day functions are not expressible as fixed local windows.
		return LocalWindow{}, false
	}

	start := normalizeClock(c.workdayStart)
	end := normalizeClock(c.workdayEnd)
	delta := 0
	if c.workdayEnd == 24*time.Hour {
		end = 0
		delta = 1
	}
	w := LocalWindow{Start: start, End: end, EndDayOffset: delta}
	if err := validateWindow(w); err != nil {
		return LocalWindow{}, false
	}
	return w, true
}

// normalizeClock reduces a duration to a time-of-day in [0,24h).
func normalizeClock(d time.Duration) time.Duration {
	day := 24 * time.Hour
	d = d % day
	if d < 0 {
		d += day
	}
	return d
}

func localMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// mergeIntervals sorts the intervals and merges overlapping or touching ones.
// Intervals that became empty after range clipping are dropped.
func mergeIntervals(in []WorkInterval) []WorkInterval {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].Start.Equal(in[j].Start) {
			return in[i].End.Before(in[j].End)
		}
		return in[i].Start.Before(in[j].Start)
	})

	out := make([]WorkInterval, 0, len(in))
	for _, iv := range in {
		if !iv.Start.Before(iv.End) {
			continue
		}
		if len(out) == 0 || iv.Start.After(out[len(out)-1].End) {
			out = append(out, iv)
			continue
		}
		if iv.End.After(out[len(out)-1].End) {
			out[len(out)-1].End = iv.End
		}
	}
	return out
}
