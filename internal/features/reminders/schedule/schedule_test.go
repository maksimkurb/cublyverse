package schedule_test

import (
	"testing"
	"time"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
)

// mustTime parses an RFC 3339 instant; the tests never use local time.
func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func TestNextDue(t *testing.T) {
	tests := []struct {
		name  string
		after string
		kind  schedule.Kind
		days  []int
		hhmm  string
		tz    string
		want  string
	}{
		{
			name:  "weekly today, time still ahead",
			after: "2026-01-05T06:00:00Z", // Monday 09:00 in Moscow
			kind:  schedule.Weekly,
			days:  []int{1},
			hhmm:  "10:00",
			tz:    "Europe/Moscow",
			want:  "2026-01-05T07:00:00Z",
		},
		{
			name:  "weekly today, time already passed",
			after: "2026-01-05T08:00:00Z", // Monday 11:00 in Moscow
			kind:  schedule.Weekly,
			days:  []int{1},
			hhmm:  "10:00",
			tz:    "Europe/Moscow",
			want:  "2026-01-12T07:00:00Z",
		},
		{
			name:  "weekly several days picks the nearest",
			after: "2026-01-07T12:00:00Z", // Wednesday 15:00 in Moscow
			kind:  schedule.Weekly,
			days:  []int{1, 3, 5},
			hhmm:  "10:00",
			tz:    "Europe/Moscow",
			want:  "2026-01-09T07:00:00Z", // Friday
		},
		{
			name:  "weekly sunday is day seven",
			after: "2026-01-10T12:00:00Z", // Saturday
			kind:  schedule.Weekly,
			days:  []int{7},
			hhmm:  "10:00",
			tz:    "Europe/Moscow",
			want:  "2026-01-11T07:00:00Z", // Sunday
		},
		{
			name:  "weekly unsorted and duplicated days",
			after: "2026-01-06T00:00:00Z", // Tuesday
			kind:  schedule.Weekly,
			days:  []int{5, 1, 5, 3},
			hhmm:  "10:00",
			tz:    "Europe/Moscow",
			want:  "2026-01-07T07:00:00Z", // Wednesday
		},
		{
			name:  "weekly crosses the year boundary",
			after: "2026-12-28T10:00:00Z", // Monday, 10:00 already passed
			kind:  schedule.Weekly,
			days:  []int{1},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2027-01-04T09:00:00Z",
		},
		{
			name:  "monthly day thirty-one in february becomes the last day",
			after: "2026-02-01T00:00:00Z",
			kind:  schedule.Monthly,
			days:  []int{31},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2026-02-28T09:00:00Z",
		},
		{
			name:  "monthly day thirty-one in a leap february becomes the twenty-ninth",
			after: "2028-02-01T00:00:00Z",
			kind:  schedule.Monthly,
			days:  []int{31},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2028-02-29T09:00:00Z",
		},
		{
			name:  "monthly day thirty-one in april becomes the thirtieth",
			after: "2026-04-01T00:00:00Z",
			kind:  schedule.Monthly,
			days:  []int{31},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2026-04-30T09:00:00Z",
		},
		{
			name:  "monthly days collapsing onto one date fire once",
			after: "2026-02-01T00:00:00Z",
			kind:  schedule.Monthly,
			days:  []int{30, 31},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2026-02-28T09:00:00Z",
		},
		{
			name:  "monthly collapsed date is not repeated for the second day",
			after: "2026-02-28T10:00:00Z", // the collapsed February date has passed
			kind:  schedule.Monthly,
			days:  []int{30, 31},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2026-03-30T09:00:00Z",
		},
		{
			name:  "monthly crosses the year boundary",
			after: "2026-12-15T00:00:00Z",
			kind:  schedule.Monthly,
			days:  []int{1},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2027-01-01T09:00:00Z",
		},
		{
			name:  "monthly skips a month without a matching moment",
			after: "2026-04-30T10:00:00Z", // April 30 at 09:00 has passed
			kind:  schedule.Monthly,
			days:  []int{31},
			hhmm:  "09:00",
			tz:    "UTC",
			want:  "2026-05-31T09:00:00Z",
		},
		{
			name:  "dst gap in Europe/Berlin shifts forward by the gap",
			after: "2026-03-28T12:00:00Z",
			kind:  schedule.Weekly,
			days:  []int{7}, // Sunday
			hhmm:  "02:30",  // 02:30 does not exist on 2026-03-29
			tz:    "Europe/Berlin",
			want:  "2026-03-29T01:30:00Z", // 03:30 CEST
		},
		{
			name:  "dst overlap in Europe/Berlin fires at the first occurrence",
			after: "2026-10-24T12:00:00Z",
			kind:  schedule.Weekly,
			days:  []int{7}, // Sunday
			hhmm:  "02:30",  // 02:30 happens twice on 2026-10-25
			tz:    "Europe/Berlin",
			want:  "2026-10-25T00:30:00Z", // 02:30 CEST, before the fall back
		},
		{
			name:  "dst gap in America/New_York shifts forward by the gap",
			after: "2026-03-07T12:00:00Z",
			kind:  schedule.Weekly,
			days:  []int{7}, // Sunday
			hhmm:  "02:30",  // 02:30 does not exist on 2026-03-08
			tz:    "America/New_York",
			want:  "2026-03-08T07:30:00Z", // 03:30 EDT
		},
		{
			name:  "dst overlap in America/New_York fires at the first occurrence",
			after: "2026-10-31T12:00:00Z",
			kind:  schedule.Weekly,
			days:  []int{7}, // Sunday
			hhmm:  "01:30",  // 01:30 happens twice on 2026-11-01
			tz:    "America/New_York",
			want:  "2026-11-01T05:30:00Z", // 01:30 EDT, before the fall back
		},
		{
			name:  "zone without daylight saving time keeps its offset",
			after: "2026-06-14T12:00:00Z",
			kind:  schedule.Weekly,
			days:  []int{1}, // Monday
			hhmm:  "09:00",
			tz:    "Asia/Oral",
			want:  "2026-06-15T04:00:00Z", // 09:00 UTC+05
		},
		{
			name:  "zone without daylight saving time across the winter",
			after: "2026-12-13T12:00:00Z",
			kind:  schedule.Weekly,
			days:  []int{1},
			hhmm:  "09:00",
			tz:    "Asia/Oral",
			want:  "2026-12-14T04:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := schedule.NextDue(mustTime(t, tt.after), tt.kind, tt.days, tt.hhmm, tt.tz)
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

// TestNextDueStrictlyAfterOneSecond pins the "+1 second" rule: an occurrence
// that is exactly one second (or less) after after is not due yet, so the tick
// that just fired cannot schedule the same instant again.
func TestNextDueStrictlyAfterOneSecond(t *testing.T) {
	due := mustTime(t, "2026-01-05T09:00:00Z") // Monday 09:00 UTC
	tests := []struct {
		name  string
		after time.Time
		want  time.Time
	}{
		{
			name:  "exactly at the occurrence",
			after: due,
			want:  mustTime(t, "2026-01-12T09:00:00Z"),
		},
		{
			name:  "half a second after the occurrence",
			after: due.Add(500 * time.Millisecond),
			want:  mustTime(t, "2026-01-12T09:00:00Z"),
		},
		{
			name:  "exactly one second after the occurrence",
			after: due.Add(time.Second),
			want:  mustTime(t, "2026-01-12T09:00:00Z"),
		},
		{
			name:  "one and a half seconds after the occurrence",
			after: due.Add(1500 * time.Millisecond),
			want:  mustTime(t, "2026-01-12T09:00:00Z"),
		},
		{
			name:  "two seconds before the occurrence",
			after: due.Add(-2 * time.Second),
			want:  due,
		},
		{
			name:  "one second and a nanosecond before the occurrence",
			after: due.Add(-time.Second - time.Nanosecond),
			want:  due,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := schedule.NextDue(tt.after, schedule.Weekly, []int{1}, "09:00", "UTC")
			if err != nil {
				t.Fatalf("NextDue: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextDue = %s, want %s", got.Format(time.RFC3339Nano), tt.want.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestNextDueErrors(t *testing.T) {
	tests := []struct {
		name string
		kind schedule.Kind
		days []int
		hhmm string
		tz   string
	}{
		{name: "unpadded hour", kind: schedule.Weekly, days: []int{1}, hhmm: "9:00", tz: "UTC"},
		{name: "hour out of range", kind: schedule.Weekly, days: []int{1}, hhmm: "24:00", tz: "UTC"},
		{name: "minute out of range", kind: schedule.Weekly, days: []int{1}, hhmm: "09:60", tz: "UTC"},
		{name: "signed hour", kind: schedule.Weekly, days: []int{1}, hhmm: "+9:00", tz: "UTC"},
		{name: "unknown time zone", kind: schedule.Weekly, days: []int{1}, hhmm: "09:00", tz: "Mars/Olympus"},
		{name: "local time zone", kind: schedule.Weekly, days: []int{1}, hhmm: "09:00", tz: "Local"},
		{name: "empty time zone", kind: schedule.Weekly, days: []int{1}, hhmm: "09:00", tz: ""},
		{name: "no days", kind: schedule.Weekly, days: nil, hhmm: "09:00", tz: "UTC"},
		{name: "weekly day eight", kind: schedule.Weekly, days: []int{8}, hhmm: "09:00", tz: "UTC"},
		{name: "weekly day zero", kind: schedule.Weekly, days: []int{0}, hhmm: "09:00", tz: "UTC"},
		{name: "monthly day thirty-two", kind: schedule.Monthly, days: []int{32}, hhmm: "09:00", tz: "UTC"},
		{name: "unknown kind", kind: schedule.Kind("daily"), days: []int{1}, hhmm: "09:00", tz: "UTC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := schedule.NextDue(mustTime(t, "2026-01-01T00:00:00Z"), tt.kind, tt.days, tt.hhmm, tt.tz); err == nil {
				t.Fatal("NextDue returned no error")
			}
		})
	}
}

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		hour    int
		minute  int
		wantErr bool
	}{
		{name: "midnight", value: "00:00", hour: 0, minute: 0},
		{name: "padded", value: "09:05", hour: 9, minute: 5},
		{name: "last minute", value: "23:59", hour: 23, minute: 59},
		{name: "empty", value: "", wantErr: true},
		{name: "unpadded", value: "9:00", wantErr: true},
		{name: "no separator", value: "0900", wantErr: true},
		{name: "wrong separator", value: "09-00", wantErr: true},
		{name: "hour out of range", value: "24:00", wantErr: true},
		{name: "minute out of range", value: "00:60", wantErr: true},
		{name: "letters", value: "ab:cd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hour, minute, err := schedule.ParseHHMM(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHHMM(%q) = %d:%d, want an error", tt.value, hour, minute)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHHMM(%q): %v", tt.value, err)
			}
			if hour != tt.hour || minute != tt.minute {
				t.Errorf("ParseHHMM(%q) = %d:%d, want %d:%d", tt.value, hour, minute, tt.hour, tt.minute)
			}
		})
	}
}

func TestValidateDays(t *testing.T) {
	tests := []struct {
		name    string
		kind    schedule.Kind
		days    []int
		wantErr bool
	}{
		{name: "weekly monday", kind: schedule.Weekly, days: []int{1}},
		{name: "weekly sunday", kind: schedule.Weekly, days: []int{7}},
		{name: "monthly first", kind: schedule.Monthly, days: []int{1}},
		{name: "monthly thirty-first", kind: schedule.Monthly, days: []int{31}},
		{name: "empty", kind: schedule.Weekly, days: nil, wantErr: true},
		{name: "weekly zero", kind: schedule.Weekly, days: []int{0}, wantErr: true},
		{name: "weekly eight", kind: schedule.Weekly, days: []int{8}, wantErr: true},
		{name: "monthly zero", kind: schedule.Monthly, days: []int{0}, wantErr: true},
		{name: "monthly thirty-two", kind: schedule.Monthly, days: []int{32}, wantErr: true},
		{name: "unknown kind", kind: schedule.Kind("yearly"), days: []int{1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schedule.ValidateDays(tt.kind, tt.days)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateDays returned no error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateDays: %v", err)
			}
		})
	}
}

func TestNormalizeDays(t *testing.T) {
	tests := []struct {
		name string
		days []int
		want []int
	}{
		{name: "sorted and deduplicated", days: []int{5, 1, 3, 5, 1}, want: []int{1, 3, 5}},
		{name: "already canonical", days: []int{2, 4}, want: []int{2, 4}},
		{name: "empty", days: nil, want: []int{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schedule.NormalizeDays(tt.days)
			if len(got) != len(tt.want) {
				t.Fatalf("NormalizeDays(%v) = %v, want %v", tt.days, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("NormalizeDays(%v) = %v, want %v", tt.days, got, tt.want)
				}
			}
		})
	}
}
