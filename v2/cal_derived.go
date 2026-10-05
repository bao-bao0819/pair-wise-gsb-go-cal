// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrWorkdaySearchExhausted is returned when a workday offset rule cannot
// find the requested workday within its search range.
var ErrWorkdaySearchExhausted = errors.New("workday search exhausted")

// DateKind indicates which date of a referenced rule is used as input.
type DateKind uint8

// Allowed values for DateKind
const (
	DateActual   DateKind = iota // the actual (original) date
	DateObserved                 // the observed (substitute) date
)

// RuleKind identifies the operation performed by a DerivedRule.
type RuleKind uint8

// Allowed values for RuleKind
const (
	RuleLeaf          RuleKind = iota // wraps an existing Holiday as a date source
	RuleDayOffset                     // shifts a source date by civil days
	RuleWorkdayOffset                 // shifts a source date by workdays of a base calendar
	RuleEarliest                      // selects the earliest date among sources
	RuleLatest                        // selects the latest date among sources
)

// Ref identifies another rule in the same DerivedSet by name, along with
// the kind of date to use. Actual and observed dates must be requested
// explicitly; they are never mixed implicitly.
type Ref struct {
	Name string   // name of the referenced rule
	Kind DateKind // which date of the referenced rule to use
}

// DerivedRule is a declarative node in a DerivedSet. Existing Holiday
// values are used as leaves; other rules reference them (and each other)
// by stable names.
type DerivedRule struct {
	Name string   // stable name used by other rules to reference this rule
	Kind RuleKind // the operation performed by this rule

	Holiday *Holiday // RuleLeaf: the holiday providing dates

	Source  Ref   // RuleDayOffset / RuleWorkdayOffset: the input date
	Sources []Ref // RuleEarliest / RuleLatest: the input dates

	Offset int // RuleDayOffset / RuleWorkdayOffset: days to shift (negative moves backward)

	Base       *BusinessCalendar // RuleWorkdayOffset: independent calendar used to count workdays
	SearchDays int               // RuleWorkdayOffset: max civil days to search for workdays (0 = default)
}

// Occurrence is a single derived date together with its source chain.
type Occurrence struct {
	Date  time.Time // the resulting date (start of day)
	Chain []string  // rule names that produced the date, from leaf to the requested rule
}

// DerivedSet is a validated collection of derived holiday rules. Rules are
// checked for missing references and cycles at construction time.
type DerivedSet struct {
	rules     map[string]*DerivedRule
	shift     map[string]int // max absolute day shift each rule can apply
	generated map[*Holiday]string

	mu         sync.Mutex
	evaluating bool
}

type memoKey struct {
	name string
	year int
	kind DateKind
}

type nodeResult struct {
	date  time.Time
	chain []string
}

// NewDerivedSet creates a DerivedSet from the given rules. An error is
// returned if any rule is misconfigured, references an unknown rule, or
// takes part in a reference cycle.
func NewDerivedSet(rules ...*DerivedRule) (*DerivedSet, error) {
	s := &DerivedSet{
		rules:     make(map[string]*DerivedRule, len(rules)),
		shift:     make(map[string]int, len(rules)),
		generated: make(map[*Holiday]string),
	}

	for _, r := range rules {
		if r == nil {
			return nil, errors.New("cal: nil derived rule")
		}
		if r.Name == "" {
			return nil, errors.New("cal: derived rule with empty name")
		}
		if _, dup := s.rules[r.Name]; dup {
			return nil, fmt.Errorf("cal: duplicate derived rule %q", r.Name)
		}
		if err := r.validate(); err != nil {
			return nil, err
		}
		s.rules[r.Name] = r
	}

	for _, r := range rules {
		for _, ref := range r.refs() {
			if _, ok := s.rules[ref.Name]; !ok {
				return nil, fmt.Errorf("cal: derived rule %q references unknown rule %q", r.Name, ref.Name)
			}
		}
	}

	// detect cycles and produce a leaf-first evaluation order
	const (
		white = iota // unvisited
		gray         // on the current path
		black        // fully visited
	)
	state := make(map[string]int, len(rules))
	var order []*DerivedRule
	var visit func(r *DerivedRule) error
	visit = func(r *DerivedRule) error {
		switch state[r.Name] {
		case gray:
			return fmt.Errorf("cal: derived rule %q: cyclic reference", r.Name)
		case black:
			return nil
		}
		state[r.Name] = gray
		for _, ref := range r.refs() {
			if err := visit(s.rules[ref.Name]); err != nil {
				return err
			}
		}
		state[r.Name] = black
		order = append(order, r)
		return nil
	}
	for _, r := range rules {
		if err := visit(r); err != nil {
			return nil, err
		}
	}

	// determine the maximum day shift each rule can apply to its sources
	for _, r := range order {
		switch r.Kind {
		case RuleLeaf:
			s.shift[r.Name] = leafShift(r.Holiday)
		case RuleDayOffset:
			s.shift[r.Name] = s.shift[r.Source.Name] + abs(r.Offset)
		case RuleWorkdayOffset:
			s.shift[r.Name] = s.shift[r.Source.Name] + r.searchDays()
		case RuleEarliest, RuleLatest:
			max := 0
			for _, ref := range r.Sources {
				if s.shift[ref.Name] > max {
					max = s.shift[ref.Name]
				}
			}
			s.shift[r.Name] = max
		}
	}

	return s, nil
}

func (r *DerivedRule) validate() error {
	switch r.Kind {
	case RuleLeaf:
		if r.Holiday == nil {
			return fmt.Errorf("cal: derived rule %q: leaf requires a holiday", r.Name)
		}
	case RuleDayOffset:
		if r.Source.Name == "" {
			return fmt.Errorf("cal: derived rule %q: offset requires a source", r.Name)
		}
	case RuleWorkdayOffset:
		if r.Source.Name == "" {
			return fmt.Errorf("cal: derived rule %q: offset requires a source", r.Name)
		}
		if r.Base == nil {
			return fmt.Errorf("cal: derived rule %q: workday offset requires a base calendar", r.Name)
		}
	case RuleEarliest, RuleLatest:
		if len(r.Sources) == 0 {
			return fmt.Errorf("cal: derived rule %q: selection requires at least one source", r.Name)
		}
	default:
		return fmt.Errorf("cal: derived rule %q: unknown rule kind %d", r.Name, r.Kind)
	}
	for _, ref := range r.refs() {
		if ref.Kind != DateActual && ref.Kind != DateObserved {
			return fmt.Errorf("cal: derived rule %q: reference %q has invalid date kind", r.Name, ref.Name)
		}
	}
	return nil
}

func (r *DerivedRule) refs() []Ref {
	switch r.Kind {
	case RuleDayOffset, RuleWorkdayOffset:
		return []Ref{r.Source}
	case RuleEarliest, RuleLatest:
		return r.Sources
	}
	return nil
}

func (r *DerivedRule) searchDays() int {
	if r.SearchDays > 0 {
		return r.SearchDays
	}
	return 4*abs(r.Offset) + 31
}

func leafShift(h *Holiday) int {
	shift := abs(h.CalcOffset)
	for _, o := range h.Observed {
		if v := abs(h.CalcOffset) + abs(o.Offset); v > shift {
			shift = v
		}
	}
	return shift
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Calc reports all occurrences of the named rule whose dates fall within
// the given year. Source rules are evaluated for adjacent years as needed
// to cover the maximum offset range of the rule, and the results are then
// clipped to the requested year. Occurrences that cross a year boundary
// retain the chain of rules that produced them.
func (s *DerivedSet) Calc(name string, year int) ([]Occurrence, error) {
	rule, ok := s.rules[name]
	if !ok {
		return nil, fmt.Errorf("cal: unknown derived rule %q", name)
	}
	if err := s.beginEval(); err != nil {
		return nil, err
	}
	defer s.endEval()

	extra := s.shift[name]/365 + 1
	memo := make(map[memoKey]nodeResult)
	var out []Occurrence
	for y := year - extra; y <= year+extra; y++ {
		res, err := s.eval(rule, y, DateActual, memo)
		if err != nil {
			return nil, err
		}
		if res.date.IsZero() || res.date.Year() != year {
			continue
		}
		out = append(out, Occurrence{Date: res.date, Chain: res.chain})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// Holiday returns a Holiday whose occurrences are calculated by the named
// rule, allowing a derived rule to be added to a regular Calendar. The
// holiday's function reports the occurrence for the requested base year,
// which may fall in an adjacent year. Calculation errors result in the
// zero time.
//
// The returned holiday must not be added to a base calendar used by a
// workday offset rule of the same set; that would form a hidden cycle and
// is reported as an error during evaluation.
func (s *DerivedSet) Holiday(name string) (*Holiday, error) {
	if _, ok := s.rules[name]; !ok {
		return nil, fmt.Errorf("cal: unknown derived rule %q", name)
	}
	h := &Holiday{
		Name: name,
		Func: func(_ *Holiday, year int) time.Time {
			res, err := s.evalBaseYear(name, year)
			if err != nil {
				return time.Time{}
			}
			return res.date
		},
	}
	s.mu.Lock()
	s.generated[h] = name
	s.mu.Unlock()
	return h, nil
}

// evalBaseYear evaluates a rule for a single base year without clipping.
func (s *DerivedSet) evalBaseYear(name string, year int) (nodeResult, error) {
	if err := s.beginEval(); err != nil {
		return nodeResult{}, err
	}
	defer s.endEval()
	return s.eval(s.rules[name], year, DateActual, make(map[memoKey]nodeResult))
}

func (s *DerivedSet) beginEval() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evaluating {
		return errors.New("cal: recursive derived set evaluation; a base workday calendar must not reference this set")
	}
	for _, r := range s.rules {
		if r.Kind != RuleWorkdayOffset || r.Base == nil {
			continue
		}
		for _, h := range r.Base.Holidays {
			if name, ok := s.generated[h]; ok {
				return fmt.Errorf("cal: base calendar of derived rule %q contains %q, a holiday derived from this set", r.Name, name)
			}
		}
	}
	s.evaluating = true
	return nil
}

func (s *DerivedSet) endEval() {
	s.mu.Lock()
	s.evaluating = false
	s.mu.Unlock()
}

// eval reports the result of a rule for a base year. Results are memoized
// so that each node is calculated at most once per evaluation, even when
// shared by multiple dependents.
func (s *DerivedSet) eval(r *DerivedRule, year int, kind DateKind, memo map[memoKey]nodeResult) (nodeResult, error) {
	key := memoKey{name: r.Name, year: year, kind: kind}
	if res, ok := memo[key]; ok {
		return res, nil
	}
	res, err := s.compute(r, year, kind, memo)
	if err != nil {
		return nodeResult{}, err
	}
	memo[key] = res
	return res, nil
}

func (s *DerivedSet) compute(r *DerivedRule, year int, kind DateKind, memo map[memoKey]nodeResult) (nodeResult, error) {
	switch r.Kind {
	case RuleLeaf:
		act, obs := r.Holiday.Calc(year)
		date := act
		if kind == DateObserved {
			date = obs
		}
		return nodeResult{date: date, chain: []string{r.Name}}, nil

	case RuleDayOffset:
		src, err := s.eval(s.rules[r.Source.Name], year, r.Source.Kind, memo)
		if err != nil {
			return nodeResult{}, err
		}
		if src.date.IsZero() {
			return nodeResult{}, nil
		}
		return nodeResult{date: src.date.AddDate(0, 0, r.Offset), chain: extendChain(src.chain, r.Name)}, nil

	case RuleWorkdayOffset:
		src, err := s.eval(s.rules[r.Source.Name], year, r.Source.Kind, memo)
		if err != nil {
			return nodeResult{}, err
		}
		if src.date.IsZero() {
			return nodeResult{}, nil
		}
		date, err := workdaysFrom(r.Base, src.date, r.Offset, r.searchDays())
		if err != nil {
			return nodeResult{}, fmt.Errorf("cal: derived rule %q: %w", r.Name, err)
		}
		return nodeResult{date: date, chain: extendChain(src.chain, r.Name)}, nil

	case RuleEarliest, RuleLatest:
		var best nodeResult
		for _, ref := range r.Sources {
			src, err := s.eval(s.rules[ref.Name], year, ref.Kind, memo)
			if err != nil {
				return nodeResult{}, err
			}
			if src.date.IsZero() {
				continue
			}
			if best.date.IsZero() ||
				(r.Kind == RuleEarliest && src.date.Before(best.date)) ||
				(r.Kind == RuleLatest && src.date.After(best.date)) {
				best = src
			}
		}
		if best.date.IsZero() {
			return nodeResult{}, nil
		}
		return nodeResult{date: best.date, chain: extendChain(best.chain, r.Name)}, nil
	}
	return nodeResult{}, fmt.Errorf("cal: derived rule %q: unknown rule kind %d", r.Name, r.Kind)
}

func extendChain(chain []string, name string) []string {
	out := make([]string, len(chain)+1)
	copy(out, chain)
	out[len(chain)] = name
	return out
}

// workdaysFrom reports the date that is offset workdays away from start
// according to the base calendar. The search is bounded to searchDays
// civil days; ErrWorkdaySearchExhausted is returned when the requested
// workday cannot be found within that range.
func workdaysFrom(base *BusinessCalendar, start time.Time, offset, searchDays int) (time.Time, error) {
	if offset == 0 {
		return start, nil
	}
	add := 1
	want := offset
	if offset < 0 {
		add = -1
		want = -offset
	}
	date := start
	found := 0
	for i := 0; i < searchDays; i++ {
		date = date.AddDate(0, 0, add)
		if base.IsWorkday(date) {
			found++
			if found == want {
				return date, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("%w: offset %d from %s within %d days",
		ErrWorkdaySearchExhausted, offset, start.Format("2006-01-02"), searchDays)
}
