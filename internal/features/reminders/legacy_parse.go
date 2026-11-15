package reminders

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/maksimkurb/cublyverse/internal/features/reminders/schedule"
)

// LegacyFormat is the shape of an export of the old n8n Data Table
// `telegram_reminders_v2`.
type LegacyFormat string

const (
	// LegacyFormatCSV is the CSV export of the Data Table: a header row and
	// RFC 4180 quoting.
	LegacyFormatCSV LegacyFormat = "csv"
	// LegacyFormatJSON is an array of row objects, as the n8n API returns them.
	LegacyFormatJSON LegacyFormat = "json"
	// LegacyFormatAuto guesses from the first non-space byte of the file.
	LegacyFormatAuto LegacyFormat = "auto"
)

// LegacyRow is one valid configuration row of the old table: the reminder rule
// together with the Telegram account that owned it.
type LegacyRow struct {
	// SourceReminderID is the reminder_id of the old row; it identifies the row
	// across imports.
	SourceReminderID string
	// TelegramUserID is the user_id of the old bot.
	TelegramUserID string
	// Input is the validated, canonical rule ready for the reminders service.
	Input Input
}

// LegacyIssue is one row that the import has to skip.
type LegacyIssue struct {
	// Row is the position in the export: the record number of the CSV file
	// (the header is record 1) or the 1-based index in the JSON array.
	Row int
	// Reason is the user-facing explanation, in Russian.
	Reason string
}

// LegacyExport is a parsed export of the old table.
type LegacyExport struct {
	// Format is the format that was parsed, never LegacyFormatAuto.
	Format LegacyFormat
	// Total is how many data rows the export contains.
	Total int
	// Deleted is how many rows are marked deleted in the old table.
	Deleted int
	// Rows are the valid records that become reminders.
	Rows []LegacyRow
	// Invalid lists the skipped records and why.
	Invalid []LegacyIssue
}

// Column names of the old Data Table that the import reads. n8n adds its own
// id/createdAt/updatedAt columns, and the in-flight columns of the workflow
// (awaiting_confirmation, locks, message ids) are ignored: only the rule is
// imported.
const (
	legacyColumnReminderID    = "reminder_id"
	legacyColumnUserID        = "user_id"
	legacyColumnMessage       = "message"
	legacyColumnTimezone      = "timezone"
	legacyColumnDays          = "days"
	legacyColumnTime          = "time_hhmm"
	legacyColumnRepeat        = "repeat_minutes"
	legacyColumnEnabled       = "enabled"
	legacyColumnDeleted       = "deleted"
	legacyMonthlyDaysPrefix   = "m:"
	legacyCSVFirstDataRecord  = 2
	legacyMaxTelegramUserIDLn = 32
)

// ParseLegacyExport parses and validates an export of the old n8n Data Table.
//
// Every row is checked with the reminder rules of the service (the same
// canonicalize call the API and the bot use), so a row that would be rejected
// when the user creates a reminder is rejected here with the same explanation.
// The parse never touches the database: the caller decides whether to import.
func ParseLegacyExport(data []byte, format LegacyFormat) (LegacyExport, error) {
	// Excel writes a UTF-8 BOM before the header, which would otherwise become
	// part of the first column name.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if len(bytes.TrimSpace(data)) == 0 {
		return LegacyExport{}, errors.New("the export is empty")
	}

	if format == "" || format == LegacyFormatAuto {
		format = detectLegacyFormat(data)
	}

	var (
		rawRows []legacyRawRow
		err     error
	)
	switch format {
	case LegacyFormatCSV:
		rawRows, err = decodeLegacyCSV(data)
	case LegacyFormatJSON:
		rawRows, err = decodeLegacyJSON(data)
	default:
		return LegacyExport{}, fmt.Errorf("unknown export format %q, want csv, json or auto", format)
	}
	if err != nil {
		return LegacyExport{}, err
	}

	export := LegacyExport{Format: format, Total: len(rawRows)}
	for _, raw := range rawRows {
		row, deleted, err := legacyRowFromFields(raw.fields)
		switch {
		case err != nil:
			export.Invalid = append(export.Invalid, LegacyIssue{Row: raw.position, Reason: err.Error()})
		case deleted:
			export.Deleted++
		default:
			export.Rows = append(export.Rows, row)
		}
	}
	return export, nil
}

// detectLegacyFormat guesses the format from the first non-space byte: a JSON
// export is an array, everything else is read as CSV.
func detectLegacyFormat(data []byte) LegacyFormat {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		return LegacyFormatJSON
	}
	return LegacyFormatCSV
}

// legacyRawRow is one row of the export as a map of columns to JSON values.
// Both formats are converted to it, so the field parsing and its error messages
// are shared: a CSV cell becomes a JSON string, a JSON export keeps its types.
type legacyRawRow struct {
	position int
	fields   map[string]json.RawMessage
}

// decodeLegacyCSV reads the RFC 4180 export. The header row names the columns,
// so their order does not matter, and quoted multi-line messages work.
func decodeLegacyCSV(data []byte) ([]legacyRawRow, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	// n8n writes the columns it has; extra or missing optional ones are handled
	// by the header map instead of a fixed record width.
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("the CSV export has no header row")
	}
	if err != nil {
		return nil, fmt.Errorf("read the CSV header: %w", err)
	}

	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(strings.ToLower(name))] = index
	}

	var rows []legacyRawRow
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV record %d: %w", legacyCSVFirstDataRecord+len(rows), err)
		}
		fields := make(map[string]json.RawMessage, len(columns))
		for name, index := range columns {
			if index >= len(record) {
				continue
			}
			encoded, err := json.Marshal(record[index])
			if err != nil {
				return nil, fmt.Errorf("encode CSV column %q: %w", name, err)
			}
			fields[name] = encoded
		}
		rows = append(rows, legacyRawRow{position: legacyCSVFirstDataRecord + len(rows), fields: fields})
	}
	return rows, nil
}

// decodeLegacyJSON reads an array of row objects. Values may be strings, numbers
// or booleans, because an n8n export keeps the column types.
func decodeLegacyJSON(data []byte) ([]legacyRawRow, error) {
	var objects []map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&objects); err != nil {
		return nil, fmt.Errorf("decode the JSON export as an array of rows: %w", err)
	}

	rows := make([]legacyRawRow, 0, len(objects))
	for index, object := range objects {
		fields := make(map[string]json.RawMessage, len(object))
		for name, value := range object {
			fields[strings.TrimSpace(strings.ToLower(name))] = value
		}
		rows = append(rows, legacyRawRow{position: index + 1, fields: fields})
	}
	return rows, nil
}

// legacyRowFromFields turns one export row into a validated rule. It reports
// whether the row is marked deleted in the old table; such rows are skipped
// before anything else is read.
func legacyRowFromFields(fields map[string]json.RawMessage) (LegacyRow, bool, error) {
	row := legacyRowOf(fields)

	deleted, _, err := row.boolean(legacyColumnDeleted)
	if err != nil {
		return LegacyRow{}, false, err
	}
	if deleted {
		return LegacyRow{}, true, nil
	}

	sourceID := strings.TrimSpace(row.text(legacyColumnReminderID))
	if sourceID == "" {
		return LegacyRow{}, false, errors.New("reminder_id is empty")
	}
	telegramUserID, err := legacyTelegramUserID(row.text(legacyColumnUserID))
	if err != nil {
		return LegacyRow{}, false, err
	}

	input := Input{
		Message:       row.text(legacyColumnMessage),
		Timezone:      row.text(legacyColumnTimezone),
		TimeHHMM:      row.text(legacyColumnTime),
		RepeatMinutes: DefaultRepeatMinutes,
		Enabled:       true,
	}
	kind, days, err := parseLegacyDays(row.text(legacyColumnDays))
	if err != nil {
		return LegacyRow{}, false, err
	}
	input.Kind, input.Days = kind, days

	if value, ok, err := row.integer(legacyColumnRepeat); err != nil {
		return LegacyRow{}, false, err
	} else if ok {
		input.RepeatMinutes = value
	}
	if value, ok, err := row.boolean(legacyColumnEnabled); err != nil {
		return LegacyRow{}, false, err
	} else if ok {
		input.Enabled = value
	}

	// The same validation the API and the bot run, so the import cannot accept a
	// rule the service would refuse.
	validated, err := canonicalize(input)
	if err != nil {
		var validation *ValidationError
		if errors.As(err, &validation) {
			return LegacyRow{}, false, errors.New(validation.Message)
		}
		return LegacyRow{}, false, err
	}
	input = Input{
		Message:       validated.message,
		Timezone:      validated.timezone,
		Kind:          validated.kind,
		Days:          validated.days,
		TimeHHMM:      validated.hhmm,
		RepeatMinutes: validated.repeatMinutes,
		Enabled:       validated.enabled,
	}
	return LegacyRow{SourceReminderID: sourceID, TelegramUserID: telegramUserID, Input: input}, false, nil
}

// legacyRowOf wraps the raw columns of one row.
type legacyRowOf map[string]json.RawMessage

// text reads a column as a string. A missing or null column is empty, a number
// and a boolean are rendered as text, which is what a CSV export of a typed
// column looks like.
func (r legacyRowOf) text(name string) string {
	value := r[name]
	if len(value) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return text
	}
	var number json.Number
	if err := json.Unmarshal(value, &number); err == nil {
		return number.String()
	}
	var boolean bool
	if err := json.Unmarshal(value, &boolean); err == nil {
		return strconv.FormatBool(boolean)
	}
	return ""
}

// integer reads a column as an integer. ok is false for a missing or empty
// column, so the caller can apply its default.
func (r legacyRowOf) integer(name string) (value int, ok bool, err error) {
	text := strings.TrimSpace(r.text(name))
	if text == "" {
		return 0, false, nil
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		// A JSON number may arrive as 10.0; an integral float is still a number.
		float, floatErr := strconv.ParseFloat(text, 64)
		if floatErr != nil || float != float64(int(float)) {
			return 0, false, fmt.Errorf("%s must be a whole number, got %q", name, text)
		}
		parsed = int(float)
	}
	return parsed, true, nil
}

// boolean reads a column as a boolean. ok is false for a missing or empty
// column. The n8n export writes true/false, older exports wrote 1/0.
func (r legacyRowOf) boolean(name string) (value bool, ok bool, err error) {
	text := strings.TrimSpace(r.text(name))
	if text == "" {
		return false, false, nil
	}
	switch strings.ToLower(text) {
	case "true", "1", "yes", "y":
		return true, true, nil
	case "false", "0", "no", "n":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("%s must be true or false, got %q", name, text)
	}
}

// legacyTelegramUserID validates the account id of the old bot. It has to look
// like a Telegram user id: anything else could never be claimed by a binding, so
// importing it would only create a row nobody sees.
func legacyTelegramUserID(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("user_id is empty")
	}
	if len(trimmed) > legacyMaxTelegramUserIDLn {
		return "", fmt.Errorf("user_id %q is not a Telegram user id", trimmed)
	}
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("user_id %q is not a Telegram user id", trimmed)
	}
	return strconv.FormatInt(id, 10), nil
}

// parseLegacyDays converts the `days` column of the old table: a CSV of ISO
// weekdays ("1,2,3,7") or days of month after the "m:" marker ("m:19,31").
func parseLegacyDays(value string) (schedule.Kind, []int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil, errors.New("days is empty")
	}
	kind := schedule.Weekly
	if strings.HasPrefix(strings.ToLower(trimmed), legacyMonthlyDaysPrefix) {
		kind = schedule.Monthly
		trimmed = trimmed[len(legacyMonthlyDaysPrefix):]
	}

	parts := strings.Split(trimmed, ",")
	days := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", nil, fmt.Errorf("days %q has an empty value", value)
		}
		day, err := strconv.Atoi(part)
		if err != nil {
			return "", nil, fmt.Errorf("days %q is not a list of numbers", value)
		}
		days = append(days, day)
	}
	return kind, schedule.NormalizeDays(days), nil
}
