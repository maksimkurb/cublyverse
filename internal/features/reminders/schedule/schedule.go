// Package schedule computes when a reminder fires next.
//
// A reminder has a kind (weekly or monthly), a list of days, a local wall clock
// time and an IANA time zone. NextDue is a pure function: the same inputs always
// give the same UTC instant, which is what makes the scheduler testable and the
// behaviour identical across the bot, the HTTP API and MCP.
package schedule

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"
)

// Kind is the shape of a schedule.
type Kind string

const (
	// Once is a single occurrence on the start date.
	Once Kind = "once"
	// Daily repeats every Interval days.
	Daily Kind = "daily"
	// Weekly repeats on ISO weekdays: 1 is Monday, 7 is Sunday.
	Weekly Kind = "weekly"
	// Monthly repeats on days of month. A day the month does not have means the
	// last day of that month: 31 is 28 or 29 February, 30 is 30 April.
	Monthly Kind = "monthly"
	// Yearly repeats on the month and day of the start date; 29 February becomes
	// 28 February in a year that has no 29th.
	Yearly Kind = "yearly"
)

// Search bounds. A weekly schedule always matches within a week and a monthly
// one within a month, so these are safety nets that turn a bug into an error
// instead of a long loop.
const (
	// WeeklySearchDays is how many days ahead a weekly schedule is searched.
	WeeklySearchDays = 15
	// MonthlySearchMonths is how many months ahead a monthly schedule is
	// searched.
	MonthlySearchMonths = 36
)

// ErrNoOccurrence means the search window contained no matching moment.
var ErrNoOccurrence = errors.New("schedule: no matching occurrence")

// NextDue returns the first occurrence of the schedule strictly later than
// after plus one second, as a UTC instant.
//
// The one-second grace is what makes the scheduler idempotent: a tick that runs
// exactly at the scheduled second must not compute the same instant again as
// the next due time.
//
// Daylight saving time is handled deterministically:
//
//   - a wall clock time that occurs twice (the autumn overlap) resolves to the
//     first occurrence, so the reminder fires once instead of twice;
//   - a wall clock time that does not exist (the spring gap) is shifted forward
//     by the length of the gap. For example 02:30 on the night Europe/Berlin
//     jumps from 02:00 to 03:00 becomes 03:30 CEST (01:30 UTC).
func NextDue(after time.Time, kind Kind, days []int, hhmm, timezone string) (time.Time, error) {
	hour, minute, err := ParseHHMM(hhmm)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := LoadLocation(timezone)
	if err != nil {
		return time.Time{}, err
	}
	if err := ValidateDays(kind, days); err != nil {
		return time.Time{}, err
	}

	threshold := after.Add(time.Second)
	local := threshold.In(loc)

	switch kind {
	case Weekly:
		for offset := 0; offset < WeeklySearchDays; offset++ {
			date := local.AddDate(0, 0, offset)
			if !containsDay(days, isoWeekday(date)) {
				continue
			}
			candidate := resolve(loc, date.Year(), date.Month(), date.Day(), hour, minute)
			if candidate.After(threshold) {
				return candidate.UTC(), nil
			}
		}
	case Monthly:
		sorted := NormalizeDays(days)
		for offset := 0; offset < MonthlySearchMonths; offset++ {
			year, month := addMonths(local.Year(), local.Month(), offset)
			lastDay := time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()
			for _, day := range sorted {
				// A day the month does not have means its last day. Two days
				// that collapse onto the same date (30 and 31 in February) are
				// skipped naturally: the first candidate is either in the
				// future and returned, or in the past and the second one is the
				// same instant.
				effective := min(day, lastDay)
				candidate := resolve(loc, year, month, effective, hour, minute)
				if candidate.After(threshold) {
					return candidate.UTC(), nil
				}
			}
		}
	default:
		return time.Time{}, fmt.Errorf("schedule: unknown kind %q", kind)
	}

	return time.Time{}, fmt.Errorf("%w: %s, days %v at %s in %s", ErrNoOccurrence, kind, days, hhmm, timezone)
}

// ParseHHMM parses a zero padded 24-hour "HH:MM" value. "9:30" and "24:00" are
// rejected, so the stored form is always canonical.
func ParseHHMM(value string) (hour, minute int, err error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, 0, fmt.Errorf("schedule: time %q must be HH:MM", value)
	}
	if hour, err = twoDigits(value[0:2]); err != nil {
		return 0, 0, fmt.Errorf("schedule: time %q must be HH:MM: %w", value, err)
	}
	if minute, err = twoDigits(value[3:5]); err != nil {
		return 0, 0, fmt.Errorf("schedule: time %q must be HH:MM: %w", value, err)
	}
	if hour > 23 || minute > 59 {
		return 0, 0, fmt.Errorf("schedule: time %q is out of range", value)
	}
	return hour, minute, nil
}

// LoadLocation resolves an IANA time zone name. "Local" is refused because it
// would depend on the server environment rather than on the user.
func LoadLocation(name string) (*time.Location, error) {
	if name == "" {
		return nil, errors.New("schedule: time zone must not be empty")
	}
	if name == "Local" {
		return nil, errors.New("schedule: time zone must be an IANA name")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("schedule: unknown time zone %q: %w", name, err)
	}
	return loc, nil
}

// ValidateDays checks that days fit the kind: 1..7 for weekly, 1..31 for
// monthly. Duplicates are allowed here and collapsed by NormalizeDays.
func ValidateDays(kind Kind, days []int) error {
	if len(days) == 0 {
		return errors.New("schedule: at least one day is required")
	}
	limit := 0
	switch kind {
	case Weekly:
		limit = 7
	case Monthly:
		limit = 31
	default:
		return fmt.Errorf("schedule: unknown kind %q", kind)
	}
	for _, day := range days {
		if day < 1 || day > limit {
			return fmt.Errorf("schedule: day %d is out of range for %s", day, kind)
		}
	}
	return nil
}

// NormalizeDays returns the days sorted and without duplicates, which keeps the
// stored form canonical.
func NormalizeDays(days []int) []int {
	out := make([]int, 0, len(days))
	for _, day := range days {
		if !slices.Contains(out, day) {
			out = append(out, day)
		}
	}
	sort.Ints(out)
	return out
}

// resolve returns the instant of a local wall clock time in loc.
//
// The wall clock time is represented as a UTC value first, so the offset lookup
// never depends on Go's choice for an ambiguous or nonexistent time.
func resolve(loc *time.Location, year int, month time.Month, day, hour, minute int) time.Time {
	wall := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)

	var matches []time.Time
	for _, offset := range zoneOffsets(loc, wall) {
		instant := wall.Add(-time.Duration(offset) * time.Second).In(loc)
		if instant.Year() == year && instant.Month() == month && instant.Day() == day &&
			instant.Hour() == hour && instant.Minute() == minute {
			matches = append(matches, instant)
		}
	}
	if len(matches) > 0 {
		// An ambiguous time has two instants; the first one is the one the user
		// means, and it also guarantees the reminder fires only once.
		first := matches[0]
		for _, candidate := range matches[1:] {
			if candidate.Before(first) {
				first = candidate
			}
		}
		return first
	}

	// The time does not exist: the clock jumped over it. Interpreting the wall
	// clock with the pre-transition offset yields the requested time shifted
	// forward by the gap, and always lands after the transition.
	return wall.Add(-time.Duration(offsetBefore(loc, wall)) * time.Second).In(loc)
}

// zoneOffsets returns the distinct UTC offsets (seconds east of UTC) the
// location uses around a wall clock time. Transitions are months apart, so
// probing a day either side is enough to see both sides of a change.
func zoneOffsets(loc *time.Location, wall time.Time) []int {
	offsets := make([]int, 0, 3)
	for _, probe := range []time.Duration{-26 * time.Hour, 0, 26 * time.Hour} {
		_, offset := wall.Add(probe).In(loc).Zone()
		if !slices.Contains(offsets, offset) {
			offsets = append(offsets, offset)
		}
	}
	return offsets
}

// offsetBefore returns the UTC offset in effect roughly a day before the wall
// clock time, which is the pre-transition offset for a spring gap.
func offsetBefore(loc *time.Location, wall time.Time) int {
	_, offset := wall.Add(-26 * time.Hour).In(loc).Zone()
	return offset
}

// addMonths returns the year and month offset months after the given one.
func addMonths(year int, month time.Month, offset int) (int, time.Month) {
	index := int(month) - 1 + offset
	return year + index/12, time.Month(index%12 + 1)
}

// isoWeekday maps Go's Sunday-first weekday to ISO numbering (Monday = 1).
func isoWeekday(t time.Time) int {
	weekday := int(t.Weekday())
	if weekday == 0 {
		return 7
	}
	return weekday
}

func containsDay(days []int, day int) bool {
	return slices.Contains(days, day)
}

// twoDigits parses exactly two ASCII digits, so signed and unpadded values are
// rejected instead of silently accepted.
func twoDigits(value string) (int, error) {
	if len(value) != 2 || value[0] < '0' || value[0] > '9' || value[1] < '0' || value[1] > '9' {
		return 0, fmt.Errorf("invalid number %q", value)
	}
	return int(value[0]-'0')*10 + int(value[1]-'0'), nil
}
