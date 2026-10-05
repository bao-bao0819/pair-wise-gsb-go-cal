package cal

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SourceKind selects which date of a source rule is consumed by a derived
// rule. The actual and observed dates of a source are referenced by
// distinct operations and are never mixed implicitly.
type SourceKind uint8

// Allowed values for SourceKind
const (
	SourceActual   SourceKind = iota + 1 // the actual (original) date of the source
	SourceObserved                       // the observed date of the source
)

// DerivedRef references another rule in a DerivedSet by its stable name.
type DerivedRef struct {
	Name string     // name of the referenced rule
	Kind SourceKind // which date of the referenced rule to use
}

// ActualRef references the actual (original) date of the named rule.
func ActualRef(name string) DerivedRef { return DerivedRef{Name: name, Kind: SourceActual} }

// ObservedRef references the observed date of the named rule.
func ObservedRef(name string) DerivedRef { return DerivedRef{Name: name, Kind: SourceObserved} }

// DerivedOp identifies the operation performed by a DerivedRule.
type DerivedOp uint8

// Allowed values for DerivedOp
const (
	DerivedLeaf          DerivedOp = iota + 1 // wraps an existing Holiday
	DerivedDayOffset                          // civil (calendar) day offset from a source
	DerivedWorkdayOffset                      // workday offset from a source using the base calendar
	DerivedEarliest                           // earliest date among the sources
	DerivedLatest                             // latest date among the sources
)

// DerivedRule is a single named node in a DerivedSet. Use the rule
// constructors (LeafRule, DayOffsetRule, WorkdayOffsetRule, EarliestRule,
// LatestRule) to create rules.
type DerivedRule struct {
	Name        string         // stable name used by other rules to reference this rule
	Description string         // further details/notes
	Type        ObservanceType // type of day being observed

	Op      DerivedOp    // operation performed by this rule
	Holiday *Holiday     // source holiday (DerivedLeaf)
	Source  DerivedRef   // source reference (DerivedDayOffset, DerivedWorkdayOffset)
	Offset  int          // day or workday offset (DerivedDayOffset, DerivedWorkdayOffset)
	Sources []DerivedRef // source references (DerivedEarliest, DerivedLatest)
}

// LeafRule creates a rule that wraps an existing Holiday as a leaf node.
func LeafRule(name string, h *Holiday) DerivedRule {
	return DerivedRule{Name: name, Op: DerivedLeaf, Holiday: h}
}

// DayOffsetRule creates a rule that offsets the source date by the given
// number of civil (calendar) days.
func DayOffsetRule(name string, src DerivedRef, days int) DerivedRule {
	return DerivedRule{Name: name, Op: DerivedDayOffset, Source: src, Offset: days}
}

// WorkdayOffsetRule creates a rule that offsets the source date by the
// given number of workdays according to the set's base business calendar.
func WorkdayOffsetRule(name string, src DerivedRef, workdays int) DerivedRule {
	return DerivedRule{Name: name, Op: DerivedWorkdayOffset, Source: src, Offset: workdays}
}

// EarliestRule creates a rule that selects the earliest date among its
// sources.
func EarliestRule(name string, srcs ...DerivedRef) DerivedRule {
	return DerivedRule{Name: name, Op: DerivedEarliest, Sources: srcs}
}

// LatestRule creates a rule that selects the latest date among its sources.
func LatestRule(name string, srcs ...DerivedRef) DerivedRule {
	return DerivedRule{Name: name, Op: DerivedLatest, Sources: srcs}
}

// refs returns the references consumed by the rule.
func (r *DerivedRule) refs() []DerivedRef {
	switch r.Op {
	case DerivedDayOffset, DerivedWorkdayOffset:
		return []DerivedRef{r.Source}
	case DerivedEarliest, DerivedLatest:
		return r.Sources
	}
	return nil
}

// workdaySpanFactor is the conservative number of civil days a single
// workday may contribute when estimating how far a result can drift from
// its source year.
const workdaySpanFactor = 3

// DefaultDerivedSearchLimit is the default maximum number of days scanned
// when resolving a single workday offset.
const DefaultDerivedSearchLimit = 366 * 3

// DerivedSet is a declarative collection of holiday rules derived from one
// another. Existing Holiday values are attached as leaves; other rules
// reference them (and each other) by stable names.
//
// The base business calendar is used only to resolve workday offsets. It
// must be an independent calendar: it must not contain holidays that
// evaluate this set, otherwise evaluation would recurse. Such hidden
// cycles are detected at evaluation time and reported as an error.
//
// A DerivedSet is not safe for concurrent evaluation.
type DerivedSet struct {
	// SearchLimit bounds the number of days scanned when resolving a
	// single workday offset. If the limit is exceeded, evaluation stops
	// and returns the reason instead of searching indefinitely.
	SearchLimit int

	base       *BusinessCalendar
	rules      map[string]*DerivedRule
	names      []string
	padYears   int
	evaluating bool
}

// NewDerivedSet creates a DerivedSet from the given rules. The base
// calendar is required if any rule uses a workday offset; it may be nil
// otherwise.
//
// The rules are validated at construction time: duplicate names, missing
// references, invalid references, and dependency cycles are reported as
// errors.
func NewDerivedSet(base *BusinessCalendar, rules ...DerivedRule) (*DerivedSet, error) {
	s := &DerivedSet{
		SearchLimit: DefaultDerivedSearchLimit,
		base:        base,
		rules:       make(map[string]*DerivedRule, len(rules)),
	}

	for i := range rules {
		r := rules[i]
		if r.Name == "" {
			return nil, errors.New("cal: derived rule with empty name")
		}
		if _, ok := s.rules[r.Name]; ok {
			return nil, fmt.Errorf("cal: duplicate derived rule name %q", r.Name)
		}
		switch r.Op {
		case DerivedLeaf:
			if r.Holiday == nil {
				return nil, fmt.Errorf("cal: derived rule %q: leaf rule requires a holiday", r.Name)
			}
		case DerivedDayOffset, DerivedWorkdayOffset:
			if err := validateDerivedRef(r.Source); err != nil {
				return nil, fmt.Errorf("cal: derived rule %q: %v", r.Name, err)
			}
			if r.Op == DerivedWorkdayOffset && base == nil {
				return nil, fmt.Errorf("cal: derived rule %q: workday offset requires a base calendar", r.Name)
			}
		case DerivedEarliest, DerivedLatest:
			if len(r.Sources) == 0 {
				return nil, fmt.Errorf("cal: derived rule %q: requires at least one source", r.Name)
			}
			for _, ref := range r.Sources {
				if err := validateDerivedRef(ref); err != nil {
					return nil, fmt.Errorf("cal: derived rule %q: %v", r.Name, err)
				}
			}
		default:
			return nil, fmt.Errorf("cal: derived rule %q: unknown operation", r.Name)
		}
		s.rules[r.Name] = &r
		s.names = append(s.names, r.Name)
	}

	for _, name := range s.names {
		for _, ref := range s.rules[name].refs() {
			if _, ok := s.rules[ref.Name]; !ok {
				return nil, fmt.Errorf("cal: derived rule %q references unknown rule %q", name, ref.Name)
			}
		}
	}

	spans := make(map[string]int, len(s.names))
	maxSpan := 0
	for _, name := range s.names {
		span, err := s.span(name, make(map[string]bool), spans, nil)
		if err != nil {
			return nil, err
		}
		if span > maxSpan {
			maxSpan = span
		}
	}
	if maxSpan > 0 {
		s.padYears = maxSpan/366 + 1
	}

	return s, nil
}

func validateDerivedRef(ref DerivedRef) error {
	if ref.Name == "" {
		return errors.New("reference with empty name")
	}
	if ref.Kind != SourceActual && ref.Kind != SourceObserved {
		return fmt.Errorf("reference %q has invalid source kind", ref.Name)
	}
	return nil
}

// span returns the maximum number of days the rule's result can drift from
// its source-year dates, accumulating offsets along the dependency chain.
// Cycles are reported as errors.
func (s *DerivedSet) span(name string, visiting map[string]bool, spans map[string]int, path []string) (int, error) {
	if sp, ok := spans[name]; ok {
		return sp, nil
	}
	if visiting[name] {
		cycle := append(append([]string{}, path...), name)
		return 0, fmt.Errorf("cal: derived rule cycle detected: %s", strings.Join(cycle, " -> "))
	}
	visiting[name] = true

	rule := s.rules[name]
	span := 0
	for _, ref := range rule.refs() {
		sub, err := s.span(ref.Name, visiting, spans, append(path, name))
		if err != nil {
			return 0, err
		}
		if sub > span {
			span = sub
		}
	}
	switch rule.Op {
	case DerivedDayOffset:
		span += absInt(rule.Offset)
	case DerivedWorkdayOffset:
		span += absInt(rule.Offset) * workdaySpanFactor
	}

	visiting[name] = false
	spans[name] = span
	return span, nil
}

// DerivedStep records one step in the source chain of a DerivedResult.
type DerivedStep struct {
	Name string    // rule name
	Year int       // source year the rule was evaluated for
	Date time.Time // date produced by the rule for that year
}

// DerivedResult is the outcome of evaluating a single rule for a source
// year. Results computed from an adjacent source year that land inside the
// requested year keep their full source chain in Trace.
type DerivedResult struct {
	Rule  *DerivedRule  // the rule that produced the result
	Year  int           // source year the rule was evaluated for
	Date  time.Time     // resulting date (start of day)
	Trace []DerivedStep // source chain from the leaf to this result
}

// Eval evaluates every rule in the set for the given year. Sources are
// evaluated for adjacent years as well, padded by the maximum offset range
// of the rule graph, and the results are then clipped to the requested
// year. Each node is computed at most once per source year within a single
// evaluation, so shared leaves are not evaluated repeatedly.
//
// Results are sorted by date, then by rule name.
func (s *DerivedSet) Eval(year int) ([]*DerivedResult, error) {
	if s.evaluating {
		return nil, errors.New("cal: derived set evaluated recursively; the base calendar must not depend on the derived set")
	}
	s.evaluating = true
	defer func() { s.evaluating = false }()

	e := &derivedEval{set: s, memo: make(map[derivedKey]*derivedValue)}
	start := time.Date(year, time.January, 1, 0, 0, 0, 0, DefaultLoc)
	end := time.Date(year+1, time.January, 1, 0, 0, 0, 0, DefaultLoc)

	var results []*DerivedResult
	for _, name := range s.names {
		for y := year - s.padYears; y <= year+s.padYears; y++ {
			v, err := e.eval(name, y)
			if err != nil {
				return nil, err
			}
			if v.actual.IsZero() || v.actual.Before(start) || !v.actual.Before(end) {
				continue
			}
			results = append(results, &DerivedResult{
				Rule:  s.rules[name],
				Year:  y,
				Date:  v.actual,
				Trace: v.actualTrace,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Date.Equal(results[j].Date) {
			return results[i].Rule.Name < results[j].Rule.Name
		}
		return results[i].Date.Before(results[j].Date)
	})
	return results, nil
}

// workdaysFrom reports the date that is offset workdays away from start
// according to the base calendar. The search is bounded by SearchLimit;
// if no workday is found within the limit, an error is returned instead of
// searching indefinitely.
func (s *DerivedSet) workdaysFrom(start time.Time, offset int) (time.Time, error) {
	if offset == 0 {
		return start, nil
	}
	add := 1
	if offset < 0 {
		add = -1
		offset = -offset
	}

	date := start
	searched := 0
	for ndays := 0; ndays < offset; {
		date = date.AddDate(0, 0, add)
		searched++
		if searched > s.SearchLimit {
			return time.Time{}, fmt.Errorf("workday offset %d not found within search limit of %d days", offset, s.SearchLimit)
		}
		if s.base.IsWorkday(date) {
			ndays++
		}
	}
	return date, nil
}

type derivedKey struct {
	name string
	year int
}

type derivedValue struct {
	actual        time.Time
	observed      time.Time
	actualTrace   []DerivedStep
	observedTrace []DerivedStep
}

type derivedEval struct {
	set  *DerivedSet
	memo map[derivedKey]*derivedValue
}

func (e *derivedEval) eval(name string, year int) (*derivedValue, error) {
	key := derivedKey{name: name, year: year}
	if v, ok := e.memo[key]; ok {
		return v, nil
	}

	rule := e.set.rules[name]
	v := &derivedValue{}

	switch rule.Op {
	case DerivedLeaf:
		act, obs := rule.Holiday.Calc(year)
		v.actual = act
		v.observed = obs
		if !act.IsZero() {
			v.actualTrace = []DerivedStep{{Name: name, Year: year, Date: act}}
		}
		if !obs.IsZero() {
			v.observedTrace = []DerivedStep{{Name: name, Year: year, Date: obs}}
		}

	case DerivedDayOffset, DerivedWorkdayOffset:
		src, err := e.eval(rule.Source.Name, year)
		if err != nil {
			return nil, err
		}
		date, trace := selectDerivedSource(src, rule.Source.Kind)
		if !date.IsZero() {
			var out time.Time
			if rule.Op == DerivedDayOffset {
				out = date.AddDate(0, 0, rule.Offset)
			} else {
				out, err = e.set.workdaysFrom(date, rule.Offset)
				if err != nil {
					return nil, fmt.Errorf("cal: derived rule %q: %v", name, err)
				}
			}
			step := DerivedStep{Name: name, Year: year, Date: out}
			v.actual = out
			v.observed = out
			v.actualTrace = appendDerivedTrace(trace, step)
			v.observedTrace = appendDerivedTrace(trace, step)
		}

	case DerivedEarliest, DerivedLatest:
		best := time.Time{}
		var bestTrace []DerivedStep
		for _, ref := range rule.Sources {
			src, err := e.eval(ref.Name, year)
			if err != nil {
				return nil, err
			}
			date, trace := selectDerivedSource(src, ref.Kind)
			if date.IsZero() {
				continue
			}
			if best.IsZero() ||
				(rule.Op == DerivedEarliest && date.Before(best)) ||
				(rule.Op == DerivedLatest && date.After(best)) {
				best = date
				bestTrace = trace
			}
		}
		if !best.IsZero() {
			step := DerivedStep{Name: name, Year: year, Date: best}
			v.actual = best
			v.observed = best
			v.actualTrace = appendDerivedTrace(bestTrace, step)
			v.observedTrace = appendDerivedTrace(bestTrace, step)
		}
	}

	e.memo[key] = v
	return v, nil
}

func selectDerivedSource(v *derivedValue, kind SourceKind) (time.Time, []DerivedStep) {
	if kind == SourceObserved {
		return v.observed, v.observedTrace
	}
	return v.actual, v.actualTrace
}

func appendDerivedTrace(trace []DerivedStep, step DerivedStep) []DerivedStep {
	out := make([]DerivedStep, len(trace)+1)
	copy(out, trace)
	out[len(trace)] = step
	return out
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
