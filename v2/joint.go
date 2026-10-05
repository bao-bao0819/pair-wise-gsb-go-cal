// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"sort"
	"time"
)

// Errors returned by JointAvailability. Callers can compare against these
// values directly (or with errors.Is) to distinguish the failure reason.
var (
	// ErrJointNoCalendars indicates that no calendars were supplied.
	ErrJointNoCalendars = errors.New("joint availability: at least one calendar is required")

	// ErrJointInvalidRange indicates that the search range is empty or
	// invalid, the duration is non-positive, or a required parameter is nil.
	ErrJointInvalidRange = errors.New("joint availability: invalid search range or duration")

	// ErrJointDuplicateID indicates that two or more calendars share the same
	// ID.
	ErrJointDuplicateID = errors.New("joint availability: duplicate calendar id")

	// ErrJointInvalidK indicates that MinParticipants is out of range for
	// the number of calendars supplied.
	ErrJointInvalidK = errors.New("joint availability: min participants out of range")

	// ErrJointNoWindow indicates that no qualifying continuous window exists
	// within the search range.
	ErrJointNoWindow = errors.New("joint availability: no window of sufficient length in the given range")
)

// JointCalendar associates a BusinessCalendar with the explicit time zone in
// which its working hours, weekends, and holidays are to be interpreted, and
// the identifier under which that team's participation is reported.
type JointCalendar struct {
	// ID uniquely identifies the team (or calendar) in the result.
	ID string

	// Calendar is the business calendar used for this team.
	Calendar *BusinessCalendar

	// Location is the time zone in which the calendar's working hours and
	// holidays are interpreted. It must be non-nil.
	Location *time.Location
}

// JointOptions controls a joint availability search.
type JointOptions struct {
	// From and To delimit the finite search range. The range is half-open:
	// windows may begin at From, and must end no later than To. Both values
	// are interpreted as absolute instants; their own locations are
	// irrelevant to the search.
	From time.Time
	To   time.Time

	// Duration is the required continuous length of the window. Time on
	// either side of a break (such as a lunch hour or overnight gap) is
	// never combined.
	Duration time.Duration

	// MinParticipants requires at least this many teams to be available at
	// the same time. A value of zero requires every team; negative values
	// are invalid.
	MinParticipants int
}

// JointBoundary records the local start and end of a window as interpreted by
// one participating calendar in its own time zone.
type JointBoundary struct {
	// ID is the identifier of the calendar (see JointCalendar.ID).
	ID string

	// Start and End are the window boundaries expressed as wall-clock times
	// in the calendar's explicit location.
	Start time.Time
	End   time.Time
}

// JointWindow is a maximal continuous interval during which the reported
// participants are simultaneously available. Its length is always at least
// the requested duration, but it extends to the full extent of the common
// availability so callers can choose any sub-interval inside it.
type JointWindow struct {
	// Start and End are the absolute (instant) boundaries of the window.
	Start time.Time
	End   time.Time

	// Participants are the IDs of the calendars available for the whole
	// window, sorted in ascending order.
	Participants []string

	// Boundaries are the local (per time zone) boundaries for every
	// participant, in the same order as Participants.
	Boundaries []JointBoundary
}

// JointResult describes the outcome of a JointAvailability search.
type JointResult struct {
	// Windows lists every maximal continuous interval in the search range
	// during which enough teams were available, ordered by starting instant.
	// Each window is long enough to host the requested duration.
	//
	// Adjacent intervals with different participant sets are reported as
	// separate windows; intervals with the same participant set are merged.
	Windows []JointWindow

	// Earliest is the earliest window in Windows, or nil if there is none.
	Earliest *JointWindow
}

// jointInterval is a half-open absolute interval [Start, End) for one
// participant.
type jointInterval struct {
	start time.Time
	end   time.Time
}

// jointEvent marks the beginning (+1) or end (-1) of a participant interval.
type jointEvent struct {
	at  time.Time
	id  string
	add bool
}

// JointAvailability searches a finite range for the earliest continuous window
// during which either every team, or at least opts.MinParticipants teams, are
// simultaneously available according to their own business calendars.
//
// Each calendar is interpreted in its own explicit time zone: its working
// hours, weekends, and holidays are resolved into absolute instants before the
// calendars are intersected. time.Local (DefaultLoc) is never read or
// modified.
//
// Time is never accumulated across a break: each calendar contributes one
// continuous interval per workday, and intersections are computed on those
// intervals, so availability on the two sides of a lunch hour or an overnight
// gap cannot be stitched together.
//
// The search only enumerates day boundaries within the finite range and
// interval endpoints; it never probes the range instant by instant.
func JointAvailability(calendars []JointCalendar, opts JointOptions) (*JointResult, error) {
	if len(calendars) == 0 {
		return nil, ErrJointNoCalendars
	}
	if !opts.From.Before(opts.To) || opts.Duration <= 0 {
		return nil, ErrJointInvalidRange
	}

	seen := make(map[string]struct{}, len(calendars))
	for i := range calendars {
		jc := calendars[i]
		if jc.Calendar == nil || jc.Location == nil || jc.ID == "" {
			return nil, ErrJointInvalidRange
		}
		if _, dup := seen[jc.ID]; dup {
			return nil, ErrJointDuplicateID
		}
		seen[jc.ID] = struct{}{}
	}

	need := opts.MinParticipants
	if need < 0 || need > len(calendars) {
		return nil, ErrJointInvalidK
	}
	if need == 0 {
		need = len(calendars)
	}

	events := collectJointEvents(calendars, opts.From, opts.To)
	segments := sweepJointEvents(events, opts.From, opts.To)

	res := &JointResult{}
	for _, seg := range segments {
		if len(seg.participants) < need {
			continue
		}
		if seg.end.Sub(seg.start) < opts.Duration {
			continue
		}

		window := JointWindow{
			Start:        seg.start,
			End:          seg.end,
			Participants: append([]string(nil), seg.participants...),
			Boundaries:   make([]JointBoundary, 0, len(seg.participants)),
		}
		for _, id := range seg.participants {
			loc := locationFor(calendars, id)
			window.Boundaries = append(window.Boundaries, JointBoundary{
				ID:    id,
				Start: seg.start.In(loc),
				End:   seg.end.In(loc),
			})
		}
		res.Windows = append(res.Windows, window)
	}

	if len(res.Windows) == 0 {
		return nil, ErrJointNoWindow
	}
	res.Earliest = &res.Windows[0]
	return res, nil
}

func locationFor(calendars []JointCalendar, id string) *time.Location {
	for i := range calendars {
		if calendars[i].ID == id {
			return calendars[i].Location
		}
	}
	return time.UTC
}

// collectJointEvents enumerates, for each calendar, the workday boundaries
// inside the finite search range (expressed in the calendar's own time zone),
// converts them to absolute instants, clips them to the range, and returns the
// resulting start/end events.
func collectJointEvents(calendars []JointCalendar, from, to time.Time) []jointEvent {
	var events []jointEvent

	for i := range calendars {
		jc := calendars[i]

		// Enumerate local dates that could intersect the range. The local
		// dates at either extreme are sufficient because each workday interval
		// is contained within a single local day (23-25h around a DST shift).
		localFrom := from.In(jc.Location)
		localTo := to.In(jc.Location)
		day := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(),
			12, 0, 0, 0, jc.Location)
		last := time.Date(localTo.Year(), localTo.Month(), localTo.Day(),
			12, 0, 0, 0, jc.Location)

		var intervals []jointInterval
		for !day.After(last) {
			start := jc.Calendar.WorkdayStart(day)
			end := jc.Calendar.WorkdayEnd(day)
			if !start.IsZero() && !end.IsZero() && end.After(start) {
				if start.Before(from) {
					start = from
				}
				if end.After(to) {
					end = to
				}
				if end.After(start) {
					intervals = append(intervals, jointInterval{start: start, end: end})
				}
			}
			day = day.AddDate(0, 0, 1)
		}

		for _, in := range mergeJointIntervals(intervals) {
			events = append(events,
				jointEvent{at: in.start, id: jc.ID, add: true},
				jointEvent{at: in.end, id: jc.ID, add: false},
			)
		}
	}

	return events
}

// mergeJointIntervals sorts and unions overlapping or touching intervals so
// that events for a single participant never cancel each other prematurely.
func mergeJointIntervals(in []jointInterval) []jointInterval {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].start.Equal(in[j].start) {
			return in[i].end.Before(in[j].end)
		}
		return in[i].start.Before(in[j].start)
	})

	out := in[:1]
	for _, cur := range in[1:] {
		last := &out[len(out)-1]
		if !cur.start.After(last.end) {
			if cur.end.After(last.end) {
				last.end = cur.end
			}
		} else {
			out = append(out, cur)
		}
	}
	return out
}

// jointSegment is a maximal interval during which a fixed set of participants
// is available.
type jointSegment struct {
	start        time.Time
	end          time.Time
	participants []string
}

// sweepJointEvents walks all interval endpoints in chronological order and
// produces maximal segments with a constant, sorted participant set. Events at
// the same instant are applied together so that zero-length transitions never
// appear in the output.
func sweepJointEvents(events []jointEvent, from, to time.Time) []jointSegment {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].at.Equal(events[j].at) {
			return events[i].at.Before(events[j].at)
		}
		if events[i].add != events[j].add {
			// endings before beginnings at the same instant
			return !events[i].add && events[j].add
		}
		return events[i].id < events[j].id
	})

	active := make(map[string]struct{})
	var segments []jointSegment
	curStart := from
	curSet := []string{}

	flush := func(end time.Time) {
		if !end.After(curStart) || len(curSet) == 0 {
			return
		}
		if n := len(segments); n > 0 {
			prev := &segments[n-1]
			if prev.end.Equal(curStart) && sameStrings(prev.participants, curSet) {
				prev.end = end
				return
			}
		}
		segments = append(segments, jointSegment{
			start:        curStart,
			end:          end,
			participants: append([]string(nil), curSet...),
		})
	}

	for i := 0; i < len(events); {
		at := events[i].at
		j := i
		for j < len(events) && events[j].at.Equal(at) {
			j++
		}

		if at.After(curStart) {
			flush(at)
		}

		for _, ev := range events[i:j] {
			if ev.add {
				active[ev.id] = struct{}{}
			} else {
				delete(active, ev.id)
			}
		}
		curStart = at
		curSet = sortedKeys(active)
		i = j
	}
	flush(to)

	return segments
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
