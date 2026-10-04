// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"time"
)

// ErrEmptyRotation is returned when a shift rotation pattern is empty.
var ErrEmptyRotation = errors.New("cal: shift rotation pattern must not be empty")

// ErrNoRotation is returned when a shift rotation operation requires a
// rotation that has not been configured.
var ErrNoRotation = errors.New("cal: shift rotation is not configured")

// civilDate identifies a Gregorian calendar date without a time of day.
type civilDate struct {
	year  int
	month time.Month
	day   int
}

// rotationSchedule is a repeating N-day work pattern anchored to a civil
// date in a bound timezone.
type rotationSchedule struct {
	loc              *time.Location     // bound timezone for phase calculations
	anchor           civilDate          // civil date of pattern index 0
	anchorJDN        int                // Julian day number of the anchor date
	pattern          []bool             // N-day sequence; true means workday
	holidaysOverride bool               // true: holidays stop work; false: rotation continues
	exceptions       map[civilDate]bool // temporary per-date overrides
}

// civilJDN reports the Julian Day Number for the given Gregorian civil date.
// Unlike JulianDayNumber it is purely date-based, so differences between
// results are exact calendar-day counts regardless of time of day or DST.
func civilJDN(year int, month time.Month, day int) int {
	a := (14 - int(month)) / 12
	y := year + 4800 - a
	m := int(month) + 12*a - 3
	return day + (153*m+2)/5 + 365*y + y/4 - y/100 + y/400 - 32045
}

// SetShiftRotation configures the calendar to use a rotating N-day work
// pattern (for example two days on, two days off) instead of the fixed
// weekday flags or WorkdayFunc.
//
// The civil date of anchor corresponds to pattern index 0, and the anchor's
// location becomes the bound timezone: instants passed to IsWorkday are
// converted to this timezone before their cycle position is determined.
// Cycle positions are computed from calendar-day differences (never by
// dividing elapsed time by 24 hours), so DST transitions do not disturb the
// phase. Dates before the anchor are positioned with a non-negative
// mathematical remainder, extending the pattern seamlessly in both
// directions.
//
// The pattern must not be empty. It is copied internally, so later changes
// to the caller's slice do not affect the calendar.
//
// holidaysOverride controls how holidays interact with the rotation: when
// true, an observed holiday stops work even if the pattern marks the day as
// a workday; when false, the rotation continues as usual on holidays.
func (c *BusinessCalendar) SetShiftRotation(anchor time.Time, pattern []bool, holidaysOverride bool) error {
	if len(pattern) == 0 {
		return ErrEmptyRotation
	}
	p := make([]bool, len(pattern))
	copy(p, pattern)
	year, month, day := anchor.Date()
	c.rotation = &rotationSchedule{
		loc:              anchor.Location(),
		anchor:           civilDate{year: year, month: month, day: day},
		anchorJDN:        civilJDN(year, month, day),
		pattern:          p,
		holidaysOverride: holidaysOverride,
	}
	return nil
}

// SetShiftException temporarily overrides the rotation result for the civil
// date containing the given instant (converted to the bound timezone). The
// override is the outermost layer: it takes precedence over the cycle and
// over holiday handling. It applies only to that single date and does not
// shift the cycle phase of any later date.
//
// ErrNoRotation is returned if no shift rotation has been configured.
func (c *BusinessCalendar) SetShiftException(date time.Time, workday bool) error {
	if c.rotation == nil {
		return ErrNoRotation
	}
	if c.rotation.exceptions == nil {
		c.rotation.exceptions = make(map[civilDate]bool)
	}
	c.rotation.exceptions[c.rotation.civilDateOf(date)] = workday
	return nil
}

// ClearShiftException removes a temporary override previously set with
// SetShiftException. It does nothing if no shift rotation is configured.
func (c *BusinessCalendar) ClearShiftException(date time.Time) {
	if c.rotation == nil {
		return
	}
	delete(c.rotation.exceptions, c.rotation.civilDateOf(date))
}

// civilDateOf reports the civil date of the given instant in the bound
// timezone.
func (r *rotationSchedule) civilDateOf(date time.Time) civilDate {
	local := date.In(r.loc)
	year, month, day := local.Date()
	return civilDate{year: year, month: month, day: day}
}

// rotationWorkday reports whether the given instant is a workday according
// to the rotation schedule.
func (c *BusinessCalendar) rotationWorkday(date time.Time) bool {
	r := c.rotation
	cd := r.civilDateOf(date)
	if workday, ok := r.exceptions[cd]; ok {
		return workday
	}
	diff := civilJDN(cd.year, cd.month, cd.day) - r.anchorJDN
	n := len(r.pattern)
	idx := ((diff % n) + n) % n
	if !r.pattern[idx] {
		return false
	}
	if r.holidaysOverride {
		local := date.In(r.loc)
		_, observed, _ := c.IsHoliday(local)
		return !observed
	}
	return true
}
