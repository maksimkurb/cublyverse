package schedule_test

import (
	"errors"
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
)

// date parses a test date.
func date(t *testing.T, value string) schedule.Date {
	t.Helper()
	parsed, err := schedule.ParseDate(value)
	if err != nil {
		t.Fatalf("parse date %q: %v", value, err)
	}
	return parsed
}

// TestRecurrenceNextDue is the main table of the flexible recurrence. It covers
// the kinds, the interval alignment relative to the start date, month ends, leap
// years, the end conditions and daylight saving time in a zone without a
// transition and in the two zones the project cares about.
func TestRecurrenceNextDue(t *testing.T) {
	tests := []struct {
		name    string
		after   string
		fired   int
		rule    schedule.Recurrence
		want    string
		wantErr bool
	}{
		{
			name:  "once in the future",
			after: "2026-01-01T00:00:00Z",
			rule: schedule.Recurrence{
				Kind:      schedule.Once,
				Interval:  1,
				StartDate: date(t, "2027-03-20"),
				TimeHHMM:  "10:00",
				Timezone:  "Europe/Moscow",
			},
			want: "2027-03-20T07:00:00Z",
		},
		{
			name:    "once that already passed",
			after:   "2027-03-20T07:00:00Z",
			rule:    schedule.Recurrence{Kind: schedule.Once, Interval: 1, StartDate: date(t, "2027-03-20"), TimeHHMM: "10:00", Timezone: "Europe/Moscow"},
			wantErr: true,
		},
		{
			name:  "daily keeps the wall clock time in a zone without DST",
			after: "2026-06-14T12:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-06-01"), TimeHHMM: "09:00", Timezone: "Asia/Oral"},
			want:  "2026-06-15T04:00:00Z",
		},
		{
			name:  "daily keeps the wall clock time in Europe/Moscow across the Berlin transition",
			after: "2026-03-28T12:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-03-01"), TimeHHMM: "02:30", Timezone: "Europe/Moscow"},
			want:  "2026-03-28T23:30:00Z", // 02:30 MSK on March 29, no shift
		},
		{
			name:  "daily every third day from the start date",
			after: "2026-01-01T10:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 3, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-04T09:00:00Z",
		},
		{
			name:  "daily interval skips the days between",
			after: "2026-01-05T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 3, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-07T09:00:00Z",
		},
		{
			name:  "daily spring gap in Europe/Berlin fires after the gap",
			after: "2026-03-28T12:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-03-01"), TimeHHMM: "02:30", Timezone: "Europe/Berlin"},
			want:  "2026-03-29T01:30:00Z", // 03:30 CEST
		},
		{
			name:  "daily autumn overlap in Europe/Berlin fires once at the first instant",
			after: "2026-10-24T12:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-10-01"), TimeHHMM: "02:30", Timezone: "Europe/Berlin"},
			want:  "2026-10-25T00:30:00Z", // 02:30 CEST, before the fall back
		},
		{
			name:  "daily spring gap in America/New_York fires after the gap",
			after: "2026-03-07T12:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-03-01"), TimeHHMM: "02:30", Timezone: "America/New_York"},
			want:  "2026-03-08T07:30:00Z", // 03:30 EDT
		},
		{
			name:  "daily autumn overlap in America/New_York fires once at the first instant",
			after: "2026-10-31T12:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-10-01"), TimeHHMM: "01:30", Timezone: "America/New_York"},
			want:  "2026-11-01T05:30:00Z", // 01:30 EDT
		},
		{
			name:  "weekly every second week on Monday and Thursday",
			after: "2026-01-09T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Weekly, Interval: 2, Days: []int{1, 4}, StartDate: date(t, "2026-01-05"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-19T09:00:00Z",
		},
		{
			name:  "weekly start date cuts the first week earlier days",
			after: "2026-01-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Weekly, Interval: 1, Days: []int{1, 4}, StartDate: date(t, "2026-01-07"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-08T09:00:00Z", // Thursday, not Monday January 5
		},
		{
			name:  "weekly interval counts weeks from the start week",
			after: "2026-01-09T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Weekly, Interval: 2, Days: []int{1}, StartDate: date(t, "2026-01-07"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-19T09:00:00Z", // week 2, not January 12
		},
		{
			name:  "monthly day thirty-one in february becomes the last day",
			after: "2026-02-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 1, Days: []int{31}, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-02-28T09:00:00Z",
		},
		{
			name:  "monthly day thirty-one in a leap february becomes the twenty-ninth",
			after: "2028-02-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 1, Days: []int{31}, StartDate: date(t, "2028-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2028-02-29T09:00:00Z",
		},
		{
			name:  "monthly collapsed days fire once",
			after: "2026-02-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 1, Days: []int{30, 31}, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-02-28T09:00:00Z",
		},
		{
			name:  "monthly every second month on the fifteenth",
			after: "2026-02-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 2, Days: []int{15}, StartDate: date(t, "2026-01-15"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-03-15T09:00:00Z",
		},
		{
			name:  "monthly second tuesday",
			after: "2026-01-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 1, MonthWeekPosition: 2, MonthWeekday: 2, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-13T09:00:00Z",
		},
		{
			name:  "monthly last friday",
			after: "2026-01-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 1, MonthWeekPosition: -1, MonthWeekday: 5, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-30T09:00:00Z",
		},
		{
			name:  "monthly nth weekday after the occurrence this month",
			after: "2026-01-13T09:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Monthly, Interval: 1, MonthWeekPosition: 2, MonthWeekday: 2, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-02-10T09:00:00Z",
		},
		{
			name:  "yearly on the same month and day",
			after: "2026-06-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Yearly, Interval: 1, StartDate: date(t, "2026-03-20"), TimeHHMM: "10:00", Timezone: "Europe/Moscow"},
			want:  "2027-03-20T07:00:00Z",
		},
		{
			name:  "yearly every second year",
			after: "2026-06-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Yearly, Interval: 2, StartDate: date(t, "2026-03-20"), TimeHHMM: "10:00", Timezone: "UTC"},
			want:  "2028-03-20T10:00:00Z",
		},
		{
			name:  "yearly february twenty-ninth becomes the twenty-eighth",
			after: "2028-03-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Yearly, Interval: 1, StartDate: date(t, "2028-02-29"), TimeHHMM: "10:00", Timezone: "UTC"},
			want:  "2029-02-28T10:00:00Z",
		},
		{
			name:  "ends after the configured count",
			after: "2026-01-05T09:00:00Z",
			fired: 1,
			rule: schedule.Recurrence{
				Kind: schedule.Weekly, Interval: 1, Days: []int{1},
				StartDate: date(t, "2026-01-05"), TimeHHMM: "09:00", Timezone: "UTC",
				End: schedule.End{Kind: schedule.EndCount, Count: 2},
			},
			want: "2026-01-12T09:00:00Z",
		},
		{
			name:    "count exhausted",
			after:   "2026-01-12T09:00:00Z",
			fired:   2,
			rule:    schedule.Recurrence{Kind: schedule.Weekly, Interval: 1, Days: []int{1}, StartDate: date(t, "2026-01-05"), TimeHHMM: "09:00", Timezone: "UTC", End: schedule.End{Kind: schedule.EndCount, Count: 2}},
			wantErr: true,
		},
		{
			name:  "until a date is inclusive",
			after: "2026-01-02T10:00:00Z",
			rule: schedule.Recurrence{
				Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-01-01"),
				TimeHHMM: "09:00", Timezone: "UTC",
				End: schedule.End{Kind: schedule.EndUntil, Date: date(t, "2026-01-03")},
			},
			want: "2026-01-03T09:00:00Z",
		},
		{
			name:  "until a date passed",
			after: "2026-01-03T09:00:00Z",
			rule: schedule.Recurrence{
				Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-01-01"),
				TimeHHMM: "09:00", Timezone: "UTC",
				End: schedule.End{Kind: schedule.EndUntil, Date: date(t, "2026-01-03")},
			},
			wantErr: true,
		},
		{
			name:  "start date in the future skips the days before it",
			after: "2026-01-01T00:00:00Z",
			rule:  schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-01-10"), TimeHHMM: "09:00", Timezone: "UTC"},
			want:  "2026-01-10T09:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.rule.NextDue(mustTime(t, tt.after), tt.fired)
			if tt.wantErr {
				if !errors.Is(err, schedule.ErrNoOccurrence) {
					t.Fatalf("NextDue error = %v, want ErrNoOccurrence", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NextDue: %v", err)
			}
			if want := mustTime(t, tt.want); !got.Equal(want) {
				t.Errorf("NextDue = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
			if got.Location() != time.UTC {
				t.Errorf("NextDue returned location %v, want UTC", got.Location())
			}
		})
	}
}

// TestRecurrenceMatchesLegacyWeeklyAndMonthly proves the migration is
// behaviour-preserving: a backfilled row has interval 1 and no end, and its
// start date is in the past, so the new recurrence must compute exactly what the
// old weekly/monthly NextDue computed.
func TestRecurrenceMatchesLegacyWeeklyAndMonthly(t *testing.T) {
	tests := []struct {
		name  string
		after string
		kind  schedule.Kind
		days  []int
		hhmm  string
		tz    string
	}{
		{name: "weekly today ahead", after: "2026-01-05T06:00:00Z", kind: schedule.Weekly, days: []int{1}, hhmm: "10:00", tz: "Europe/Moscow"},
		{name: "weekly today passed", after: "2026-01-05T08:00:00Z", kind: schedule.Weekly, days: []int{1}, hhmm: "10:00", tz: "Europe/Moscow"},
		{name: "weekly several days", after: "2026-01-07T12:00:00Z", kind: schedule.Weekly, days: []int{1, 3, 5}, hhmm: "10:00", tz: "Europe/Moscow"},
		{name: "weekly sunday", after: "2026-01-10T12:00:00Z", kind: schedule.Weekly, days: []int{7}, hhmm: "10:00", tz: "Europe/Moscow"},
		{name: "weekly berlin spring gap", after: "2026-03-28T12:00:00Z", kind: schedule.Weekly, days: []int{7}, hhmm: "02:30", tz: "Europe/Berlin"},
		{name: "weekly berlin overlap", after: "2026-10-24T12:00:00Z", kind: schedule.Weekly, days: []int{7}, hhmm: "02:30", tz: "Europe/Berlin"},
		{name: "weekly new york gap", after: "2026-03-07T12:00:00Z", kind: schedule.Weekly, days: []int{7}, hhmm: "02:30", tz: "America/New_York"},
		{name: "monthly day 31 february", after: "2026-02-01T00:00:00Z", kind: schedule.Monthly, days: []int{31}, hhmm: "09:00", tz: "UTC"},
		{name: "monthly day 31 leap february", after: "2028-02-01T00:00:00Z", kind: schedule.Monthly, days: []int{31}, hhmm: "09:00", tz: "UTC"},
		{name: "monthly collapsed", after: "2026-02-01T00:00:00Z", kind: schedule.Monthly, days: []int{30, 31}, hhmm: "09:00", tz: "UTC"},
		{name: "monthly unsorted", after: "2026-01-01T00:00:00Z", kind: schedule.Monthly, days: []int{31, 15, 1}, hhmm: "09:00", tz: "UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			after := mustTime(t, tt.after)
			legacy, err := schedule.NextDue(after, tt.kind, tt.days, tt.hhmm, tt.tz)
			if err != nil {
				t.Fatalf("legacy NextDue: %v", err)
			}
			recurrence := schedule.Recurrence{
				Kind:      tt.kind,
				Interval:  1,
				Days:      tt.days,
				StartDate: date(t, "2000-01-01"),
				TimeHHMM:  tt.hhmm,
				Timezone:  tt.tz,
			}
			got, err := recurrence.NextDue(after, 0)
			if err != nil {
				t.Fatalf("recurrence NextDue: %v", err)
			}
			if !got.Equal(legacy) {
				t.Errorf("recurrence = %s, legacy = %s", got.Format(time.RFC3339), legacy.Format(time.RFC3339))
			}
		})
	}
}

// TestRecurrenceStrictlyAfterOneSecond mirrors TestNextDueStrictlyAfterOneSecond
// for the recurrence form.
func TestRecurrenceStrictlyAfterOneSecond(t *testing.T) {
	due := mustTime(t, "2026-01-05T09:00:00Z")
	rule := schedule.Recurrence{
		Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-01-01"),
		TimeHHMM: "09:00", Timezone: "UTC",
	}
	tests := []struct {
		name  string
		after time.Time
		want  time.Time
	}{
		{name: "exactly at the occurrence", after: due, want: mustTime(t, "2026-01-06T09:00:00Z")},
		{name: "half a second after", after: due.Add(500 * time.Millisecond), want: mustTime(t, "2026-01-06T09:00:00Z")},
		{name: "two seconds before", after: due.Add(-2 * time.Second), want: due},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rule.NextDue(tt.after, 0)
			if err != nil {
				t.Fatalf("NextDue: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextDue = %s, want %s", got.Format(time.RFC3339Nano), tt.want.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestRecurrenceErrors(t *testing.T) {
	tests := []struct {
		name string
		rule schedule.Recurrence
	}{
		{name: "unknown kind", rule: schedule.Recurrence{Kind: "fortnightly", Interval: 1, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"}},
		{name: "bad time", rule: schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-01-01"), TimeHHMM: "9:00", Timezone: "UTC"}},
		{name: "bad zone", rule: schedule.Recurrence{Kind: schedule.Daily, Interval: 1, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "Mars/Olympus"}},
		{name: "missing start", rule: schedule.Recurrence{Kind: schedule.Daily, Interval: 1, TimeHHMM: "09:00", Timezone: "UTC"}},
		{name: "zero interval", rule: schedule.Recurrence{Kind: schedule.Daily, Interval: 0, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"}},
		{name: "weekly without days", rule: schedule.Recurrence{Kind: schedule.Weekly, Interval: 1, StartDate: date(t, "2026-01-01"), TimeHHMM: "09:00", Timezone: "UTC"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.rule.NextDue(mustTime(t, "2026-01-01T00:00:00Z"), 0); err == nil {
				t.Fatal("NextDue returned no error")
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "canonical", value: "2026-03-20", want: "2026-03-20"},
		{name: "leap day", value: "2028-02-29", want: "2028-02-29"},
		{name: "unpadded", value: "2026-3-20", wantErr: true},
		{name: "impossible day", value: "2026-02-30", wantErr: true},
		{name: "empty", value: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := schedule.ParseDate(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) = %s, want an error", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q): %v", tt.value, err)
			}
			if got.String() != tt.want {
				t.Errorf("ParseDate(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}
