package reminders_test

import (
	"os"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
)

// readFixture loads a synthetic export from testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// legacyRowBySource finds a parsed row by its old reminder id.
func legacyRowBySource(t *testing.T, export reminders.LegacyExport, sourceID string) reminders.LegacyRow {
	t.Helper()
	for _, row := range export.Rows {
		if row.SourceReminderID == sourceID {
			return row
		}
	}
	t.Fatalf("row %s is not in the parsed export: %+v", sourceID, export.Rows)
	return reminders.LegacyRow{}
}

// TestParseLegacyCSV covers the CSV export of the n8n Data Table: header row,
// RFC 4180 quoting, the weekly and monthly forms of `days`, a deleted row, a
// multiline message, doubled quotes and emoji.
func TestParseLegacyCSV(t *testing.T) {
	export, err := reminders.ParseLegacyExport(readFixture(t, "legacy_reminders.csv"), reminders.LegacyFormatCSV)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if export.Format != reminders.LegacyFormatCSV {
		t.Errorf("format = %q, want csv", export.Format)
	}
	if export.Total != 7 {
		t.Errorf("total = %d, want 7", export.Total)
	}
	if export.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", export.Deleted)
	}
	if len(export.Rows) != 4 {
		t.Fatalf("valid rows = %d, want 4: %+v", len(export.Rows), export.Rows)
	}
	if len(export.Invalid) != 2 {
		t.Fatalf("invalid rows = %d, want 2: %+v", len(export.Invalid), export.Invalid)
	}

	// The invalid rows are reported by record number: the header is record 1.
	byRow := make(map[int]string, len(export.Invalid))
	for _, issue := range export.Invalid {
		byRow[issue.Row] = issue.Reason
	}
	if reason, ok := byRow[5]; !ok || !strings.Contains(reason, "Время") {
		t.Errorf("row 5 reason = %q, want the time explanation", reason)
	}
	if reason, ok := byRow[6]; !ok || !strings.Contains(reason, "часовой пояс") {
		t.Errorf("row 6 reason = %q, want the time zone explanation", reason)
	}

	weekly := legacyRowBySource(t, export, "r-1")
	if weekly.TelegramUserID != "1001" {
		t.Errorf("telegram user = %q, want 1001", weekly.TelegramUserID)
	}
	if weekly.Input.Kind != schedule.Weekly {
		t.Errorf("kind = %q, want weekly", weekly.Input.Kind)
	}
	if got := weekly.Input.Days; len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Errorf("days = %v, want [1 3 5]", got)
	}
	if weekly.Input.TimeHHMM != "09:30" {
		t.Errorf("time = %q, want 09:30", weekly.Input.TimeHHMM)
	}
	if weekly.Input.RepeatMinutes != 15 {
		t.Errorf("repeat = %d, want 15", weekly.Input.RepeatMinutes)
	}
	if weekly.Input.Timezone != "Europe/Moscow" {
		t.Errorf("timezone = %q, want Europe/Moscow", weekly.Input.Timezone)
	}
	if weekly.Input.Message != "Позвонить маме 👩‍👧" {
		t.Errorf("message = %q, want the emoji text", weekly.Input.Message)
	}
	if !weekly.Input.Enabled {
		t.Error("a weekly row is disabled")
	}

	monthly := legacyRowBySource(t, export, "r-2")
	if monthly.Input.Kind != schedule.Monthly {
		t.Errorf("kind = %q, want monthly", monthly.Input.Kind)
	}
	if got := monthly.Input.Days; len(got) != 2 || got[0] != 19 || got[1] != 31 {
		t.Errorf("days = %v, want [19 31]", got)
	}
	if !strings.Contains(monthly.Input.Message, "\n") {
		t.Errorf("multiline message lost its newline: %q", monthly.Input.Message)
	}
	if monthly.Input.TimeHHMM != "20:05" {
		t.Errorf("time = %q, want 20:05", monthly.Input.TimeHHMM)
	}

	// A quoted message keeps its inner double quotes.
	quoted := legacyRowBySource(t, export, "r-6")
	if quoted.Input.Message != `Сказать "привет" 🏋️` {
		t.Errorf("message = %q, want the quoted text", quoted.Input.Message)
	}
	if got := quoted.Input.Days; len(got) != 1 || got[0] != 7 {
		t.Errorf("days = %v, want [7]", got)
	}
	if quoted.Input.RepeatMinutes != 1440 {
		t.Errorf("repeat = %d, want 1440", quoted.Input.RepeatMinutes)
	}

	// A disabled rule stays disabled; it is imported so the user can switch it
	// back on.
	disabled := legacyRowBySource(t, export, "r-7")
	if disabled.Input.Enabled {
		t.Error("an enabled=false row was imported as enabled")
	}
	if disabled.Input.Timezone != "Europe/Berlin" {
		t.Errorf("timezone = %q, want Europe/Berlin", disabled.Input.Timezone)
	}
}

// TestParseLegacyJSON covers the JSON export: typed values (a numeric user id, a
// numeric and a string repeat interval, booleans) and the same rule set.
func TestParseLegacyJSON(t *testing.T) {
	export, err := reminders.ParseLegacyExport(readFixture(t, "legacy_reminders.json"), reminders.LegacyFormatJSON)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if export.Format != reminders.LegacyFormatJSON {
		t.Errorf("format = %q, want json", export.Format)
	}
	if export.Total != 5 || export.Deleted != 1 || len(export.Rows) != 2 || len(export.Invalid) != 2 {
		t.Fatalf("export = %+v, want 5 rows (1 deleted, 2 valid, 2 invalid)", export)
	}

	first := legacyRowBySource(t, export, "j-1")
	// A numeric user id is normalised to text, which is what a binding stores.
	if first.TelegramUserID != "2001" {
		t.Errorf("telegram user = %q, want 2001", first.TelegramUserID)
	}
	if first.Input.Kind != schedule.Weekly || first.Input.RepeatMinutes != 20 {
		t.Errorf("row = %+v, want weekly with a 20 minute repeat", first.Input)
	}

	second := legacyRowBySource(t, export, "j-2")
	if second.Input.Kind != schedule.Monthly {
		t.Errorf("kind = %q, want monthly", second.Input.Kind)
	}
	if got := second.Input.Days; len(got) != 2 || got[0] != 1 || got[1] != 15 {
		t.Errorf("days = %v, want [1 15]", got)
	}
	if second.Input.RepeatMinutes != 30 {
		t.Errorf("repeat = %d, want 30 (a string value)", second.Input.RepeatMinutes)
	}
	if !strings.Contains(second.Input.Message, "😀") || !strings.Contains(second.Input.Message, "\n") {
		t.Errorf("message = %q, want the multiline emoji text", second.Input.Message)
	}

	// The invalid rows carry the record index of the array (1-based).
	rows := make(map[int]bool, len(export.Invalid))
	for _, issue := range export.Invalid {
		rows[issue.Row] = true
	}
	if !rows[4] || !rows[5] {
		t.Errorf("invalid rows = %+v, want 4 and 5", export.Invalid)
	}
}

// TestParseLegacyAutoDetectsFormat proves --format auto reads both shapes.
func TestParseLegacyAutoDetectsFormat(t *testing.T) {
	csv, err := reminders.ParseLegacyExport(readFixture(t, "legacy_reminders.csv"), reminders.LegacyFormatAuto)
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if csv.Format != reminders.LegacyFormatCSV {
		t.Errorf("format = %q, want csv", csv.Format)
	}
	if csv.Total != 7 {
		t.Errorf("total = %d, want 7", csv.Total)
	}

	json, err := reminders.ParseLegacyExport(readFixture(t, "legacy_reminders.json"), reminders.LegacyFormatAuto)
	if err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if json.Format != reminders.LegacyFormatJSON {
		t.Errorf("format = %q, want json", json.Format)
	}
	if json.Total != 5 {
		t.Errorf("total = %d, want 5", json.Total)
	}

	// A UTF-8 BOM before the header must not become part of the first column.
	withBOM := append([]byte("\xef\xbb\xbf"), readFixture(t, "legacy_reminders.csv")...)
	withBOMExport, err := reminders.ParseLegacyExport(withBOM, reminders.LegacyFormatAuto)
	if err != nil {
		t.Fatalf("parse with BOM: %v", err)
	}
	if len(withBOMExport.Rows) != 4 {
		t.Errorf("BOM export rows = %d, want 4", len(withBOMExport.Rows))
	}
}

// TestParseLegacyRejectsBrokenInput covers the file-level errors.
func TestParseLegacyRejectsBrokenInput(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		format reminders.LegacyFormat
		want   string
	}{
		{
			name:   "empty file",
			data:   "  \n",
			format: reminders.LegacyFormatAuto,
			want:   "empty",
		},
		{
			name:   "broken json",
			data:   "[{\"reminder_id\":]",
			format: reminders.LegacyFormatJSON,
			want:   "JSON",
		},
		{
			name:   "csv without header",
			data:   "",
			format: reminders.LegacyFormatCSV,
			want:   "empty",
		},
		{
			name:   "unknown format",
			data:   "a,b\n",
			format: "xlsx",
			want:   "unknown export format",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := reminders.ParseLegacyExport([]byte(tt.data), tt.format)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestParseLegacyRowErrors covers the row-level rejections with their reasons.
func TestParseLegacyRowErrors(t *testing.T) {
	header := "reminder_id,user_id,message,timezone,days,time_hhmm,repeat_minutes,enabled\n"
	tests := []struct {
		name string
		row  string
		want string
	}{
		{
			name: "missing reminder id",
			row:  ",1001,Сообщение,UTC,1,10:00,10,true\n",
			want: "reminder_id is empty",
		},
		{
			name: "bad telegram user id",
			row:  "r-1,@someone,Сообщение,UTC,1,10:00,10,true\n",
			want: "not a Telegram user id",
		},
		{
			name: "empty days",
			row:  "r-2,1001,Сообщение,UTC,,10:00,10,true\n",
			want: "days is empty",
		},
		{
			name: "weekly day out of range",
			row:  "r-3,1001,Сообщение,UTC,\"1,8\",10:00,10,true\n",
			want: "Дни недели",
		},
		{
			name: "monthly day out of range",
			row:  "r-4,1001,Сообщение,UTC,m:32,10:00,10,true\n",
			want: "Дни месяца",
		},
		{
			name: "not a number in days",
			row:  "r-5,1001,Сообщение,UTC,monday,10:00,10,true\n",
			want: "not a list of numbers",
		},
		{
			name: "repeat out of range",
			row:  "r-6,1001,Сообщение,UTC,1,10:00,0,true\n",
			want: "Интервал повтора",
		},
		{
			name: "repeat is not a number",
			row:  "r-7,1001,Сообщение,UTC,1,10:00,часто,true\n",
			want: "whole number",
		},
		{
			name: "enabled is not a boolean",
			row:  "r-8,1001,Сообщение,UTC,1,10:00,10,может быть\n",
			want: "true or false",
		},
		{
			name: "empty message",
			row:  "r-9,1001,,UTC,1,10:00,10,true\n",
			want: "Сообщение",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			export, err := reminders.ParseLegacyExport([]byte(header+tt.row), reminders.LegacyFormatCSV)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(export.Rows) != 0 {
				t.Fatalf("row was accepted: %+v", export.Rows)
			}
			if len(export.Invalid) != 1 {
				t.Fatalf("invalid = %+v, want one issue", export.Invalid)
			}
			if reason := export.Invalid[0].Reason; !strings.Contains(reason, tt.want) {
				t.Errorf("reason = %q, want it to mention %q", reason, tt.want)
			}
			if row := export.Invalid[0].Row; row != 2 {
				t.Errorf("row = %d, want 2 (the first data record)", row)
			}
		})
	}
}
