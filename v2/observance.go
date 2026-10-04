// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"sort"
	"time"
)

// DefaultObservanceSearchLimit is the default maximum number of candidate
// days examined when searching for a substitute observance day.
const DefaultObservanceSearchLimit = 366

// SearchDirection indicates which way to look for a substitute observance
// day when the actual day of a holiday cannot be used.
type SearchDirection int

// Allowed values for SearchDirection
const (
	SearchForward  SearchDirection = iota + 1 // search later dates
	SearchBackward                            // search earlier dates
)

// ObservanceRequest describes a single holiday to include in a batch
// observance plan.
type ObservanceRequest struct {
	ID            string          // stable identifier for the holiday
	Holiday       *Holiday        // the holiday definition (never modified)
	Priority      int             // lower values are scheduled first
	Direction     SearchDirection // search direction for substitute days
	ProtectActual bool            // the actual day cannot be occupied by other observances
}

// RejectCode indicates why a candidate observance day was rejected.
type RejectCode uint8

// Allowed values for RejectCode
const (
	RejectNotWorkday       RejectCode = iota + 1 // candidate is not a workday
	RejectProtectedActual                        // candidate is the protected actual day of another holiday
	RejectOccupiedObserved                       // candidate is already an assigned observance day
)

// String returns a human-readable description of the rejection code.
func (c RejectCode) String() string {
	switch c {
	case RejectNotWorkday:
		return "not a workday"
	case RejectProtectedActual:
		return "protected actual day"
	case RejectOccupiedObserved:
		return "observance day already assigned"
	}
	return "unknown"
}

// ObservanceReason records one rejected candidate day in the search chain
// for a substitute observance day.
type ObservanceReason struct {
	Date time.Time  // the rejected candidate day
	Code RejectCode // why the candidate was rejected
	By   string     // ID of the holiday holding the day, if applicable
}

// ObservanceResult reports the planned observance of one holiday occurrence.
// The actual day is always preserved; only the observed day is planned.
type ObservanceResult struct {
	ID        string             // stable identifier from the request
	Holiday   *Holiday           // the holiday definition (shared, not modified)
	Year      int                // the year the occurrence was calculated for
	Actual    time.Time          // the actual occurrence day
	Observed  time.Time          // the planned observance day (zero if unschedulable)
	Exhausted bool               // true when the search limit was reached without finding a day
	Reasons   []ObservanceReason // chain of rejected candidate days, in search order
}

// Moved reports whether the planned observance day differs from the actual
// day.
func (r *ObservanceResult) Moved() bool {
	return !r.Observed.IsZero() && !r.Observed.Equal(r.Actual)
}

// ObservancePlanner computes batch observance plans for a year. Occurrences
// are scheduled in a deterministic order (priority, then ID, then actual
// day), so the same set of requests produces the same plan regardless of
// registration order.
//
// Candidate observance days must be workdays and cannot be days protected
// by another request (ProtectActual) or days already assigned as an
// observance day. Occurrences from the neighboring years that can affect
// the year's boundaries are included in the same computation.
type ObservancePlanner struct {
	workday     [7]bool   // flags to indicate a day of the week is a workday
	WorkdayFunc WorkdayFn // optional function to override workday flags

	// MaxSearch is the maximum number of candidate days examined per
	// occurrence. If it is not positive, DefaultObservanceSearchLimit is
	// used. The search is always bounded; unschedulable occurrences are
	// reported with Exhausted set instead of looping forever.
	MaxSearch int
}

// NewObservancePlanner creates an ObservancePlanner with work days of
// Monday through Friday and the default search limit.
func NewObservancePlanner() *ObservancePlanner {
	p := &ObservancePlanner{MaxSearch: DefaultObservanceSearchLimit}
	p.workday[time.Monday] = true
	p.workday[time.Tuesday] = true
	p.workday[time.Wednesday] = true
	p.workday[time.Thursday] = true
	p.workday[time.Friday] = true
	return p
}

// SetWorkday changes the given day's status as a standard working day.
func (p *ObservancePlanner) SetWorkday(day time.Weekday, workday bool) {
	p.workday[day] = workday
}

// isWorkday reports whether the given date is a candidate workday.
func (p *ObservancePlanner) isWorkday(date time.Time) bool {
	if p.WorkdayFunc != nil {
		return p.WorkdayFunc(date)
	}
	return p.workday[date.Weekday()]
}

// observanceOccurrence is a single occurrence of a requested holiday in a
// specific calculation year.
type observanceOccurrence struct {
	req    *ObservanceRequest
	year   int
	actual time.Time
}

// dateKey identifies a day for occupancy tracking.
type dateKey struct {
	year  int
	month time.Month
	day   int
}

func makeDateKey(t time.Time) dateKey {
	y, m, d := t.Date()
	return dateKey{year: y, month: m, day: d}
}

// Plan computes the observance days for the occurrences of the requested
// holidays in the given year. The holiday definitions are never modified;
// all results are returned as new ObservanceResult values.
//
// Occurrences calculated for the neighboring years are included in the
// computation when their actual day is close enough to the year's boundary
// to influence it, but only results for the requested year are returned.
// Results are ordered by priority, then ID, then actual day.
func (p *ObservancePlanner) Plan(year int, reqs ...ObservanceRequest) []ObservanceResult {
	maxSearch := p.MaxSearch
	if maxSearch <= 0 {
		maxSearch = DefaultObservanceSearchLimit
	}

	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, DefaultLoc)
	yearEnd := time.Date(year, time.December, 31, 0, 0, 0, 0, DefaultLoc)

	// collect occurrences for the requested year plus neighboring-year
	// occurrences whose actual day is close enough to a boundary to
	// influence the plan
	var occs []observanceOccurrence
	for i := range reqs {
		req := &reqs[i]
		if req.Holiday == nil {
			continue
		}
		for _, y := range []int{year - 1, year, year + 1} {
			actual, _ := req.Holiday.Calc(y)
			if actual.IsZero() {
				continue
			}
			if y != year {
				lo := yearStart.AddDate(0, 0, -maxSearch)
				hi := yearEnd.AddDate(0, 0, maxSearch)
				if actual.Before(lo) || actual.After(hi) {
					continue
				}
			}
			occs = append(occs, observanceOccurrence{req: req, year: y, actual: actual})
		}
	}

	// deterministic scheduling order: priority, then ID, then actual day
	sort.SliceStable(occs, func(i, j int) bool {
		if occs[i].req.Priority != occs[j].req.Priority {
			return occs[i].req.Priority < occs[j].req.Priority
		}
		if occs[i].req.ID != occs[j].req.ID {
			return occs[i].req.ID < occs[j].req.ID
		}
		return occs[i].actual.Before(occs[j].actual)
	})

	// protect the actual days of holidays configured as non-occupiable
	// before any substitute days are assigned
	protected := make(map[dateKey]int)
	for i := range occs {
		if occs[i].req.ProtectActual {
			protected[makeDateKey(occs[i].actual)] = i
		}
	}

	assigned := make(map[dateKey]string)
	var results []ObservanceResult

	for i := range occs {
		o := &occs[i]
		res := ObservanceResult{
			ID:      o.req.ID,
			Holiday: o.req.Holiday,
			Year:    o.year,
			Actual:  o.actual,
		}

		step := 1
		if o.req.Direction == SearchBackward {
			step = -1
		}

		candidate := o.actual
		for n := 0; n <= maxSearch; n++ {
			key := makeDateKey(candidate)
			var code RejectCode
			var by string
			switch {
			case !p.isWorkday(candidate):
				code = RejectNotWorkday
			default:
				if pi, ok := protected[key]; ok && pi != i {
					code = RejectProtectedActual
					by = occs[pi].req.ID
				} else if id, ok := assigned[key]; ok {
					code = RejectOccupiedObserved
					by = id
				}
			}

			if code == 0 {
				res.Observed = candidate
				assigned[key] = o.req.ID
				break
			}
			res.Reasons = append(res.Reasons, ObservanceReason{Date: candidate, Code: code, By: by})
			candidate = candidate.AddDate(0, 0, step)
		}

		if res.Observed.IsZero() {
			res.Exhausted = true
		}
		if o.year == year {
			results = append(results, res)
		}
	}

	return results
}
