package reminders_test

import (
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
)

// TestScheduleDescription pins the single Russian wording used by the bot and
// mirrored by the WebApp.
func TestScheduleDescription(t *testing.T) {
	base := func() reminders.Reminder {
		return reminders.Reminder{
			Kind:          reminders.Weekly,
			Interval:      1,
			Days:          []int{1, 4},
			StartDate:     schedule.NewDate(2026, 1, 5),
			TimeHHMM:      "09:00",
			Timezone:      "Europe/Moscow",
			RepeatMinutes: 10,
		}
	}

	tests := []struct {
		name     string
		reminder reminders.Reminder
		want     string
	}{
		{
			name:     "weekly",
			reminder: base(),
			want:     "каждую неделю по пн, чт в 09:00",
		},
		{
			name: "weekly every second week",
			reminder: func() reminders.Reminder {
				r := base()
				r.Interval = 2
				return r
			}(),
			want: "каждые 2 недели по пн, чт в 09:00",
		},
		{
			name: "daily",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Daily
				r.Days = nil
				return r
			}(),
			want: "каждый день в 09:00",
		},
		{
			name: "daily every third day",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Daily
				r.Interval = 3
				r.Days = nil
				return r
			}(),
			want: "каждые 3 дня в 09:00",
		},
		{
			name: "monthly day of month",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Monthly
				r.Days = []int{31}
				return r
			}(),
			want: "каждое 31-е число месяца в 09:00",
		},
		{
			name: "monthly several days",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Monthly
				r.Days = []int{1, 15, 30}
				return r
			}(),
			want: "каждый месяц 1, 15 и 30 числа в 09:00",
		},
		{
			name: "monthly second tuesday",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Monthly
				r.Days = nil
				r.MonthWeekPosition = 2
				r.MonthWeekday = 2
				return r
			}(),
			want: "каждый второй вторник месяца в 09:00",
		},
		{
			name: "monthly last friday",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Monthly
				r.Days = nil
				r.MonthWeekPosition = -1
				r.MonthWeekday = 5
				return r
			}(),
			want: "каждую последнюю пятницу месяца в 09:00",
		},
		{
			name: "monthly last sunday",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Monthly
				r.Days = nil
				r.MonthWeekPosition = -1
				r.MonthWeekday = 7
				return r
			}(),
			want: "каждое последнее воскресенье месяца в 09:00",
		},
		{
			name: "yearly",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Yearly
				r.Days = nil
				r.StartDate = schedule.NewDate(2026, 3, 20)
				return r
			}(),
			want: "каждый год 20 марта в 09:00",
		},
		{
			name: "once",
			reminder: func() reminders.Reminder {
				r := base()
				r.Kind = reminders.Once
				r.Days = nil
				r.StartDate = schedule.NewDate(2027, 3, 20)
				r.TimeHHMM = "10:00"
				return r
			}(),
			want: "20.03.2027 в 10:00 (один раз)",
		},
		{
			name: "until",
			reminder: func() reminders.Reminder {
				r := base()
				r.End = schedule.End{Kind: schedule.EndUntil, Date: schedule.NewDate(2026, 12, 31)}
				return r
			}(),
			want: "каждую неделю по пн, чт в 09:00 до 31.12.2026",
		},
		{
			name: "count",
			reminder: func() reminders.Reminder {
				r := base()
				r.End = schedule.End{Kind: schedule.EndCount, Count: 3}
				return r
			}(),
			want: "каждую неделю по пн, чт в 09:00 (всего 3 раза)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reminders.ScheduleDescription(tt.reminder); got != tt.want {
				t.Errorf("ScheduleDescription = %q, want %q", got, tt.want)
			}
		})
	}
}
