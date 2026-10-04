// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"testing"
	"time"
)

// testFiscalConfig is a 4-4-5 calendar ending on the last Saturday on or
// before December 31, named by its ending Gregorian year.
func testFiscalConfig() FiscalConfig {
	return FiscalConfig{
		YearEndMonth:    time.December,
		YearEndDay:      31,
		YearEndWeekday:  time.Saturday,
		Pattern:         FiscalPattern445,
		ExtraWeekPeriod: 12,
		NameByStartYear: false,
		MinYear:         2000,
		MaxYear:         2100,
	}
}

func utcDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestFiscalYearBoundaries(t *testing.T) {
	c, err := NewFiscalCalendar(testFiscalConfig())
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	// known year-end dates for a "last Saturday on or before Dec 31" anchor
	wantEnds := map[int]time.Time{
		2019: utcDate(2019, 12, 28),
		2020: utcDate(2020, 12, 26),
		2021: utcDate(2021, 12, 25),
		2022: utcDate(2022, 12, 31),
		2023: utcDate(2023, 12, 30),
		2024: utcDate(2024, 12, 28),
	}
	for fy, want := range wantEnds {
		end, err := c.YearEnd(fy)
		if err != nil {
			t.Fatalf("YearEnd(%d): %v", fy, err)
		}
		if !end.Equal(want) {
			t.Errorf("YearEnd(%d) = %s, want %s", fy, end, want)
		}
	}

	// adjacent fiscal years must be contiguous and non-overlapping
	for fy := 2001; fy <= 2100; fy++ {
		start, err := c.YearStart(fy)
		if err != nil {
			t.Fatalf("YearStart(%d): %v", fy, err)
		}
		prevEnd, err := c.YearEnd(fy - 1)
		if err != nil {
			t.Fatalf("YearEnd(%d): %v", fy-1, err)
		}
		if !start.Equal(prevEnd.AddDate(0, 0, 1)) {
			t.Errorf("fiscal year %d starts %s, previous ends %s", fy, start, prevEnd)
		}
		weeks, err := c.WeeksInYear(fy)
		if err != nil {
			t.Fatalf("WeeksInYear(%d): %v", fy, err)
		}
		if weeks != 52 && weeks != 53 {
			t.Errorf("fiscal year %d has %d weeks", fy, weeks)
		}
		end, _ := c.YearEnd(fy)
		if got := int(end.Sub(start).Hours()/24) + 1; got != weeks*7 {
			t.Errorf("fiscal year %d spans %d days, want %d", fy, got, weeks*7)
		}
	}
}

func TestFiscalRoundTrip(t *testing.T) {
	c, err := NewFiscalCalendar(testFiscalConfig())
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	// walk every day of several complete fiscal years that cross Gregorian
	// year boundaries, including the 53-week FY2022
	start, err := c.YearStart(2019)
	if err != nil {
		t.Fatalf("YearStart: %v", err)
	}
	end, err := c.YearEnd(2024)
	if err != nil {
		t.Fatalf("YearEnd: %v", err)
	}

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		fd, err := c.FiscalDate(d)
		if err != nil {
			t.Fatalf("FiscalDate(%s): %v", d, err)
		}

		// reverse mapping must return the same day
		back, err := c.Date(fd)
		if err != nil {
			t.Fatalf("Date(%+v): %v", fd, err)
		}
		if !back.Equal(d) {
			t.Fatalf("round trip failed for %s: got %s via %+v", d, back, fd)
		}

		// structural invariants
		if fd.Quarter != (fd.Period-1)/3+1 {
			t.Errorf("%s: quarter %d inconsistent with period %d", d, fd.Quarter, fd.Period)
		}
		if fd.Day < 1 || fd.Day > 7 {
			t.Errorf("%s: invalid day of week %d", d, fd.Day)
		}
		wip, err := c.WeeksInPeriod(fd.Year, fd.Period)
		if err != nil {
			t.Fatalf("WeeksInPeriod(%d, %d): %v", fd.Year, fd.Period, err)
		}
		if fd.Week < 1 || fd.Week > wip {
			t.Errorf("%s: week %d out of range 1-%d", d, fd.Week, wip)
		}

		// day of fiscal week must cycle 1-7 from the year start
		yearStart, err := c.YearStart(fd.Year)
		if err != nil {
			t.Fatalf("YearStart(%d): %v", fd.Year, err)
		}
		wantDay := int(d.Sub(yearStart).Hours()/24)%7 + 1
		if fd.Day != wantDay {
			t.Errorf("%s: day of week %d, want %d", d, fd.Day, wantDay)
		}
	}
}

func TestFiscal53WeekYear(t *testing.T) {
	c, err := NewFiscalCalendar(testFiscalConfig())
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	// find 53-week years from the actual anchor boundaries; there must be
	// at least one, and they must not follow a fixed 5-year schedule
	var long []int
	for fy := 2000; fy <= 2100; fy++ {
		weeks, err := c.WeeksInYear(fy)
		if err != nil {
			t.Fatalf("WeeksInYear(%d): %v", fy, err)
		}
		if weeks == 53 {
			long = append(long, fy)
		}
	}
	if len(long) == 0 {
		t.Fatal("no 53-week fiscal year found in supported range")
	}

	// FY2022 ends 2022-12-31, 371 days after FY2021 ends 2021-12-25
	if weeks, _ := c.WeeksInYear(2022); weeks != 53 {
		t.Errorf("WeeksInYear(2022) = %d, want 53", weeks)
	}
	found2022 := false
	for _, fy := range long {
		if fy == 2022 {
			found2022 = true
		}
	}
	if !found2022 {
		t.Error("FY2022 not detected as a 53-week year")
	}

	// the extra week belongs to the configured period (12): with a 4-4-5
	// pattern period 12 grows from 5 to 6 weeks, all others stay fixed
	wantWeeks := [12]int{4, 4, 5, 4, 4, 5, 4, 4, 5, 4, 4, 6}
	total := 0
	for p := 1; p <= 12; p++ {
		wip, err := c.WeeksInPeriod(2022, p)
		if err != nil {
			t.Fatalf("WeeksInPeriod(2022, %d): %v", p, err)
		}
		if wip != wantWeeks[p-1] {
			t.Errorf("WeeksInPeriod(2022, %d) = %d, want %d", p, wip, wantWeeks[p-1])
		}
		total += wip
	}
	if total != 53 {
		t.Errorf("period weeks sum to %d, want 53", total)
	}

	// the extra week is the last week of period 12 and ends on the year end
	start, end, err := c.PeriodRange(2022, 12)
	if err != nil {
		t.Fatalf("PeriodRange(2022, 12): %v", err)
	}
	if !start.Equal(utcDate(2022, 11, 20)) || !end.Equal(utcDate(2022, 12, 31)) {
		t.Errorf("PeriodRange(2022, 12) = %s to %s", start, end)
	}
	fd, err := c.FiscalDate(utcDate(2022, 12, 31))
	if err != nil {
		t.Fatalf("FiscalDate(2022-12-31): %v", err)
	}
	if fd != (FiscalDate{Year: 2022, Quarter: 4, Period: 12, Week: 6, Day: 7}) {
		t.Errorf("FiscalDate(2022-12-31) = %+v, want year 2022 period 12 week 6 day 7", fd)
	}
	fd, err = c.FiscalDate(utcDate(2022, 12, 25))
	if err != nil {
		t.Fatalf("FiscalDate(2022-12-25): %v", err)
	}
	if fd.Period != 12 || fd.Week != 6 || fd.Day != 1 {
		t.Errorf("FiscalDate(2022-12-25) = %+v, want period 12 week 6 day 1", fd)
	}

	// a 52-week year keeps the standard pattern
	for p, want := range [12]int{4, 4, 5, 4, 4, 5, 4, 4, 5, 4, 4, 5} {
		wip, err := c.WeeksInPeriod(2023, p+1)
		if err != nil {
			t.Fatalf("WeeksInPeriod(2023, %d): %v", p+1, err)
		}
		if wip != want {
			t.Errorf("WeeksInPeriod(2023, %d) = %d, want %d", p+1, wip, want)
		}
	}
}

func TestFiscalExtraWeekPeriod(t *testing.T) {
	// the same calendar with the extra week assigned to period 1
	cfg := testFiscalConfig()
	cfg.ExtraWeekPeriod = 1
	c, err := NewFiscalCalendar(cfg)
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	wip, err := c.WeeksInPeriod(2022, 1)
	if err != nil {
		t.Fatalf("WeeksInPeriod(2022, 1): %v", err)
	}
	if wip != 5 {
		t.Errorf("WeeksInPeriod(2022, 1) = %d, want 5", wip)
	}
	if wip, _ := c.WeeksInPeriod(2022, 12); wip != 5 {
		t.Errorf("WeeksInPeriod(2022, 12) = %d, want 5", wip)
	}

	// FY2022 starts 2021-12-26; with the extra week in period 1 the first
	// period runs 5 weeks and ends 2022-01-29
	start, end, err := c.PeriodRange(2022, 1)
	if err != nil {
		t.Fatalf("PeriodRange(2022, 1): %v", err)
	}
	if !start.Equal(utcDate(2021, 12, 26)) || !end.Equal(utcDate(2022, 1, 29)) {
		t.Errorf("PeriodRange(2022, 1) = %s to %s", start, end)
	}
}

func TestFiscalWeekPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern FiscalWeekPattern
		want    [3]int
	}{
		{FiscalPattern445, [3]int{4, 4, 5}},
		{FiscalPattern454, [3]int{4, 5, 4}},
		{FiscalPattern544, [3]int{5, 4, 4}},
	} {
		cfg := testFiscalConfig()
		cfg.Pattern = tc.pattern
		c, err := NewFiscalCalendar(cfg)
		if err != nil {
			t.Fatalf("NewFiscalCalendar(%v): %v", tc.pattern, err)
		}
		for q := 0; q < 4; q++ {
			for i := 0; i < 3; i++ {
				wip, err := c.WeeksInPeriod(2023, q*3+i+1)
				if err != nil {
					t.Fatalf("WeeksInPeriod: %v", err)
				}
				if wip != tc.want[i] {
					t.Errorf("pattern %v period %d: %d weeks, want %d",
						tc.pattern, q*3+i+1, wip, tc.want[i])
				}
			}
		}
	}
}

func TestFiscalYearNaming(t *testing.T) {
	cfg := testFiscalConfig()
	cfg.NameByStartYear = true
	c, err := NewFiscalCalendar(cfg)
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	// named by start year: FY2021 ends in Gregorian 2022 and is the
	// 53-week year
	end, err := c.YearEnd(2021)
	if err != nil {
		t.Fatalf("YearEnd(2021): %v", err)
	}
	if !end.Equal(utcDate(2022, 12, 31)) {
		t.Errorf("YearEnd(2021) = %s, want 2022-12-31", end)
	}
	if weeks, _ := c.WeeksInYear(2021); weeks != 53 {
		t.Errorf("WeeksInYear(2021) = %d, want 53", weeks)
	}

	fd, err := c.FiscalDate(utcDate(2022, 12, 31))
	if err != nil {
		t.Fatalf("FiscalDate(2022-12-31): %v", err)
	}
	if fd.Year != 2021 {
		t.Errorf("FiscalDate(2022-12-31).Year = %d, want 2021", fd.Year)
	}
}

func TestFiscalInvalidConfig(t *testing.T) {
	bad := []FiscalConfig{
		func() FiscalConfig { c := testFiscalConfig(); c.YearEndMonth = 0; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.YearEndMonth = 13; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.YearEndDay = 0; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.YearEndDay = 32; return c }(),
		func() FiscalConfig {
			c := testFiscalConfig()
			c.YearEndMonth = time.February
			c.YearEndDay = 30
			return c
		}(),
		func() FiscalConfig { c := testFiscalConfig(); c.YearEndWeekday = 7; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.Pattern = FiscalWeekPattern{4, 4, 4}; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.Pattern = FiscalWeekPattern{5, 5, 5}; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.ExtraWeekPeriod = 0; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.ExtraWeekPeriod = 13; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.MinYear = 0; return c }(),
		func() FiscalConfig { c := testFiscalConfig(); c.MinYear = 2100; c.MaxYear = 2000; return c }(),
	}
	for i, cfg := range bad {
		if _, err := NewFiscalCalendar(cfg); err == nil {
			t.Errorf("bad config %d accepted: %+v", i, cfg)
		}
	}
}

func TestFiscalOutOfRange(t *testing.T) {
	c, err := NewFiscalCalendar(testFiscalConfig())
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	if _, err := c.FiscalDate(utcDate(1990, 6, 15)); err == nil {
		t.Error("FiscalDate(1990-06-15) accepted, want range error")
	}
	if _, err := c.FiscalDate(utcDate(2200, 6, 15)); err == nil {
		t.Error("FiscalDate(2200-06-15) accepted, want range error")
	}
	if _, err := c.YearStart(1999); err == nil {
		t.Error("YearStart(1999) accepted, want range error")
	}
	if _, err := c.YearEnd(2101); err == nil {
		t.Error("YearEnd(2101) accepted, want range error")
	}
	if _, err := c.Date(FiscalDate{Year: 2101, Period: 1, Week: 1, Day: 1}); err == nil {
		t.Error("Date with year 2101 accepted, want range error")
	}
}

func TestFiscalDateInvalid(t *testing.T) {
	c, err := NewFiscalCalendar(testFiscalConfig())
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	bad := []FiscalDate{
		{Year: 2022, Period: 0, Week: 1, Day: 1},
		{Year: 2022, Period: 13, Week: 1, Day: 1},
		{Year: 2022, Period: 1, Week: 0, Day: 1},
		{Year: 2022, Period: 1, Week: 5, Day: 1},  // period 1 has 4 weeks
		{Year: 2023, Period: 12, Week: 6, Day: 1}, // 52-week year
		{Year: 2022, Period: 1, Week: 1, Day: 0},
		{Year: 2022, Period: 1, Week: 1, Day: 8},
		{Year: 2022, Quarter: 2, Period: 1, Week: 1, Day: 1}, // inconsistent
	}
	for _, fd := range bad {
		if _, err := c.Date(fd); err == nil {
			t.Errorf("Date(%+v) accepted, want error", fd)
		}
	}
}

func TestFiscalWorkdaysInPeriod(t *testing.T) {
	c, err := NewFiscalCalendar(testFiscalConfig())
	if err != nil {
		t.Fatalf("NewFiscalCalendar: %v", err)
	}

	bc := NewBusinessCalendar()

	// FY2022 period 3 runs 2022-02-20 through 2022-03-26 (5 weeks)
	start, end, err := c.PeriodRange(2022, 3)
	if err != nil {
		t.Fatalf("PeriodRange(2022, 3): %v", err)
	}
	if !start.Equal(utcDate(2022, 2, 20)) || !end.Equal(utcDate(2022, 3, 26)) {
		t.Fatalf("PeriodRange(2022, 3) = %s to %s", start, end)
	}

	n, err := c.WorkdaysInPeriod(bc, 2022, 3)
	if err != nil {
		t.Fatalf("WorkdaysInPeriod: %v", err)
	}
	if n != 25 {
		t.Errorf("WorkdaysInPeriod = %d, want 25", n)
	}

	// a holiday inside the period reduces the workday count but must not
	// change the period boundaries
	bc.AddHoliday(&Holiday{
		Name:  "Test Holiday",
		Month: time.March,
		Day:   14,
		Func:  CalcDayOfMonth,
	})
	n, err = c.WorkdaysInPeriod(bc, 2022, 3)
	if err != nil {
		t.Fatalf("WorkdaysInPeriod: %v", err)
	}
	if n != 24 {
		t.Errorf("WorkdaysInPeriod with holiday = %d, want 24", n)
	}
	start2, end2, err := c.PeriodRange(2022, 3)
	if err != nil {
		t.Fatalf("PeriodRange(2022, 3): %v", err)
	}
	if !start2.Equal(start) || !end2.Equal(end) {
		t.Error("period boundaries changed after adding a holiday")
	}

	if _, err := c.WorkdaysInPeriod(nil, 2022, 3); err == nil {
		t.Error("WorkdaysInPeriod with nil calendar accepted, want error")
	}
}
