// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"fmt"
	"time"
)

// FiscalWeekPattern describes how the weeks of a fiscal quarter are
// distributed across its three periods (months). The three values are the
// number of weeks in the first, second, and third period of each quarter.
type FiscalWeekPattern [3]int

// Supported fiscal week patterns.
var (
	FiscalPattern445 = FiscalWeekPattern{4, 4, 5}
	FiscalPattern454 = FiscalWeekPattern{4, 5, 4}
	FiscalPattern544 = FiscalWeekPattern{5, 4, 4}
)

func (p FiscalWeekPattern) valid() bool {
	return p == FiscalPattern445 || p == FiscalPattern454 || p == FiscalPattern544
}

// FiscalConfig holds the configuration for a FiscalCalendar.
type FiscalConfig struct {
	// YearEndMonth and YearEndDay specify the anchor date used to locate the
	// end of each fiscal year. If YearEndDay exceeds the number of days in
	// YearEndMonth for a given Gregorian year (e.g. Feb 29), it is clamped
	// to the last day of that month.
	YearEndMonth time.Month
	YearEndDay   int

	// YearEndWeekday is the weekday of the fiscal year end. Each fiscal year
	// ends on the last occurrence of this weekday on or before the anchor
	// date (YearEndMonth/YearEndDay) of the corresponding Gregorian year.
	YearEndWeekday time.Weekday

	// Pattern is the weekly pattern of each fiscal quarter. It must be one
	// of FiscalPattern445, FiscalPattern454, or FiscalPattern544.
	Pattern FiscalWeekPattern

	// ExtraWeekPeriod is the period (1-12) that receives the additional
	// week in a 53-week fiscal year.
	ExtraWeekPeriod int

	// NameByStartYear controls fiscal year numbering. When true, a fiscal
	// year is named after the Gregorian year in which it starts (its year
	// end anchor falls in the following Gregorian year). When false, it is
	// named after the Gregorian year in which it ends.
	NameByStartYear bool

	// MinYear and MaxYear declare the inclusive range of supported fiscal
	// years. Inputs that map to fiscal years outside this range are
	// rejected.
	MinYear int
	MaxYear int
}

// FiscalDate identifies a day within the fiscal calendar.
type FiscalDate struct {
	Year    int // fiscal year number (per the configured naming rule)
	Quarter int // fiscal quarter, 1-4
	Period  int // fiscal period (month) within the year, 1-12
	Week    int // week within the period, 1-based
	Day     int // day within the fiscal week, 1-7
}

// FiscalCalendar represents a 4-4-5 style fiscal (retail) calendar.
//
// Fiscal years consist of exactly 52 or 53 whole weeks. Whether a year has
// 53 weeks is determined by the actual anchor boundaries (the distance
// between consecutive year-end dates), not by a fixed insertion schedule.
//
// A FiscalCalendar is independent of BusinessCalendar: holiday and workday
// information from a BusinessCalendar may be used to count workdays within
// a period, but never alters period boundaries.
type FiscalCalendar struct {
	cfg FiscalConfig
}

// NewFiscalCalendar creates a FiscalCalendar from the given configuration.
// An error is returned if the configuration is invalid.
func NewFiscalCalendar(cfg FiscalConfig) (*FiscalCalendar, error) {
	if cfg.YearEndMonth < time.January || cfg.YearEndMonth > time.December {
		return nil, fmt.Errorf("cal: invalid fiscal year-end month %d", cfg.YearEndMonth)
	}
	if cfg.YearEndDay < 1 || cfg.YearEndDay > daysInMonth(cfg.YearEndMonth, true) {
		return nil, fmt.Errorf("cal: invalid fiscal year-end day %d for month %s",
			cfg.YearEndDay, cfg.YearEndMonth)
	}
	if cfg.YearEndWeekday < time.Sunday || cfg.YearEndWeekday > time.Saturday {
		return nil, fmt.Errorf("cal: invalid fiscal year-end weekday %d", cfg.YearEndWeekday)
	}
	if !cfg.Pattern.valid() {
		return nil, fmt.Errorf("cal: invalid fiscal week pattern %v", cfg.Pattern)
	}
	if cfg.ExtraWeekPeriod < 1 || cfg.ExtraWeekPeriod > 12 {
		return nil, fmt.Errorf("cal: invalid extra week period %d", cfg.ExtraWeekPeriod)
	}
	if cfg.MinYear < 1 || cfg.MaxYear > 9999 || cfg.MinYear > cfg.MaxYear {
		return nil, fmt.Errorf("cal: invalid supported fiscal year range %d-%d",
			cfg.MinYear, cfg.MaxYear)
	}
	return &FiscalCalendar{cfg: cfg}, nil
}

// daysInMonth reports the number of days in the given month. If leap is
// true, February is assumed to have 29 days.
func daysInMonth(month time.Month, leap bool) int {
	switch month {
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if leap {
			return 29
		}
		return 28
	}
	return 31
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// endGregorianYear reports the Gregorian year in which the anchor for the
// given fiscal year falls.
func (c *FiscalCalendar) endGregorianYear(fy int) int {
	if c.cfg.NameByStartYear {
		return fy + 1
	}
	return fy
}

// fiscalYearForEndYear reports the fiscal year number whose year-end anchor
// falls in the given Gregorian year.
func (c *FiscalCalendar) fiscalYearForEndYear(e int) int {
	if c.cfg.NameByStartYear {
		return e - 1
	}
	return e
}

// anchorEnd computes the fiscal year-end date for the fiscal year whose
// anchor falls in Gregorian year e: the last configured weekday on or
// before the configured anchor date. The result is at midnight UTC.
func (c *FiscalCalendar) anchorEnd(e int) time.Time {
	day := c.cfg.YearEndDay
	if max := daysInMonth(c.cfg.YearEndMonth, isLeapYear(e)); day > max {
		day = max
	}
	d := time.Date(e, c.cfg.YearEndMonth, day, 0, 0, 0, 0, time.UTC)
	diff := (int(d.Weekday()) - int(c.cfg.YearEndWeekday) + 7) % 7
	return d.AddDate(0, 0, -diff)
}

func (c *FiscalCalendar) checkYear(fy int) error {
	if fy < c.cfg.MinYear || fy > c.cfg.MaxYear {
		return fmt.Errorf("cal: fiscal year %d outside supported range %d-%d",
			fy, c.cfg.MinYear, c.cfg.MaxYear)
	}
	return nil
}

// YearStart reports the first day of the given fiscal year (midnight UTC).
// An error is returned if the fiscal year is outside the supported range.
func (c *FiscalCalendar) YearStart(fy int) (time.Time, error) {
	if err := c.checkYear(fy); err != nil {
		return time.Time{}, err
	}
	return c.anchorEnd(c.endGregorianYear(fy)-1).AddDate(0, 0, 1), nil
}

// YearEnd reports the last day of the given fiscal year (midnight UTC).
// An error is returned if the fiscal year is outside the supported range.
func (c *FiscalCalendar) YearEnd(fy int) (time.Time, error) {
	if err := c.checkYear(fy); err != nil {
		return time.Time{}, err
	}
	return c.anchorEnd(c.endGregorianYear(fy)), nil
}

// WeeksInYear reports the number of weeks (52 or 53) in the given fiscal
// year. An error is returned if the fiscal year is outside the supported
// range.
func (c *FiscalCalendar) WeeksInYear(fy int) (int, error) {
	if err := c.checkYear(fy); err != nil {
		return 0, err
	}
	e := c.endGregorianYear(fy)
	days := int(c.anchorEnd(e).Sub(c.anchorEnd(e-1)).Hours() / 24)
	return days / 7, nil
}

// WeeksInPeriod reports the number of weeks in the given period of the
// given fiscal year, accounting for the extra week in 53-week years.
func (c *FiscalCalendar) WeeksInPeriod(fy, period int) (int, error) {
	if err := c.checkYear(fy); err != nil {
		return 0, err
	}
	if period < 1 || period > 12 {
		return 0, fmt.Errorf("cal: invalid fiscal period %d", period)
	}
	weeks := c.cfg.Pattern[(period-1)%3]
	if period == c.cfg.ExtraWeekPeriod {
		wiy, err := c.WeeksInYear(fy)
		if err != nil {
			return 0, err
		}
		if wiy == 53 {
			weeks++
		}
	}
	return weeks, nil
}

// weeksBeforePeriod reports the number of weeks before the start of the
// given period within the given fiscal year. The period must be valid.
func (c *FiscalCalendar) weeksBeforePeriod(fy, period int) int {
	weeksPerQuarter := c.cfg.Pattern[0] + c.cfg.Pattern[1] + c.cfg.Pattern[2]
	weeks := weeksPerQuarter * ((period - 1) / 3)
	for i := 0; i < (period-1)%3; i++ {
		weeks += c.cfg.Pattern[i]
	}
	if c.cfg.ExtraWeekPeriod < period {
		if wiy, err := c.WeeksInYear(fy); err == nil && wiy == 53 {
			weeks++
		}
	}
	return weeks
}

// FiscalDate maps a Gregorian date to its fiscal year, quarter, period,
// week, and day of week. An error is returned if the date falls outside
// the supported fiscal year range.
func (c *FiscalCalendar) FiscalDate(t time.Time) (FiscalDate, error) {
	year, month, day := t.Date()
	d := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)

	// locate the Gregorian year of the anchor that ends the fiscal year
	// containing d
	e := year
	for d.After(c.anchorEnd(e)) {
		e++
	}
	for !d.After(c.anchorEnd(e - 1)) {
		e--
	}

	fy := c.fiscalYearForEndYear(e)
	if err := c.checkYear(fy); err != nil {
		return FiscalDate{}, err
	}

	start := c.anchorEnd(e-1).AddDate(0, 0, 1)
	dayOfYear := int(d.Sub(start).Hours()/24) + 1
	weekOfYear := (dayOfYear-1)/7 + 1
	dayOfWeek := (dayOfYear-1)%7 + 1

	// walk periods to find the one containing weekOfYear
	period := 0
	weeksSeen := 0
	for p := 1; p <= 12; p++ {
		wip, err := c.WeeksInPeriod(fy, p)
		if err != nil {
			return FiscalDate{}, err
		}
		if weekOfYear <= weeksSeen+wip {
			period = p
			break
		}
		weeksSeen += wip
	}
	if period == 0 {
		return FiscalDate{}, fmt.Errorf("cal: unable to locate fiscal period for %s", d)
	}

	return FiscalDate{
		Year:    fy,
		Quarter: (period-1)/3 + 1,
		Period:  period,
		Week:    weekOfYear - weeksSeen,
		Day:     dayOfWeek,
	}, nil
}

// Date maps a fiscal date back to its Gregorian date (midnight UTC). The
// Quarter field is optional; if non-zero it must be consistent with the
// Period. An error is returned for invalid or out-of-range input.
func (c *FiscalCalendar) Date(fd FiscalDate) (time.Time, error) {
	if err := c.checkYear(fd.Year); err != nil {
		return time.Time{}, err
	}
	if fd.Period < 1 || fd.Period > 12 {
		return time.Time{}, fmt.Errorf("cal: invalid fiscal period %d", fd.Period)
	}
	if fd.Quarter != 0 && fd.Quarter != (fd.Period-1)/3+1 {
		return time.Time{}, fmt.Errorf("cal: fiscal quarter %d inconsistent with period %d",
			fd.Quarter, fd.Period)
	}
	wip, err := c.WeeksInPeriod(fd.Year, fd.Period)
	if err != nil {
		return time.Time{}, err
	}
	if fd.Week < 1 || fd.Week > wip {
		return time.Time{}, fmt.Errorf("cal: invalid week %d for fiscal period %d of year %d (1-%d)",
			fd.Week, fd.Period, fd.Year, wip)
	}
	if fd.Day < 1 || fd.Day > 7 {
		return time.Time{}, fmt.Errorf("cal: invalid fiscal day of week %d", fd.Day)
	}

	start := c.anchorEnd(c.endGregorianYear(fd.Year)-1).AddDate(0, 0, 1)
	offset := (c.weeksBeforePeriod(fd.Year, fd.Period)+fd.Week-1)*7 + fd.Day - 1
	return start.AddDate(0, 0, offset), nil
}

// PeriodRange reports the first and last day (midnight UTC) of the given
// period of the given fiscal year. Period boundaries are determined solely
// by the fiscal configuration; holidays never affect them.
func (c *FiscalCalendar) PeriodRange(fy, period int) (start, end time.Time, err error) {
	start, err = c.Date(FiscalDate{Year: fy, Period: period, Week: 1, Day: 1})
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	wip, err := c.WeeksInPeriod(fy, period)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end = start.AddDate(0, 0, wip*7-1)
	return start, end, nil
}

// WorkdaysInPeriod reports the number of workdays in the given period of
// the given fiscal year according to the given business calendar. Holiday
// and workday rules from bc affect only the count, never the period
// boundaries.
func (c *FiscalCalendar) WorkdaysInPeriod(bc *BusinessCalendar, fy, period int) (int, error) {
	if bc == nil {
		return 0, errors.New("cal: nil business calendar")
	}
	start, end, err := c.PeriodRange(fy, period)
	if err != nil {
		return 0, err
	}
	n := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if bc.IsWorkday(d) {
			n++
		}
	}
	return n, nil
}
