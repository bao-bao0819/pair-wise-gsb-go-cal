// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"fmt"
	"time"
)

// FiscalWeekPattern identifies the weekly structure of the fiscal periods
// within each quarter of a fiscal year.
type FiscalWeekPattern int

const (
	// FiscalPattern445 divides each fiscal quarter into periods of 4, 4,
	// and 5 weeks.
	FiscalPattern445 FiscalWeekPattern = iota
	// FiscalPattern454 divides each fiscal quarter into periods of 4, 5,
	// and 4 weeks.
	FiscalPattern454
	// FiscalPattern544 divides each fiscal quarter into periods of 5, 4,
	// and 4 weeks.
	FiscalPattern544
)

// periodWeeks reports the base number of weeks in each of the 12 fiscal
// periods for the pattern.
func (p FiscalWeekPattern) periodWeeks() ([12]int, bool) {
	var q [3]int
	switch p {
	case FiscalPattern445:
		q = [3]int{4, 4, 5}
	case FiscalPattern454:
		q = [3]int{4, 5, 4}
	case FiscalPattern544:
		q = [3]int{5, 4, 4}
	default:
		return [12]int{}, false
	}
	var w [12]int
	for i := 0; i < 12; i++ {
		w[i] = q[i%3]
	}
	return w, true
}

// FiscalYearEndRule determines how the year end anchor date is resolved to
// an actual year end date.
type FiscalYearEndRule int

const (
	// FiscalYearEndOnOrBefore selects the last occurrence of the configured
	// weekday on or before the anchor date.
	FiscalYearEndOnOrBefore FiscalYearEndRule = iota
	// FiscalYearEndNearest selects the occurrence of the configured weekday
	// nearest to the anchor date. Ties resolve to the earlier occurrence.
	FiscalYearEndNearest
)

// FiscalYearNaming determines how fiscal years are numbered.
type FiscalYearNaming int

const (
	// FiscalYearNamedByStart numbers a fiscal year by the Gregorian year in
	// which it begins.
	FiscalYearNamedByStart FiscalYearNaming = iota
	// FiscalYearNamedByEnd numbers a fiscal year by the Gregorian year in
	// which it ends.
	FiscalYearNamedByEnd
)

// FiscalConfig describes the structure of a 4-4-5 style fiscal calendar.
type FiscalConfig struct {
	// YearEndMonth and YearEndDay specify the anchor date used to locate
	// the end of each fiscal year (for example, January 31).
	YearEndMonth time.Month
	YearEndDay   int

	// YearEndWeekday is the weekday on which fiscal years end (for
	// example, Saturday).
	YearEndWeekday time.Weekday

	// YearEndRule selects the occurrence of YearEndWeekday nearest to, or
	// on or before, the anchor date as the actual year end.
	YearEndRule FiscalYearEndRule

	// Pattern is the weekly pattern of fiscal periods (4-4-5, 4-5-4, or
	// 5-4-4).
	Pattern FiscalWeekPattern

	// ExtraWeekPeriod is the fiscal period (1-12) that receives the
	// additional week in 53-week years.
	ExtraWeekPeriod int

	// Naming determines whether fiscal years are numbered by the Gregorian
	// year in which they start or in which they end.
	Naming FiscalYearNaming

	// MinYear and MaxYear declare the supported fiscal year range
	// (inclusive). Inputs that map outside of this range are rejected.
	MinYear int
	MaxYear int
}

// FiscalDate describes the position of a Gregorian date within a fiscal
// calendar.
type FiscalDate struct {
	Year         int // fiscal year number (per the configured naming rule)
	Quarter      int // fiscal quarter (1-4)
	Period       int // fiscal period within the year (1-12)
	Week         int // fiscal week within the year (1-52 or 1-53)
	WeekInPeriod int // fiscal week within the period (1-4, 1-5, or 1-6)
	Day          int // day within the fiscal week (1-7)
}

// FiscalCalendar represents a 4-4-5 style fiscal (retail) calendar.
//
// Fiscal year boundaries are determined solely by the configured year end
// anchor; 53-week years occur naturally whenever the distance between two
// consecutive year end dates is 371 days. Holidays from the associated
// BusinessCalendar may be used to count workdays within a period, but they
// never affect period boundaries.
type FiscalCalendar struct {
	config FiscalConfig
	weeks  [12]int // base weeks per period

	// Business is an optional business calendar used to determine workdays
	// within fiscal periods.
	Business *BusinessCalendar
}

// NewFiscalCalendar creates a FiscalCalendar from the given configuration.
// An error is returned if the configuration is invalid.
func NewFiscalCalendar(cfg FiscalConfig) (*FiscalCalendar, error) {
	weeks, ok := cfg.Pattern.periodWeeks()
	if !ok {
		return nil, fmt.Errorf("cal: invalid fiscal week pattern %d", cfg.Pattern)
	}
	if cfg.YearEndMonth < time.January || cfg.YearEndMonth > time.December {
		return nil, fmt.Errorf("cal: invalid fiscal year end month %d", cfg.YearEndMonth)
	}
	maxDay := time.Date(2001, cfg.YearEndMonth+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if cfg.YearEndDay < 1 || cfg.YearEndDay > maxDay {
		return nil, fmt.Errorf("cal: invalid fiscal year end day %d for month %d", cfg.YearEndDay, cfg.YearEndMonth)
	}
	if cfg.YearEndWeekday < time.Sunday || cfg.YearEndWeekday > time.Saturday {
		return nil, fmt.Errorf("cal: invalid fiscal year end weekday %d", cfg.YearEndWeekday)
	}
	if cfg.YearEndRule != FiscalYearEndOnOrBefore && cfg.YearEndRule != FiscalYearEndNearest {
		return nil, fmt.Errorf("cal: invalid fiscal year end rule %d", cfg.YearEndRule)
	}
	if cfg.ExtraWeekPeriod < 1 || cfg.ExtraWeekPeriod > 12 {
		return nil, fmt.Errorf("cal: invalid extra week period %d", cfg.ExtraWeekPeriod)
	}
	if cfg.Naming != FiscalYearNamedByStart && cfg.Naming != FiscalYearNamedByEnd {
		return nil, fmt.Errorf("cal: invalid fiscal year naming %d", cfg.Naming)
	}
	if cfg.MinYear > cfg.MaxYear {
		return nil, fmt.Errorf("cal: invalid fiscal year range %d-%d", cfg.MinYear, cfg.MaxYear)
	}
	return &FiscalCalendar{config: cfg, weeks: weeks}, nil
}

// errYearRange reports a fiscal year outside of the supported range.
func (c *FiscalCalendar) errYearRange(year int) error {
	return fmt.Errorf("cal: fiscal year %d outside supported range %d-%d",
		year, c.config.MinYear, c.config.MaxYear)
}

// checkYear validates that a fiscal year is within the supported range.
func (c *FiscalCalendar) checkYear(year int) error {
	if year < c.config.MinYear || year > c.config.MaxYear {
		return c.errYearRange(year)
	}
	return nil
}

// endGregorianYear reports the Gregorian year in which the given fiscal
// year ends.
func (c *FiscalCalendar) endGregorianYear(fiscalYear int) int {
	if c.config.Naming == FiscalYearNamedByEnd {
		return fiscalYear
	}
	return fiscalYear + 1
}

// yearEnd reports the fiscal year end date for the fiscal year ending in
// the given Gregorian year.
func (c *FiscalCalendar) yearEnd(gregYear int) time.Time {
	ref := time.Date(gregYear, c.config.YearEndMonth, c.config.YearEndDay, 0, 0, 0, 0, time.UTC)
	back := (int(ref.Weekday()) - int(c.config.YearEndWeekday) + 7) % 7
	if c.config.YearEndRule == FiscalYearEndNearest && back > 3 {
		return ref.AddDate(0, 0, 7-back)
	}
	return ref.AddDate(0, 0, -back)
}

// YearEnd reports the last day of the given fiscal year.
func (c *FiscalCalendar) YearEnd(year int) (time.Time, error) {
	if err := c.checkYear(year); err != nil {
		return time.Time{}, err
	}
	return c.yearEnd(c.endGregorianYear(year)), nil
}

// YearStart reports the first day of the given fiscal year.
func (c *FiscalCalendar) YearStart(year int) (time.Time, error) {
	if err := c.checkYear(year); err != nil {
		return time.Time{}, err
	}
	return c.yearEnd(c.endGregorianYear(year) - 1).AddDate(0, 0, 1), nil
}

// yearWeeks reports the number of weeks in each period of the fiscal year
// ending in the given Gregorian year, including the extra week for 53-week
// years.
func (c *FiscalCalendar) yearWeeks(endGregYear int) [12]int {
	weeks := c.weeks
	days := int(c.yearEnd(endGregYear).Sub(c.yearEnd(endGregYear-1)).Hours()/24)
	if days/7 == 53 {
		weeks[c.config.ExtraWeekPeriod-1]++
	}
	return weeks
}

// WeeksInYear reports the number of weeks (52 or 53) in the given fiscal
// year.
func (c *FiscalCalendar) WeeksInYear(year int) (int, error) {
	if err := c.checkYear(year); err != nil {
		return 0, err
	}
	g := c.endGregorianYear(year)
	return int(c.yearEnd(g).Sub(c.yearEnd(g-1)).Hours()/24) / 7, nil
}

// WeeksInPeriod reports the number of weeks in the given fiscal period,
// accounting for the extra week in 53-week years.
func (c *FiscalCalendar) WeeksInPeriod(year, period int) (int, error) {
	if err := c.checkYear(year); err != nil {
		return 0, err
	}
	if period < 1 || period > 12 {
		return 0, fmt.Errorf("cal: invalid fiscal period %d", period)
	}
	return c.yearWeeks(c.endGregorianYear(year))[period-1], nil
}

// FiscalDateOf maps a Gregorian date to its fiscal year, quarter, period,
// week, and day within the week. An error is returned if the date falls
// outside of the supported fiscal year range.
func (c *FiscalCalendar) FiscalDateOf(date time.Time) (FiscalDate, error) {
	y, m, dom := date.Date()
	d := time.Date(y, m, dom, 0, 0, 0, 0, time.UTC)

	// locate the Gregorian year in which the date's fiscal year ends; the
	// year end anchor can drift a few days across Gregorian year
	// boundaries, so both directions must be considered
	g := y
	if d.After(c.yearEnd(g)) {
		g++
	}
	if !d.After(c.yearEnd(g - 1)) {
		g--
	}

	year := g
	if c.config.Naming == FiscalYearNamedByStart {
		year = g - 1
	}
	if err := c.checkYear(year); err != nil {
		return FiscalDate{}, err
	}

	start := c.yearEnd(g-1).AddDate(0, 0, 1)
	dayOfYear := int(d.Sub(start).Hours() / 24) // 0-based
	week := dayOfYear/7 + 1
	day := dayOfYear%7 + 1

	weeks := c.yearWeeks(g)
	period := 0
	weekInPeriod := 0
	remaining := week
	for i := 0; i < 12; i++ {
		if remaining <= weeks[i] {
			period = i + 1
			weekInPeriod = remaining
			break
		}
		remaining -= weeks[i]
	}

	return FiscalDate{
		Year:         year,
		Quarter:      (period-1)/3 + 1,
		Period:       period,
		Week:         week,
		WeekInPeriod: weekInPeriod,
		Day:          day,
	}, nil
}

// Date reports the Gregorian date for the given fiscal coordinates: fiscal
// year, period (1-12), week within the period, and day within the week
// (1-7). It is the inverse of FiscalDateOf. An error is returned for
// coordinates that do not exist in the fiscal calendar.
func (c *FiscalCalendar) Date(year, period, weekInPeriod, day int) (time.Time, error) {
	if err := c.checkYear(year); err != nil {
		return time.Time{}, err
	}
	if period < 1 || period > 12 {
		return time.Time{}, fmt.Errorf("cal: invalid fiscal period %d", period)
	}
	g := c.endGregorianYear(year)
	weeks := c.yearWeeks(g)
	if weekInPeriod < 1 || weekInPeriod > weeks[period-1] {
		return time.Time{}, fmt.Errorf("cal: invalid week %d in fiscal period %d of year %d",
			weekInPeriod, period, year)
	}
	if day < 1 || day > 7 {
		return time.Time{}, fmt.Errorf("cal: invalid day %d of fiscal week", day)
	}

	offset := day - 1 + (weekInPeriod-1)*7
	for i := 0; i < period-1; i++ {
		offset += weeks[i] * 7
	}
	return c.yearEnd(g-1).AddDate(0, 0, 1+offset), nil
}

// PeriodStart reports the first day of the given fiscal period.
func (c *FiscalCalendar) PeriodStart(year, period int) (time.Time, error) {
	return c.Date(year, period, 1, 1)
}

// PeriodEnd reports the last day of the given fiscal period.
func (c *FiscalCalendar) PeriodEnd(year, period int) (time.Time, error) {
	if err := c.checkYear(year); err != nil {
		return time.Time{}, err
	}
	if period < 1 || period > 12 {
		return time.Time{}, fmt.Errorf("cal: invalid fiscal period %d", period)
	}
	return c.Date(year, period, c.yearWeeks(c.endGregorianYear(year))[period-1], 7)
}

// WorkdaysInPeriod reports the number of workdays in the given fiscal
// period according to the associated Business calendar. Holidays and
// workday rules only affect the count; they never change period
// boundaries. An error is returned if no Business calendar is associated.
func (c *FiscalCalendar) WorkdaysInPeriod(year, period int) (int, error) {
	if c.Business == nil {
		return 0, errors.New("cal: no business calendar associated with fiscal calendar")
	}
	start, err := c.PeriodStart(year, period)
	if err != nil {
		return 0, err
	}
	end, err := c.PeriodEnd(year, period)
	if err != nil {
		return 0, err
	}
	return c.Business.WorkdaysInRange(start, end), nil
}
