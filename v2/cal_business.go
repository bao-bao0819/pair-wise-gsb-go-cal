// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"time"
)

// ErrEmptyWorkdayCycle is returned when a workday cycle is configured with an
// empty pattern.
var ErrEmptyWorkdayCycle = errors.New("workday cycle pattern must not be empty")

// WorkdayFn reports whether the given date is a workday.
// This is useful for situations where work days change throughout the year.
//
// If your workdays are fixed (Mon-Fri for example) then a WorkdayFn
// is not necessary and you can use SetWorkday() instead.
type WorkdayFn func(date time.Time) bool

// WorkdayStartFn reports the start of the workday for the given date.
// This is useful for situations where work days have different start times
// throughout the year.
//
// If your workdays start at the same time every day then a WorkdayStartFn
// is not necessary and you can use SetWorkingHours() instead.
type WorkdayStartFn func(date time.Time) time.Time

// WorkdayEndFn reports the end of the workday for the given date.
// This is useful for situations where work days have different end times
// throughout the year.
//
// If your workdays end at the same time every day then a WorkdayEndFn
// is not necessary and you can use SetWorkingHours() instead.
type WorkdayEndFn func(date time.Time) time.Time

// BusinessCalendar represents a calendar used for business purposes.
type BusinessCalendar struct {
	workday     [7]bool   // flags to indicate a day of the week is a workday
	WorkdayFunc WorkdayFn // optional function to override workday flags

	// cycleAnchor is the civil date that corresponds to cycle index 0; its
	// location is the binding timezone used to convert instants and civil date
	// keys. Stored at noon to make date arithmetic immune to DST shifts.
	cycleAnchor  time.Time
	cyclePattern []bool // repeating N-day workday pattern; copied on set

	// holidaysSuspendWork reports whether a holiday forces the day off even
	// when the rotation would otherwise work it. NewBusinessCalendar sets it
	// true to preserve the default "holidays are not workdays" behavior; an
	// explicit SetHolidaysSuspendWork(false) lets the rotation run through
	// holidays.
	holidaysSuspendWork bool

	// workdayExceptions are ad-hoc shift swaps keyed by civil date in the
	// binding timezone. They override both the cycle and holidays, but never
	// shift the phase of later dates.
	workdayExceptions map[holCacheKey]bool

	workdayStart     time.Duration  // the time of day at which the workdays starts
	WorkdayStartFunc WorkdayStartFn // optional function to override workday start time
	workdayEnd       time.Duration  // the time of day at which workdays end
	WorkdayEndFunc   WorkdayEndFn   // optional function to override workday end time

	Calendar
}

// NewBusinessCalendar creates a new BusinessCalendar with no holidays defined
// and work days of Monday through Friday from 9am-5pm.
func NewBusinessCalendar() *BusinessCalendar {
	c := &BusinessCalendar{}
	c.workday[time.Monday] = true
	c.workday[time.Tuesday] = true
	c.workday[time.Wednesday] = true
	c.workday[time.Thursday] = true
	c.workday[time.Friday] = true
	c.workdayStart = 9 * time.Hour
	c.workdayEnd = 17 * time.Hour
	c.holidaysSuspendWork = true
	return c
}

// SetWorkday changes the given day's status as a standard working day
func (c *BusinessCalendar) SetWorkday(day time.Weekday, workday bool) {
	c.workday[day] = workday
}

// SetWorkdayCycle configures workdays as an N-day boolean sequence anchored to
// a civil date. anchor corresponds to pattern index 0; subsequent dates use
// indices 1, 2, ... and earlier dates use the mathematically defined
// non-negative remainder of (date-anchor) mod len(pattern), so a pattern such
// as two days on, two days off keeps its phase across weekends, month
// boundaries, leap days, and DST transitions.
//
// Date differences are counted by calendar date, never by dividing a 24-hour
// duration. The anchor's timezone becomes the binding timezone: callers may
// pass an instant in any timezone and it is first converted to that timezone
// before its civil date is resolved.
//
// Once set, the cycle replaces the per-weekday flags and WorkdayFn for
// determining the underlying working pattern. The pattern slice is copied, so
// later mutation of the caller's slice does not affect the calendar.
//
// ErrEmptyWorkdayCycle is returned if pattern is empty.
func (c *BusinessCalendar) SetWorkdayCycle(anchor time.Time, pattern []bool) error {
	if len(pattern) == 0 {
		return ErrEmptyWorkdayCycle
	}

	c.cycleAnchor = civilNoon(anchor)
	c.cyclePattern = make([]bool, len(pattern))
	copy(c.cyclePattern, pattern)
	return nil
}

// SetHolidaysSuspendWork configures whether configured holidays override the
// working pattern and force the day off (suspend true), or whether the
// rotation runs as usual through holidays (suspend false).
//
// When no workday cycle is configured the default matches the historical
// behavior: holidays suspend work.
func (c *BusinessCalendar) SetHolidaysSuspendWork(suspend bool) {
	c.holidaysSuspendWork = suspend
}

// SetWorkdayException records an ad-hoc schedule adjustment for a single civil
// date: pass true to make the date a workday (for example a weekend worked to
// compensate for a holiday), or false to grant the date off. The override is
// applied outermost in IsWorkday and only affects that date; the phase of
// later dates is determined by the original cycle and is never shifted.
//
// The date's civil year/month/day is taken in the binding timezone: the
// workday cycle anchor's timezone when a cycle is set, otherwise UTC. Its
// time-of-day is ignored.
func (c *BusinessCalendar) SetWorkdayException(date time.Time, workday bool) {
	if c.workdayExceptions == nil {
		c.workdayExceptions = make(map[holCacheKey]bool)
	}
	c.workdayExceptions[civilKey(date, c.bindingLoc())] = workday
}

// RemoveWorkdayException removes a previously recorded ad-hoc schedule
// adjustment for the given civil date, if any.
func (c *BusinessCalendar) RemoveWorkdayException(date time.Time) {
	if c.workdayExceptions != nil {
		delete(c.workdayExceptions, c.civilKeyInBinding(date))
	}
}

// ClearWorkdayExceptions removes all ad-hoc schedule adjustments.
func (c *BusinessCalendar) ClearWorkdayExceptions() {
	c.workdayExceptions = nil
}

// bindingLoc reports the timezone used to resolve caller instants and civil
// date keys. A configured cycle anchors the timezone to the anchor's
// location; otherwise UTC is used.
func (c *BusinessCalendar) bindingLoc() *time.Location {
	if c.cyclePattern != nil {
		if loc := c.cycleAnchor.Location(); loc != nil {
			return loc
		}
	}
	return time.UTC
}

// civilKeyInBinding converts an instant to the binding timezone and returns
// its civil date key.
func (c *BusinessCalendar) civilKeyInBinding(date time.Time) holCacheKey {
	return civilKey(date, c.bindingLoc())
}

// civilKey returns the civil date key of an instant interpreted in loc.
func civilKey(date time.Time, loc *time.Location) holCacheKey {
	local := date.In(loc)
	year, month, day := local.Date()
	return holCacheKey{year: year, month: month, day: day}
}

// civilNoon returns noon of t's civil date in t's location. Noon is used so
// that whole calendar-day differences never straddle a date around DST.
func civilNoon(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 12, 0, 0, 0, t.Location())
}

// civilDays reports the signed number of calendar dates from a to b by
// comparing noon instants in their own timezones. The result is a pure date
// difference (immune to DST and never computed as a duration divided by 24h).
func civilDays(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	an := time.Date(ay, am, ad, 12, 0, 0, 0, time.UTC)
	bn := time.Date(by, bm, bd, 12, 0, 0, 0, time.UTC)
	return int(bn.Sub(an) / (24 * time.Hour))
}

// SetWorkHours sets the start and end times for a workday.
//
// Only hours and minutes will be considered. The time component of start
// should be less than the time component of end.
func (c *BusinessCalendar) SetWorkHours(start time.Duration, end time.Duration) {
	c.workdayStart = start
	c.workdayEnd = end
}

// IsWorkday reports whether a given date is a work day (business day).
func (c *BusinessCalendar) IsWorkday(date time.Time) bool {
	// Ad-hoc shift swaps apply outermost: they override both the rotation and
	// holidays for that date alone, without moving the cycle phase.
	if c.workdayExceptions != nil {
		if workday, ok := c.workdayExceptions[c.civilKeyInBinding(date)]; ok {
			return workday
		}
	}

	local := date.In(c.bindingLoc())

	if c.cyclePattern != nil {
		idx := civilDays(c.cycleAnchor, local) % len(c.cyclePattern)
		if idx < 0 {
			idx += len(c.cyclePattern)
		}
		workday := c.cyclePattern[idx]
		if !workday {
			return false
		}
		if !c.holidaysSuspendWork {
			return true
		}

		_, obs, _ := c.IsHoliday(local)
		return !obs
	}

	var workday bool
	if c.WorkdayFunc == nil {
		workday = c.workday[date.Weekday()]
	} else {
		workday = c.WorkdayFunc(date)
	}

	if !workday {
		return false
	}

	_, obs, _ := c.IsHoliday(date)
	return !obs
}

// IsWorkTime reports whether a given date and time is within working hours.
func (c *BusinessCalendar) IsWorkTime(date time.Time) bool {
	if !c.IsWorkday(date) {
		return false
	}

	var startHour int
	var startMinute int
	var endHour int
	var endMinute int
	var startSecond int
	var endSecond int
	if c.WorkdayStartFunc == nil {
		startHour = int(c.workdayStart.Hours()) % 24
		startMinute = int(c.workdayStart.Minutes()) % 60
		startSecond = int(c.workdayStart.Seconds()) % 60
	} else {
		startTime := c.WorkdayStartFunc(date)
		startHour = startTime.Hour()
		startMinute = startTime.Minute()
		startSecond = startTime.Second()
	}
	if c.WorkdayEndFunc == nil {
		// Using 24h as "till the end of day" seems to be pretty common.
		// By subtracting a second we get the desired behavior.
		if c.workdayEnd == 24*time.Hour {
			endHour = 23
			endMinute = 59
			endSecond = 59
		} else {
			endHour = int(c.workdayEnd.Hours()) % 24
			endMinute = int(c.workdayEnd.Minutes()) % 60
			endSecond = int(c.workdayEnd.Seconds()) % 60
		}
	} else {
		endTime := c.WorkdayEndFunc(date)
		endHour = endTime.Hour()
		endMinute = endTime.Minute()
		endSecond = endTime.Second()
	}

	h, m, s := date.Clock()
	return (h == startHour && m == startMinute && s >= startSecond) ||
		(h == startHour && m > startMinute) ||
		(h > startHour && h < endHour) ||
		(h == endHour && m < endMinute) ||
		(h == endHour && m == endMinute && s <= endSecond)
}

// WorkdaysRemain reports the total number of remaining workdays in the month
// for the given date.
func (c *BusinessCalendar) WorkdaysRemain(date time.Time) int {
	n := 0
	month := date.Month()
	date = date.AddDate(0, 0, 1)
	for ; month == date.Month(); date = date.AddDate(0, 0, 1) {
		if c.IsWorkday(date) {
			n++
		}
	}
	return n

}

// WorkdaysInMonth reports the total number of workdays for the given year and
// month.
func (c *BusinessCalendar) WorkdaysInMonth(year int, month time.Month) int {
	t := time.Date(year, month, 1, 12, 0, 0, 0, time.UTC)
	if c.IsWorkday(t) {
		return c.WorkdaysRemain(t) + 1
	}

	return c.WorkdaysRemain(t)
}

// HolidaysInRange reports the number of holidays between the start and end
// times (inclusive).
func (c *BusinessCalendar) HolidaysInRange(start, end time.Time) int {
	factor := 1
	if end.Before(start) {
		factor = -1
		start, end = end, start
	}
	result := 0
	to := DayStart(end)
	for i := DayStart(start); i.Before(to) || i.Equal(to); i = i.AddDate(0, 0, 1) {
		_, holiday, _ := c.IsHoliday(i)
		if holiday {
			result++
		}
	}
	return factor * result
}

// WorkdaysInRange reports the number of workdays between the start and end
// times (inclusive).
func (c *BusinessCalendar) WorkdaysInRange(start, end time.Time) int {
	factor := 1
	if end.Before(start) {
		factor = -1
		start, end = end, start
	}
	result := 0
	to := DayStart(end)
	for i := DayStart(start); i.Before(to) || i.Equal(to); i = i.AddDate(0, 0, 1) {
		if c.IsWorkday(i) {
			result++
		}
	}
	return factor * result
}

// WorkdayN reports the day of the month that corresponds to the nth workday
// for the given year and month.
//
// The value of n affects the direction of counting:
//
//	n > 0: counting begins at the first day of the month.
//	n == 0: the result is always 0.
//	n < 0: counting begins at the end of the month.
func (c *BusinessCalendar) WorkdayN(year int, month time.Month, n int) int {
	var date time.Time
	var add int
	if n == 0 {
		return 0
	}

	if n > 0 {
		date = time.Date(year, month, 1, 12, 0, 0, 0, time.UTC)
		add = 1
	} else {
		date = time.Date(year, month+1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		add = -1
		n = -n
	}

	ndays := 0
	for ; month == date.Month(); date = date.AddDate(0, 0, add) {
		if c.IsWorkday(date) {
			ndays++
			if ndays == n {
				return date.Day()
			}
		}
	}
	return 0
}

// WorkdaysFrom reports the date of a workday that is offset days
// away from start.
//
// If n > 0, then the date returned is start + offset workdays.
// If n == 0, then the date is returned unchanged.
// If n < 0, then the date returned is start - offset workdays.
func (c *BusinessCalendar) WorkdaysFrom(start time.Time, offset int) time.Time {
	date := start
	var add int

	if offset == 0 {
		return start
	}

	if offset > 0 {
		add = 1
	} else {
		add = -1
		offset = -offset
	}

	for ndays := 0; ndays < offset; {
		date = date.AddDate(0, 0, add)
		if c.IsWorkday(date) {
			ndays++
		}
	}

	return date
}

// WorkHours reports the number of working hours for the given day.
func (c *BusinessCalendar) WorkHours(date time.Time) time.Duration {
	if !c.IsWorkday(date) {
		return 0
	}

	var startHour int
	var startMinute int
	var endHour int
	var endMinute int
	if c.WorkdayStartFunc == nil {
		startHour = int(c.workdayStart.Hours()) % 24
		startMinute = int(c.workdayStart.Minutes()) % 60
	} else {
		startTime := c.WorkdayStartFunc(date)
		startHour = startTime.Hour()
		startMinute = startTime.Minute()
	}
	if c.WorkdayEndFunc == nil {
		endHour = int(c.workdayEnd.Hours()) % 24
		endMinute = int(c.workdayEnd.Minutes()) % 60
	} else {
		endTime := c.WorkdayEndFunc(date)
		endHour = endTime.Hour()
		endMinute = endTime.Minute()
	}

	return (time.Duration(endHour)*time.Hour + time.Duration(endMinute)*time.Minute) -
		(time.Duration(startHour)*time.Hour + time.Duration(startMinute)*time.Minute)
}

// WorkdayStart reports the time at which work starts in the given day.
// If the day is not a workday, the zero time is returned.
func (c *BusinessCalendar) WorkdayStart(date time.Time) time.Time {
	if !c.IsWorkday(date) {
		return time.Time{}
	}

	if c.WorkdayStartFunc == nil {
		year, month, day := date.Date()
		return time.Date(year, month, day, 0, 0, 0, 0, date.Location()).Add(c.workdayStart)
	}
	return c.WorkdayStartFunc(date)
}

// WorkdayEnd reports the time at which work ends in the given day.
// If the day is not a workday, the zero time is returned.
func (c *BusinessCalendar) WorkdayEnd(date time.Time) time.Time {
	if !c.IsWorkday(date) {
		return time.Time{}
	}

	if c.WorkdayEndFunc == nil {
		year, month, day := date.Date()
		return time.Date(year, month, day, 0, 0, 0, 0, date.Location()).Add(c.workdayEnd)
	}
	return c.WorkdayEndFunc(date)
}

// NextWorkdayStart reports the start of the next work day from the given date.
func (c *BusinessCalendar) NextWorkdayStart(date time.Time) time.Time {
	t := date
	if date.After(c.WorkdayStart(date)) {
		t = t.Add(24 * time.Hour)
	}

	for !c.IsWorkday(t) {
		t = t.Add(24 * time.Hour)
	}
	return c.WorkdayStart(t)
}

// NextWorkdayEnd reports the end of the current or next work day from the given date.
func (c *BusinessCalendar) NextWorkdayEnd(date time.Time) time.Time {
	t := date
	if date.After(c.WorkdayEnd(date)) {
		t = t.Add(24 * time.Hour)
	}

	for !c.IsWorkday(t) {
		t = t.Add(24 * time.Hour)
	}
	return c.WorkdayEnd(t)
}

// WorkHoursInRange reports the working hours between the given start and end
// dates.
func (c *BusinessCalendar) WorkHoursInRange(start, end time.Time) time.Duration {
	r := time.Duration(0)
	if end.Before(start) {
		start, end = end, start
	}

	var current time.Time
	if c.IsWorkTime(start) {
		current = start
	} else {
		dayStart := c.WorkdayStart(start)
		if dayStart.IsZero() {
			current = c.NextWorkdayStart(start)
		} else {
			dayEnd := c.WorkdayEnd(start)
			if start.After(dayEnd) {
				current = c.NextWorkdayStart(start)
			} else {
				current = dayStart
			}
		}
	}

	for current.Before(end) {
		dayEnd := c.WorkdayEnd(current)
		last := MinTime(dayEnd, end)
		r += last.Sub(current)
		current = c.NextWorkdayStart(last)
	}
	return r
}

// AddWorkHours determines the time in the future where the worked hours will
// be completed.
//
// If duration <= 0, then the original date is returned.
func (c *BusinessCalendar) AddWorkHours(date time.Time, worked time.Duration) time.Time {
	if worked <= 0 {
		return date
	}

	start := date
	if !c.IsWorkday(start) {
		start = c.NextWorkdayStart(start)
	} else if !c.IsWorkTime(start) {
		dayStart := c.WorkdayStart(start)
		dayEnd := c.WorkdayEnd(start)
		if start.Before(dayStart) {
			start = dayStart
		} else if start.After(dayEnd) {
			start = c.NextWorkdayStart(start)
		}
	}

	var r time.Time
	for worked > 0 {
		dayEnd := c.WorkdayEnd(start)
		r = MinTime(start.Add(worked), dayEnd)
		worked -= c.WorkHoursInRange(start, r)
		start = c.NextWorkdayStart(dayEnd)
	}
	return r
}
