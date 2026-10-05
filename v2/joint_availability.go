// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"sort"
	"time"
)

var (
	// ErrNoJointAvailability indicates that no matching availability window
	// exists in the requested finite range.
	ErrNoJointAvailability = errors.New("no joint availability window")

	// ErrInvalidTimeRange indicates that the search range is empty or
	// reversed.
	ErrInvalidTimeRange = errors.New("availability search range must end after it starts")

	// ErrInvalidDuration indicates that the requested duration is zero or
	// negative.
	ErrInvalidDuration = errors.New("availability duration must be greater than zero")

	// ErrNoCalendars indicates that no calendars were supplied.
	ErrNoCalendars = errors.New("at least one calendar is required")

	// ErrEmptyCalendarID indicates that a calendar has no identifier.
	ErrEmptyCalendarID = errors.New("calendar ID must not be empty")

	// ErrDuplicateCalendarID indicates that two calendars use the same
	// identifier.
	ErrDuplicateCalendarID = errors.New("duplicate calendar ID")

	// ErrNilCalendar indicates that a calendar entry has no BusinessCalendar.
	ErrNilCalendar = errors.New("calendar must not be nil")

	// ErrNilLocation indicates that a calendar entry has no explicit
	// time.Location.
	ErrNilLocation = errors.New("calendar location must not be nil")

	// ErrInvalidMinimumParticipants indicates that the requested participant
	// count is outside the number of supplied calendars.
	ErrInvalidMinimumParticipants = errors.New("minimum participants must be between one and the number of calendars")

	// ErrInvalidWorkInterval indicates that a calendar produced an invalid
	// working-time interval.
	ErrInvalidWorkInterval = errors.New("invalid work interval")
)

// WorkTimeInterval is a continuous working-time interval.
type WorkTimeInterval struct {
	Start time.Time
	End   time.Time
}

// WorkdayIntervalsFn reports the continuous working-time intervals for the
// given local date. Separate intervals represent a break, such as a lunch
// break, and must not be combined by availability calculations.
type WorkdayIntervalsFn func(date time.Time) []WorkTimeInterval

// JointCalendar associates a business calendar with an explicit timezone and
// an identifier used in availability results.
type JointCalendar struct {
	ID       string
	Calendar *BusinessCalendar
	Location *time.Location
}

// LocalBoundary reports an availability window using a participant's local
// timezone.
type LocalBoundary struct {
	CalendarID string
	Start      time.Time
	End        time.Time
}

// JointAvailabilityWindow is a continuous interval when the calendars listed
// in Participants are all available.
type JointAvailabilityWindow struct {
	Start           time.Time
	End             time.Time
	Participants    []string
	LocalBoundaries []LocalBoundary
}

// WorkdayIntervals reports the continuous working-time intervals for a date.
// By default this is the calendar's single daily work interval. A calendar may
// set WorkdayIntervalsFunc to report intervals separated by breaks.
func (c *BusinessCalendar) WorkdayIntervals(date time.Time) []WorkTimeInterval {
	if !c.IsWorkday(date) {
		return nil
	}
	return c.workdayIntervalsFor(date)
}

func (c *BusinessCalendar) workdayIntervalsFor(date time.Time) []WorkTimeInterval {
	if c.WorkdayIntervalsFunc != nil {
		return normalizeWorkIntervals(c.WorkdayIntervalsFunc(date), date.Location())
	}

	start := dateWithClock(date, c.workdayStart)
	end := dateWithClock(date, c.workdayEnd)
	if c.WorkdayStartFunc != nil {
		start = c.WorkdayStartFunc(date)
	}
	if c.WorkdayEndFunc != nil {
		end = c.WorkdayEndFunc(date)
	}
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil
	}
	return []WorkTimeInterval{{Start: start, End: end}}
}

// EarliestJointAvailability returns the earliest continuous interval when all
// supplied calendars are available for duration.
func EarliestJointAvailability(calendars []JointCalendar, start, end time.Time, duration time.Duration) (JointAvailabilityWindow, error) {
	return EarliestAvailability(calendars, start, end, duration, len(calendars))
}

// EarliestAvailability returns the earliest continuous interval when at least
// minParticipants calendars are available for duration. A minParticipants
// value of zero means all calendars must participate.
func EarliestAvailability(calendars []JointCalendar, start, end time.Time, duration time.Duration, minParticipants int) (JointAvailabilityWindow, error) {
	windows, err := AvailabilityWindows(calendars, start, end, duration, minParticipants)
	if err != nil {
		return JointAvailabilityWindow{}, err
	}
	if len(windows) == 0 {
		return JointAvailabilityWindow{}, ErrNoJointAvailability
	}
	window := windows[0]
	window.End = window.Start.Add(duration)
	for index := range window.LocalBoundaries {
		loc := window.LocalBoundaries[index].Start.Location()
		window.LocalBoundaries[index].End = window.End.In(loc)
	}
	return window, nil
}

// AvailabilityWindows returns all continuous intervals of at least duration
// when minParticipants calendars are available. Results are ordered by their
// absolute start time and are split whenever the participant set changes. A
// minParticipants value of zero means all calendars must participate.
func AvailabilityWindows(calendars []JointCalendar, start, end time.Time, duration time.Duration, minParticipants int) ([]JointAvailabilityWindow, error) {
	if !start.Before(end) {
		return nil, ErrInvalidTimeRange
	}
	if duration <= 0 {
		return nil, ErrInvalidDuration
	}
	if len(calendars) == 0 {
		return nil, ErrNoCalendars
	}

	seen := make(map[string]bool, len(calendars))
	for _, calendar := range calendars {
		if calendar.ID == "" {
			return nil, ErrEmptyCalendarID
		}
		if seen[calendar.ID] {
			return nil, ErrDuplicateCalendarID
		}
		seen[calendar.ID] = true
		if calendar.Calendar == nil {
			return nil, ErrNilCalendar
		}
		if calendar.Location == nil {
			return nil, ErrNilLocation
		}
	}

	if minParticipants == 0 {
		minParticipants = len(calendars)
	}
	if minParticipants < 1 || minParticipants > len(calendars) {
		return nil, ErrInvalidMinimumParticipants
	}

	events, err := availabilityEvents(calendars, start, end)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].time.Before(events[j].time)
	})

	active := make(map[string]bool, len(calendars))
	var windows []JointAvailabilityWindow
	segmentStart := events[0].time
	for i := 0; i < len(events); {
		eventTime := events[i].time
		if eventTime.After(segmentStart) {
			if window, ok := availabilityWindow(segmentStart, eventTime, duration, minParticipants, calendars, active); ok {
				windows = append(windows, window)
			}
		}

		for i < len(events) && events[i].time.Equal(eventTime) {
			if events[i].ending {
				delete(active, events[i].calendarID)
			} else {
				active[events[i].calendarID] = true
			}
			i++
		}
		segmentStart = eventTime
	}

	return windows, nil
}

type availabilityEvent struct {
	time       time.Time
	calendarID string
	ending     bool
}

func availabilityEvents(calendars []JointCalendar, start, end time.Time) ([]availabilityEvent, error) {
	var events []availabilityEvent

	for _, calendar := range calendars {
		intervals, err := calendarWorkIntervals(calendar, start, end)
		if err != nil {
			return nil, err
		}
		for _, interval := range intervals {
			events = append(events,
				availabilityEvent{time: interval.Start, calendarID: calendar.ID},
				availabilityEvent{time: interval.End, calendarID: calendar.ID, ending: true},
			)
		}
	}

	return events, nil
}

func calendarWorkIntervals(calendar JointCalendar, start, end time.Time) ([]WorkTimeInterval, error) {
	loc := calendar.Location
	day := localDay(start.In(loc))
	lastDay := localDay(end.In(loc))

	var intervals []WorkTimeInterval
	for !day.After(lastDay) {
		date := day
		day = day.AddDate(0, 0, 1)

		if !isWorkdayAt(calendar.Calendar, date) {
			continue
		}

		dayIntervals := calendar.Calendar.workdayIntervalsFor(date)
		for index, interval := range dayIntervals {
			intervalStart := interval.Start
			intervalEnd := interval.End
			if !intervalStart.Before(intervalEnd) ||
				intervalStart.Before(date) ||
				intervalEnd.After(date.AddDate(0, 0, 1)) ||
				(index > 0 && intervalStart.Before(dayIntervals[index-1].End)) {
				return nil, ErrInvalidWorkInterval
			}

			if !intervalEnd.After(start) || !intervalStart.Before(end) {
				continue
			}

			if intervalStart.Before(start) {
				intervalStart = start
			}
			if intervalEnd.After(end) {
				intervalEnd = end
			}
			if intervalStart.Before(intervalEnd) {
				intervals = append(intervals, WorkTimeInterval{Start: intervalStart, End: intervalEnd})
			}
		}
	}

	return intervals, nil
}

func localDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

func isWorkdayAt(calendar *BusinessCalendar, date time.Time) bool {
	var workday bool
	if calendar.WorkdayFunc == nil {
		workday = calendar.workday[date.Weekday()]
	} else {
		workday = calendar.WorkdayFunc(date)
	}
	if !workday {
		return false
	}
	_, observed := isHolidayAt(&calendar.Calendar, date)
	return !observed
}

func isHolidayAt(calendar *Calendar, date time.Time) (bool, bool) {
	calendarAtLocation := &Calendar{Holidays: calendar.Holidays}
	var actual, observed bool
	actual, observed, _ = calendarAtLocation.IsHoliday(date)
	return actual, observed
}

func dateWithClock(date time.Time, value time.Duration) time.Time {
	year, month, day := date.Date()
	if value == 24*time.Hour {
		return time.Date(year, month, day+1, 0, 0, 0, 0, date.Location())
	}
	return time.Date(year, month, day,
		int(value.Hours())%24,
		int(value.Minutes())%60,
		int(value.Seconds())%60,
		int(value.Nanoseconds()%int64(time.Second)),
		date.Location())
}

func availabilityWindow(start, end time.Time, duration time.Duration, minParticipants int, calendars []JointCalendar, active map[string]bool) (JointAvailabilityWindow, bool) {
	if end.Sub(start) < duration || len(active) < minParticipants {
		return JointAvailabilityWindow{}, false
	}

	participants := make([]string, 0, len(active))
	for _, calendar := range calendars {
		if active[calendar.ID] {
			participants = append(participants, calendar.ID)
		}
	}
	sort.Strings(participants)

	boundaries := make([]LocalBoundary, 0, len(participants))
	for _, calendar := range calendars {
		if !active[calendar.ID] {
			continue
		}
		boundaries = append(boundaries, LocalBoundary{
			CalendarID: calendar.ID,
			Start:      start.In(calendar.Location),
			End:        end.In(calendar.Location),
		})
	}
	sort.Slice(boundaries, func(i, j int) bool {
		return boundaries[i].CalendarID < boundaries[j].CalendarID
	})

	return JointAvailabilityWindow{
		Start:           start,
		End:             end,
		Participants:    participants,
		LocalBoundaries: boundaries,
	}, true
}

func normalizeWorkIntervals(intervals []WorkTimeInterval, loc *time.Location) []WorkTimeInterval {
	if len(intervals) == 0 {
		return nil
	}

	normalized := make([]WorkTimeInterval, len(intervals))
	for index, interval := range intervals {
		normalized[index] = WorkTimeInterval{
			Start: ReplaceLocation(interval.Start, loc),
			End:   ReplaceLocation(interval.End, loc),
		}
	}

	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Start.Equal(normalized[j].Start) {
			return normalized[i].End.Before(normalized[j].End)
		}
		return normalized[i].Start.Before(normalized[j].Start)
	})

	return normalized
}
