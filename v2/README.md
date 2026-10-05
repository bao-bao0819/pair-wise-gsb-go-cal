# cal/v2: Go (golang) calendar library for dealing with holidays and work days

This library augments the Go time package to provide easy handling of holidays
and work days (business days).

Holiday instances are calculated from either builtin or user-created functions
to support exact days, floating days such as the 3rd Monday of the month, 
yearly offsets such as the 100th day of the year, or more complex rules such as 
offsets from Easter. Holidays may provide separate actual and observed dates 
for cases where holidays are celebrated on an alternate day if they fall on a
specific day of the week (usually weekends).

The Calendar type provides the basic functionality for creating a yearly 
calendar with holidays.

BusinessCalendar adds additional functionality for calculating workdays and 
hours worked.

# Differences from v1
For v2, much of the functionality of this library was rewritten to address
shortcomings of the v1 releases. This version provides the following benefits
over v1:
* Holidays
  * Observation rules are tied to individual holidays rather than a per-calendar 
  	setting
  * Name, description, and observance type fields added
  * Starting, ending, and exception year options
  * Holiday definitions are separated into subpackages by ISO code (no longer 
    necessary to bundle all holidays in the final binary)
* Calendar
  * Separation of business specific functionality into BusinessCalendar
  * Name and description fields added
  * Support for time.Location matching to ease use of multiple Calendars
* BusinessCalendar
  * Full support for working hours and related calculations
  * Work days and work start and end times can be provided by custom functions

# Example
Here is a simple usage example of a cron job that runs once per day:
```go
package main

import (
	"time"

	"github.com/rickar/cal/v2"
	"github.com/rickar/cal/v2/us"
)

func main() {
	c := cal.NewBusinessCalendar()
	c.Name = "Bigco, Inc."
	c.Description = "Default company calendar"

	// add holidays that the business observes
	c.AddHoliday(
		us.NewYear,
		us.MemorialDay,
		us.IndependenceDay,
		us.LaborDay,
		us.ThanksgivingDay,
		us.ChristmasDay,
	)

	// change the default of a Mon - Fri, 9am-5pm work week
	c.SetWorkday(time.Saturday, true)
	c.SetWorkHours(8*time.Hour, 18*time.Hour+30*time.Minute)

	t := time.Now()

	// run different batch processing jobs based on the day

	if c.IsWorkday(t) {
		// create daily activity reports
	}

if cal.IsWeekend(t) {
		// validate employee time submissions
	}

	if c.WorkdaysRemain(t) == 10 {
		// 10 business days before the end of month
		// send account statements to customers
	}

	if c.WorkdaysRemain(t) == 0 {
		// last business day of the month
		// execute auto billing transfers
	}

	// determine the number of working hours left in the current month
	nextMonth := cal.DayStart(cal.MonthStart(t.AddDate(0, 1, 0)))
	hoursLeft := c.WorkHoursInRange(t, nextMonth)

	// check if there are any tasks for this month that are in danger of missing their deadline
	pendingTasks := []struct{ pendingHours time.Duration }{{pendingHours: 32}} // assumed to be fetched from a DB or API
	for _, task := range pendingTasks {
		if hoursLeft < task.pendingHours {
			// send alert to management
		}
	}
}
```

## Joint availability

When several teams follow their own working calendars in different time
zones, `JointAvailability` finds the earliest continuous window during which
either every team, or at least `MinParticipants` teams, are available at the
same time.

Each calendar is paired with the explicit time zone in which its working
hours, weekends, and holidays are interpreted; those local rules are
resolved into absolute instants before the calendars are intersected, and
`DefaultLoc`/`time.Local` is never read or modified. Availability is computed
from continuous intervals, so time on the two sides of a break (lunch,
overnight, weekend) is never added together. The search only enumerates day
boundaries within the supplied finite range and sweeps the resulting
interval endpoints; it never probes the range point by point.

```go
ny, _ := time.LoadLocation("America/New_York")
tk, _ := time.LoadLocation("Asia/Tokyo")

result, err := cal.JointAvailability(
	[]cal.JointCalendar{
		{ID: "ny", Calendar: cal.NewBusinessCalendar(), Location: ny},
		{ID: "tk", Calendar: cal.NewBusinessCalendar(), Location: tk},
	},
	cal.JointOptions{
		From:            time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		To:              time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC),
		Duration:        2 * time.Hour,
		MinParticipants: 0, // 0 means every team must be available
	},
)
switch {
case err == cal.ErrJointNoWindow:
	// no continuous window long enough in the range
case err != nil:
	// ErrJointNoCalendars, ErrJointDuplicateID, ErrJointInvalidK, ...
default:
	w := result.Earliest
	fmt.Println(w.Start, w.End, w.Participants)
	// w.Boundaries holds the same instant expressed as local wall-clock
	// start/end times for each participating calendar
}
```

With `MinParticipants` greater than zero (and no larger than the number of
calendars), each returned window carries the sorted `Participants` list for
that interval; adjacent intervals whose participant set changes are reported
as separate windows.
