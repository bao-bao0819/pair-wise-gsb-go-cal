// (c) Rick Arnold. Licensed under the BSD license (see LICENSE).

package cal

import "time"

// localBoundary describes how a nominal local wall-clock time maps to an
// instant.
type localBoundaryKind uint8

const (
	boundaryUnique localBoundaryKind = iota // maps to exactly one instant
	boundaryGap                             // local time does not exist (spring forward)
	boundaryFold                            // local time occurs twice (fall back)
)

// localBoundary is the result of resolving a local wall-clock time.
//
// For a unique instant Instant is set. For a gap Instant is the single
// transition instant (both gap policies converge there). For a fold Instant is
// the first occurrence and Second is the later occurrence.
type localBoundary struct {
	First  time.Time
	Second time.Time
	Kind   localBoundaryKind
}

// anchor is a verified valid instant used to reconstruct instants that share
// its zone offset. base is the (possibly fictional) local midnight expressed
// as a Unix second under that constant offset.
type anchor struct {
	unix  int64
	sod   int64 // seconds after local midnight of the sample
	delta int   // sample date offset in days from the nominal day
	off   int   // eastward UTC offset in seconds
}

// resolveWindow converts one configured local window attributed to day into a
// half-open interval of instants.
func (c *BusinessCalendar) resolveWindow(day time.Time, w LocalWindow) (WorkInterval, bool, error) {
	start, skipped, err := c.resolveBoundary(day, 0, w.Start)
	if err != nil || skipped {
		return WorkInterval{}, skipped, err
	}

	endDelta := w.EndDayOffset
	endSod := w.End
	if endDelta == 0 && endSod == 24*time.Hour {
		// 24:00 is the following midnight, not an out-of-range time of day.
		endDelta = 1
		endSod = 0
	}
	end, skipped, err := c.resolveBoundary(day, endDelta, endSod)
	if err != nil || skipped {
		return WorkInterval{}, skipped, err
	}

	if !start.Before(end) {
		if c.dstPolicy == AmbiguousSkip {
			return WorkInterval{}, true, nil
		}
		return WorkInterval{}, false, ErrZeroWindow
	}
	return WorkInterval{Start: start, End: end}, false, nil
}

// resolveBoundary converts the local clock time sod on day plus dateDelta days
// into an instant, applying the calendar's DST policy.
func (c *BusinessCalendar) resolveBoundary(day time.Time, dateDelta int, sod time.Duration) (time.Time, bool, error) {
	y, m, d := day.Date()
	b := resolveLocal(c.loc, y, m, d, dateDelta, sod)

	switch b.Kind {
	case boundaryUnique:
		return b.First, false, nil

	case boundaryGap:
		switch c.dstPolicy {
		case AmbiguousError:
			return time.Time{}, false, ErrAmbiguousTime
		case AmbiguousSkip:
			return time.Time{}, true, nil
		default:
			// Earlier and later both coincide with the transition instant.
			return b.First, false, nil
		}

	default: // boundaryFold
		switch c.dstPolicy {
		case AmbiguousError:
			return time.Time{}, false, ErrAmbiguousTime
		case AmbiguousSkip:
			return time.Time{}, true, nil
		case AmbiguousLater:
			return b.Second, false, nil
		default:
			return b.First, false, nil
		}
	}
}

// resolveLocal maps a nominal local wall-clock time (the given date plus
// dateDelta days and sod since midnight) to one or two instants.
//
// It never relies on time.Date's normalization of missing/duplicate times.
// Instead it samples several definitely-valid wall times, reconstructs the
// candidate instant under each observed zone offset, and round-trips each
// candidate to prove which instants actually carry the requested local time.
func resolveLocal(loc *time.Location, y int, m time.Month, d, dateDelta int, sod time.Duration) localBoundary {
	targetDate := time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, dateDelta)
	ty, tm, td := targetDate.Date()
	targetSod := int64(sod / time.Second)
	targetNsec := int64(sod % time.Second)

	anchors := collectAnchors(loc, y, m, d, dateDelta)

	var candidates []time.Time
	seen := make(map[int64]bool)
	for _, a := range anchors {
		// Local midnight of the sample's date expressed under the sample's
		// constant offset, then walk to the target date/time. Round-trip
		// verification decides whether the reconstructed instant is real.
		midnight := a.unix - a.sod
		unix := midnight + int64(dateDelta-a.delta)*86400 + targetSod
		t := time.Unix(unix, targetNsec).In(loc)
		// The candidate must keep the anchor's offset; time.Time can otherwise
		// normalize a non-existent gap time into the neighboring zone while
		// preserving the wall clock.
		if _, off := t.Zone(); off != a.off {
			continue
		}
		if !carriesLocal(t, ty, tm, td, targetSod, targetNsec) {
			continue
		}
		if !seen[unix] {
			seen[unix] = true
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 1 {
		return localBoundary{First: candidates[0], Kind: boundaryUnique}
	}
	if len(candidates) >= 2 {
		sortTimes(candidates)
		return localBoundary{First: candidates[0], Second: candidates[len(candidates)-1], Kind: boundaryFold}
	}

	// No instant carries the requested local time: a spring-forward gap. The
	// transition instant separates the before and after offsets.
	transition := gapTransition(loc, ty, tm, td)
	if transition.IsZero() {
		// Defensive fallback; treat as unique using time.Date normalization.
		return localBoundary{
			First: time.Date(ty, tm, td, int(targetSod/3600), int((targetSod%3600)/60),
				int(targetSod%60), int(targetNsec), loc),
			Kind: boundaryUnique,
		}
	}
	return localBoundary{First: transition, Kind: boundaryGap}
}

// collectAnchors samples wall times that cannot themselves straddle a
// transition and records one verified anchor per offset in effect around the
// target date. Samples straddle the target date on both sides so a transition
// exactly at midnight still exposes both offsets.
func collectAnchors(loc *time.Location, y int, m time.Month, d, targetDelta int) []anchor {
	samples := []struct {
		offsetFromTarget int
		sod              int64
	}{
		{-1, 23*3600 + 30*60}, // late evening the day before the target
		{0, 30 * 60},
		{0, 12*3600 + 30*60},
		{0, 23*3600 + 30*60},
		{1, 30 * 60}, // early morning the day after the target
	}

	byOffset := make(map[int]anchor)
	for _, s := range samples {
		dayDelta := targetDelta + s.offsetFromTarget
		date := time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, dayDelta)
		cy, cm, cd := date.Date()
		t := time.Date(cy, cm, cd, int(s.sod/3600), int((s.sod%3600)/60), 0, 0, loc)
		if !carriesLocal(t, cy, cm, cd, s.sod, 0) {
			continue
		}
		_, off := t.Zone()
		byOffset[off] = anchor{unix: t.Unix(), sod: s.sod, delta: dayDelta, off: off}
	}

	anchors := make([]anchor, 0, len(byOffset))
	for _, a := range byOffset {
		anchors = append(anchors, a)
	}
	sortAnchors(anchors)
	return anchors
}

// carriesLocal reports whether t presents exactly the requested local date and
// time of day in loc.
func carriesLocal(t time.Time, y int, m time.Month, d int, sod, nsec int64) bool {
	ty, tm, td := t.Date()
	h, mi, s := t.Clock()
	got := int64(h)*3600 + int64(mi)*60 + int64(s)
	return ty == y && tm == m && td == d && got == sod && int64(t.Nanosecond()) == nsec
}

// gapTransition locates the spring-forward transition instant on the nominal
// day by binary searching for the first second at which the offset that is in
// effect late in the day begins to apply.
func gapTransition(loc *time.Location, y int, m time.Month, d int) time.Time {
	date := time.Date(y, m, d, 0, 0, 0, 0, loc)
	early := time.Date(y, m, d, 0, 30, 0, 0, loc)
	late := time.Date(y, m, d, 23, 30, 0, 0, loc)
	_, earlyOff := early.Zone()
	_, afterOff := late.Zone()
	if earlyOff == afterOff {
		return time.Time{}
	}

	dayStart := date.Unix()
	dayEnd := date.AddDate(0, 0, 1).Unix()

	lo, hi := dayStart, dayEnd
	for lo < hi {
		mid := lo + (hi-lo)/2
		_, off := time.Unix(mid, 0).In(loc).Zone()
		if off == afterOff {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo >= dayEnd {
		return time.Time{}
	}
	return time.Unix(lo, 0).In(loc)
}

func sortTimes(ts []time.Time) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

func sortAnchors(a []anchor) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j].unix < a[j-1].unix; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
