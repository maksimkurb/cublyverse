package schedule

import (
	"fmt"
	"time"
)

// Date is a civil calendar date without a time of day and without a zone. A
// schedule stores dates in the reminder's own time zone, so a date alone is the
// right unit: "the 15th of March" must not shift with the server's location.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate builds a date from its parts. The parts are normalised by the
// calendar, so time.Date(2026, time.February, 28, ...) is accepted as is.
func NewDate(year int, month time.Month, day int) Date {
	t := time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
	return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// ParseDate parses the canonical form `YYYY-MM-DD`. Unpadded parts and dates
// the calendar does not have are rejected instead of being silently adjusted.
func ParseDate(value string) (Date, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return Date{}, fmt.Errorf("schedule: date %q must be YYYY-MM-DD: %w", value, err)
	}
	return Date{Year: parsed.Year(), Month: parsed.Month(), Day: parsed.Day()}, nil
}

// DateOf returns the civil date of an instant in loc.
func DateOf(instant time.Time, loc *time.Location) Date {
	year, month, day := instant.In(loc).Date()
	return Date{Year: year, Month: month, Day: day}
}

// IsZero reports whether the date was not set.
func (d Date) IsZero() bool { return d.Year == 0 && d.Month == 0 && d.Day == 0 }

// String renders the canonical `YYYY-MM-DD` form.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// Before reports whether d is an earlier calendar day than other.
func (d Date) Before(other Date) bool { return d.epochDay() < other.epochDay() }

// After reports whether d is a later calendar day than other.
func (d Date) After(other Date) bool { return d.epochDay() > other.epochDay() }

// Equal reports whether both values are the same calendar day.
func (d Date) Equal(other Date) bool { return d.epochDay() == other.epochDay() }

// AddDays returns the date n days later (or earlier, for a negative n).
func (d Date) AddDays(n int) Date {
	shifted := time.Unix(int64(d.epochDay()+n)*86400, 0).UTC()
	return Date{Year: shifted.Year(), Month: shifted.Month(), Day: shifted.Day()}
}

// ISOWeekday returns the ISO weekday: 1 is Monday, 7 is Sunday.
func (d Date) ISOWeekday() int {
	return isoWeekday(time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC))
}

// monday returns the Monday of the ISO week that contains d.
func (d Date) monday() Date { return d.AddDays(1 - d.ISOWeekday()) }

func (d Date) epochDay() int {
	return int(time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// EndKind is how a repeating recurrence stops.
type EndKind string

const (
	// EndNever repeats forever.
	EndNever EndKind = "none"
	// EndUntil repeats through End.Date inclusive.
	EndUntil EndKind = "until"
	// EndCount repeats End.Count times in total.
	EndCount EndKind = "count"
)

// End is the stop condition of a recurrence.
type End struct {
	// Kind is none, until or count.
	Kind EndKind
	// Date is the last allowed day for EndUntil.
	Date Date
	// Count is the total number of occurrences for EndCount.
	Count int
}

// Recurrence is a complete, pure description of when a reminder repeats.
//
// All dates are civil dates in Timezone, and all times are the local wall clock
// time TimeHHMM. NextDue turns the description into a UTC instant.
type Recurrence struct {
	// Kind is once, daily, weekly, monthly or yearly.
	Kind Kind
	// Interval is the gap in Kind units (days, weeks, months or years), >= 1.
	// It is always relative to StartDate.
	Interval int
	// Days are ISO weekdays (1..7) for a weekly rule, or days of month (1..31)
	// for a monthly day-of-month rule. Other kinds do not use it.
	Days []int
	// MonthWeekPosition selects the nth weekday of a month: 1..4, or -1 for the
	// last one. Zero means the rule uses Days instead.
	MonthWeekPosition int
	// MonthWeekday is the ISO weekday of an nth-weekday monthly rule.
	MonthWeekday int
	// StartDate is the first possible day of the rule.
	StartDate Date
	// End is the stop condition.
	End End
	// TimeHHMM is the local wall clock time, HH:MM.
	TimeHHMM string
	// Timezone is the IANA time zone of the schedule.
	Timezone string
}

// recurrenceSearchLimit bounds the walk through candidate periods. Every kind
// jumps directly to the period near `after`, so a healthy rule needs one or two
// iterations; the limit only turns a bug into an error instead of a long loop.
const recurrenceSearchLimit = 1000

// NextDue returns the first occurrence strictly later than after plus one
// second, as a UTC instant. `fired` is how many occurrences already fired; it
// is used by an EndCount rule, where the occurrence returned is number
// fired+1.
//
// ErrNoOccurrence means the recurrence has no future occurrence: it is a
// one-time date in the past, an "until" date has passed, or the count is
// exhausted.
func (r Recurrence) NextDue(after time.Time, fired int) (time.Time, error) {
	hour, minute, err := ParseHHMM(r.TimeHHMM)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := LoadLocation(r.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	if r.Interval < 1 {
		return time.Time{}, fmt.Errorf("schedule: interval %d must be at least 1", r.Interval)
	}
	if r.StartDate.IsZero() {
		return time.Time{}, fmt.Errorf("schedule: start date is required")
	}
	if r.End.Kind == EndCount && fired >= r.End.Count {
		return time.Time{}, errExhausted("count")
	}

	threshold := after.Add(time.Second)
	resolveDate := func(date Date) time.Time {
		return resolve(loc, date.Year, date.Month, date.Day, hour, minute)
	}
	// allowed applies the start and end dates to one candidate day.
	allowed := func(date Date) (bool, error) {
		if date.Before(r.StartDate) {
			return false, nil
		}
		if r.End.Kind == EndUntil && date.After(r.End.Date) {
			return false, errExhausted("until")
		}
		return true, nil
	}
	accept := func(date Date) (bool, time.Time, error) {
		ok, err := allowed(date)
		if err != nil {
			return false, time.Time{}, err
		}
		if !ok {
			return false, time.Time{}, nil
		}
		candidate := resolveDate(date)
		if candidate.After(threshold) {
			return true, candidate.UTC(), nil
		}
		return false, time.Time{}, nil
	}

	switch r.Kind {
	case Once:
		ok, candidate, err := accept(r.StartDate)
		switch {
		case err != nil:
			return time.Time{}, err
		case !ok:
			return time.Time{}, errExhausted("once")
		default:
			return candidate, nil
		}

	case Daily:
		first := dayOffset(r.StartDate, DateOf(threshold.In(loc), loc))
		step := r.Interval
		k := 0
		if first > 0 {
			k = first / step
		}
		for i := 0; i < recurrenceSearchLimit; i++ {
			ok, candidate, err := accept(r.StartDate.AddDays(k * step))
			if err != nil {
				return time.Time{}, err
			}
			if ok {
				return candidate, nil
			}
			k++
		}
		return time.Time{}, fmt.Errorf("%w: daily interval %d", ErrNoOccurrence, r.Interval)

	case Weekly:
		days := NormalizeDays(r.Days)
		if len(days) == 0 {
			return time.Time{}, fmt.Errorf("schedule: weekly recurrence needs at least one weekday")
		}
		weeks := dayOffset(r.StartDate.monday(), DateOf(threshold.In(loc), loc).monday()) / 7
		k := 0
		if weeks > 0 {
			k = weeks / r.Interval
		}
		for i := 0; i < recurrenceSearchLimit; i++ {
			monday := r.StartDate.monday().AddDays(k * r.Interval * 7)
			for _, day := range days {
				date := monday.AddDays(day - 1)
				ok, candidate, err := accept(date)
				if err != nil {
					return time.Time{}, err
				}
				if ok {
					return candidate, nil
				}
			}
			k++
		}
		return time.Time{}, fmt.Errorf("%w: weekly interval %d", ErrNoOccurrence, r.Interval)

	case Monthly:
		startMonth := monthIndex(r.StartDate.Year, r.StartDate.Month)
		firstMonth := monthIndex(threshold.In(loc).Year(), threshold.In(loc).Month())
		k := 0
		if firstMonth > startMonth {
			k = (firstMonth - startMonth) / r.Interval
		}
		for i := 0; i < recurrenceSearchLimit; i++ {
			year, month := monthFromIndex(startMonth + k*r.Interval)
			for _, date := range monthlyDates(year, month, r) {
				ok, candidate, err := accept(date)
				if err != nil {
					return time.Time{}, err
				}
				if ok {
					return candidate, nil
				}
			}
			k++
		}
		return time.Time{}, fmt.Errorf("%w: monthly interval %d", ErrNoOccurrence, r.Interval)

	case Yearly:
		firstYear := threshold.In(loc).Year()
		k := 0
		if firstYear > r.StartDate.Year {
			k = (firstYear - r.StartDate.Year) / r.Interval
		}
		for i := 0; i < recurrenceSearchLimit; i++ {
			year := r.StartDate.Year + k*r.Interval
			day := min(r.StartDate.Day, daysInMonth(year, r.StartDate.Month))
			ok, candidate, err := accept(NewDate(year, r.StartDate.Month, day))
			if err != nil {
				return time.Time{}, err
			}
			if ok {
				return candidate, nil
			}
			k++
		}
		return time.Time{}, fmt.Errorf("%w: yearly interval %d", ErrNoOccurrence, r.Interval)

	default:
		return time.Time{}, fmt.Errorf("schedule: unknown kind %q", r.Kind)
	}
}

// errExhausted reports a recurrence that has no future occurrence. The reason
// is part of the message so a log explains why a reminder stopped.
func errExhausted(reason string) error {
	return fmt.Errorf("%w: %s rule has no future occurrence", ErrNoOccurrence, reason)
}

// dayOffset returns how many days b is after a (negative when it is before).
func dayOffset(a, b Date) int { return b.epochDay() - a.epochDay() }

// monthlyDates returns the candidate dates of one month, sorted. An nth-weekday
// rule produces one date; a day-of-month rule produces one date per distinct
// effective day, where a day the month does not have means its last day.
func monthlyDates(year int, month time.Month, r Recurrence) []Date {
	lastDay := daysInMonth(year, month)
	if r.MonthWeekPosition != 0 {
		weekday := r.MonthWeekday
		if weekday < 1 || weekday > 7 {
			return nil
		}
		if r.MonthWeekPosition == -1 {
			last := NewDate(year, month, lastDay)
			return []Date{last.AddDays(-((last.ISOWeekday() - weekday + 7) % 7))}
		}
		first := NewDate(year, month, 1)
		day := 1 + (weekday-first.ISOWeekday()+7)%7 + (r.MonthWeekPosition-1)*7
		if day > lastDay {
			return nil
		}
		return []Date{NewDate(year, month, day)}
	}

	out := make([]Date, 0, len(r.Days))
	for _, day := range NormalizeDays(r.Days) {
		effective := min(day, lastDay)
		date := NewDate(year, month, effective)
		if len(out) == 0 || !out[len(out)-1].Equal(date) {
			out = append(out, date)
		}
	}
	return out
}

// monthIndex maps a year and month to a monotonically increasing number.
func monthIndex(year int, month time.Month) int { return year*12 + int(month) - 1 }

// monthFromIndex is the inverse of monthIndex.
func monthFromIndex(index int) (int, time.Month) {
	return index / 12, time.Month(index%12 + 1)
}

// daysInMonth returns how many days a month has.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 12, 0, 0, 0, time.UTC).Day()
}
