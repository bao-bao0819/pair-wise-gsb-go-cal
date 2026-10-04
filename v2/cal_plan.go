package cal

import (
	"fmt"
	"sort"
	"time"
)

// DefaultPlanMaxSearch is the default maximum number of days that
// PlanObservances will search in each direction when looking for an
// available observation day.
const DefaultPlanMaxSearch = 31

// SearchDirection indicates which direction PlanObservances searches for an
// available observation day when the default day is unavailable.
type SearchDirection int

// Allowed values for SearchDirection
const (
	SearchForward  SearchDirection = iota // search later dates
	SearchBackward                        // search earlier dates
	SearchNearest                         // search outward from the default day, earlier dates first
)

// PlanEntry describes a single holiday to include in an observation plan.
type PlanEntry struct {
	ID        string          // stable identifier; must be unique and non-empty
	Holiday   *Holiday        // the holiday definition (never modified by planning)
	Priority  int             // scheduling order; lower values are placed first
	Direction SearchDirection // direction to search when the default day is unavailable
	Protected bool            // the actual day may not be taken by another holiday's observation
}

// PlanOptions configures the behavior of PlanObservances.
type PlanOptions struct {
	// WorkWeek indicates which weekdays are eligible observation days,
	// indexed by time.Weekday. The zero value means Monday-Friday.
	WorkWeek [7]bool

	// MaxSearch is the maximum number of days to search in each direction
	// for an available observation day. Values <= 0 use DefaultPlanMaxSearch.
	MaxSearch int
}

// Observation records the planned observance of a single holiday occurrence.
type Observation struct {
	ID       string    // the PlanEntry ID
	Holiday  *Holiday  // the holiday definition that was planned
	Actual   time.Time // the actual day of the holiday (always preserved)
	Observed time.Time // the planned observation day
	Shifted  bool      // true if Observed differs from the holiday's default observed day
	Reasons  []string  // reason chain explaining each rejected day when Shifted is true
}

// ObservancePlan is the result of planning holiday observations for a year.
type ObservancePlan struct {
	Year         int            // the plan year
	Observations []*Observation // planned observations, ordered by actual date then ID
}

// Observation returns the planned observation for the given entry ID, or nil
// if there is none.
func (p *ObservancePlan) Observation(id string) *Observation {
	for _, o := range p.Observations {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// PlanError is returned by PlanObservances when no available observation day
// can be found for a holiday within the search limit.
type PlanError struct {
	ID      string   // the PlanEntry ID that could not be placed
	Limit   int      // the search limit that was reached
	Reasons []string // reason chain for every day that was rejected
}

// Error implements the error interface.
func (e *PlanError) Error() string {
	return fmt.Sprintf("cal: no observation day available for %q within search limit of %d days", e.ID, e.Limit)
}

// planInstance is a single occurrence of a PlanEntry in a specific year.
type planInstance struct {
	entry    *PlanEntry
	year     int       // the year of the actual occurrence
	actual   time.Time // the actual day
	obs      time.Time // the default observed day from Holiday.Calc
	boundary bool      // true if the occurrence belongs to an adjacent year
}

// planOccupant records which instance reserved a day and why.
type planOccupant struct {
	inst *planInstance
	kind string // "protected actual day" or "observed day"
}

// PlanObservances plans observation days for a set of holidays in the given
// year. It is intended for calendars where two holidays observed on the same
// day require one of them to be deferred to another workday.
//
// Actual holiday dates are always preserved; planning only determines
// observation days. The holiday definitions themselves are never modified,
// and Holiday.Calc behavior is unaffected.
//
// Planning proceeds as follows:
//
//  1. Occurrences are collected for the plan year. Occurrences from adjacent
//     years whose observation day could cross the year boundary are included
//     in the same calculation so that boundary conflicts are resolved
//     consistently, but they are not reported in the plan.
//  2. The actual days of entries marked Protected are reserved so that no
//     other holiday may be observed on them.
//  3. Observation days are assigned in explicit order: by Priority (lower
//     first), then by ID. The default observed day from Holiday.Calc is used
//     when it is a workday and unoccupied; otherwise the nearest available
//     workday is found by searching in the entry's Direction, up to the
//     search limit.
//
// The result is deterministic: registering the same entries in a different
// order produces the same plan. If no day is available for an entry within
// the search limit, a *PlanError is returned instead of looping forever.
func PlanObservances(year int, entries []PlanEntry, opts *PlanOptions) (*ObservancePlan, error) {
	workWeek := [7]bool{}
	maxSearch := DefaultPlanMaxSearch
	if opts != nil {
		workWeek = opts.WorkWeek
		if opts.MaxSearch > 0 {
			maxSearch = opts.MaxSearch
		}
	}
	hasWorkday := false
	for _, ok := range workWeek {
		if ok {
			hasWorkday = true
			break
		}
	}
	if !hasWorkday {
		workWeek = [7]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true,
			time.Thursday: true, time.Friday: true,
		}
	}

	seen := make(map[string]bool, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.ID == "" {
			return nil, fmt.Errorf("cal: plan entry ID must not be empty")
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("cal: duplicate plan entry ID %q", e.ID)
		}
		seen[e.ID] = true
		if e.Holiday == nil {
			return nil, fmt.Errorf("cal: plan entry %q has a nil Holiday", e.ID)
		}
	}

	yearStart := time.Date(year, time.January, 1, 0, 0, 0, 0, DefaultLoc)
	yearEnd := time.Date(year, time.December, 31, 0, 0, 0, 0, DefaultLoc)

	var instances []*planInstance
	for i := range entries {
		e := &entries[i]
		for y := year - 1; y <= year+1; y++ {
			act, obs := e.Holiday.Calc(y)
			if act.IsZero() {
				continue
			}
			inst := &planInstance{entry: e, year: y, actual: act, obs: obs, boundary: y != year}
			if inst.boundary {
				lo := MinTime(act, obs).AddDate(0, 0, -maxSearch)
				hi := MaxTime(act, obs).AddDate(0, 0, maxSearch)
				if hi.Before(yearStart) || lo.After(yearEnd) {
					continue
				}
			}
			instances = append(instances, inst)
		}
	}

	sort.Slice(instances, func(i, j int) bool {
		a, b := instances[i], instances[j]
		if a.entry.Priority != b.entry.Priority {
			return a.entry.Priority < b.entry.Priority
		}
		if a.entry.ID != b.entry.ID {
			return a.entry.ID < b.entry.ID
		}
		if !a.actual.Equal(b.actual) {
			return a.actual.Before(b.actual)
		}
		return a.year < b.year
	})

	keyOf := func(t time.Time) holCacheKey {
		y, m, d := t.Date()
		return holCacheKey{year: y, month: m, day: d}
	}
	occupied := make(map[holCacheKey]*planOccupant)

	// reserve the actual days of protected entries before any scheduling
	for _, inst := range instances {
		if inst.entry.Protected {
			occupied[keyOf(inst.actual)] = &planOccupant{inst: inst, kind: "protected actual day"}
		}
	}

	unavailable := func(inst *planInstance, day time.Time) string {
		if !workWeek[day.Weekday()] {
			return "not a workday"
		}
		occ, ok := occupied[keyOf(day)]
		if !ok || occ.inst == inst {
			return ""
		}
		if occ.kind == "protected actual day" && inst.entry.Protected && keyOf(inst.actual) == keyOf(day) {
			// two protected holidays may share the same actual day
			return ""
		}
		return fmt.Sprintf("conflicts with %s of %q", occ.kind, occ.inst.entry.ID)
	}

	formatDay := func(t time.Time) string {
		return fmt.Sprintf("%s (%s)", t.Format("2006-01-02"), t.Weekday())
	}

	plan := &ObservancePlan{Year: year}
	for _, inst := range instances {
		var reasons []string
		var placed time.Time

		if why := unavailable(inst, inst.obs); why == "" {
			placed = inst.obs
		} else {
			reasons = append(reasons, fmt.Sprintf("%s unavailable: %s", formatDay(inst.obs), why))
			for offset := 1; offset <= maxSearch && placed.IsZero(); offset++ {
				var candidates []time.Time
				switch inst.entry.Direction {
				case SearchBackward:
					candidates = []time.Time{inst.obs.AddDate(0, 0, -offset)}
				case SearchNearest:
					candidates = []time.Time{inst.obs.AddDate(0, 0, -offset), inst.obs.AddDate(0, 0, offset)}
				default:
					candidates = []time.Time{inst.obs.AddDate(0, 0, offset)}
				}
				for _, cand := range candidates {
					if why := unavailable(inst, cand); why != "" {
						reasons = append(reasons, fmt.Sprintf("%s unavailable: %s", formatDay(cand), why))
						continue
					}
					placed = cand
					break
				}
			}
		}

		if placed.IsZero() {
			return nil, &PlanError{ID: inst.entry.ID, Limit: maxSearch, Reasons: reasons}
		}
		if !placed.Equal(inst.obs) {
			reasons = append(reasons, fmt.Sprintf("%s selected", formatDay(placed)))
		}
		occupied[keyOf(placed)] = &planOccupant{inst: inst, kind: "observed day"}

		if !inst.boundary {
			plan.Observations = append(plan.Observations, &Observation{
				ID:       inst.entry.ID,
				Holiday:  inst.entry.Holiday,
				Actual:   inst.actual,
				Observed: placed,
				Shifted:  !placed.Equal(inst.obs),
				Reasons:  reasons,
			})
		}
	}

	sort.Slice(plan.Observations, func(i, j int) bool {
		a, b := plan.Observations[i], plan.Observations[j]
		if !a.Actual.Equal(b.Actual) {
			return a.Actual.Before(b.Actual)
		}
		return a.ID < b.ID
	})

	return plan, nil
}
