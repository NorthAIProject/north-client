package i18n

import (
	"context"
	"strings"
	"time"
)

// Date formats t with a Go layout in the request's locale. See FormatTime.
func Date(ctx context.Context, t time.Time, layout string) string {
	return FormatTime(LocaleFrom(ctx), t, layout)
}

// FormatTime formats t with a Go layout, naming months and weekdays in tag's
// language.
//
// Callers keep the layout they already had ("2 Jan", "Mon 2 Jan, 15:04"), so
// converting a template is a one-word change. The layout's English names are
// swapped for placeholders before time.Format runs and for the locale's names
// after, because time.Format only ever speaks English.
//
// Word order mostly survives translation: "2 ene" and "seg 2 jan" read
// naturally. The few layouts that do not are rewritten in layoutsFor first.
func FormatTime(tag string, t time.Time, layout string) string {
	names, ok := dateNames[tag]
	if !ok {
		return t.Format(layout)
	}
	if rewritten, ok := layoutsFor[tag][layout]; ok {
		layout = rewritten
	}
	// Longest token first: "January" contains "Jan", "Monday" contains "Mon".
	// The placeholders are control characters, which are not layout tokens and
	// cannot appear in any layout a template writes.
	layout = strings.NewReplacer(
		"January", "\x01",
		"Monday", "\x02",
		"Jan", "\x03",
		"Mon", "\x04",
	).Replace(layout)
	out := t.Format(layout)
	return strings.NewReplacer(
		"\x01", names.months[t.Month()-1],
		"\x02", names.weekdays[t.Weekday()],
		"\x03", names.shortMonths[t.Month()-1],
		"\x04", names.shortWeekdays[t.Weekday()],
	).Replace(out)
}

type calendarNames struct {
	months, shortMonths     [12]string
	weekdays, shortWeekdays [7]string // Sunday first, as time.Weekday counts
}

var (
	spanishNames = calendarNames{
		months:        [12]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
		shortMonths:   [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"},
		weekdays:      [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
		shortWeekdays: [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
	}
	portugueseNames = calendarNames{
		months:        [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"},
		shortMonths:   [12]string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"},
		weekdays:      [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"},
		shortWeekdays: [7]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"},
	}
)

// dateNames has no English entry: English falls through to time.Format.
var dateNames = map[string]calendarNames{
	"es":    spanishNames,
	"pt-PT": portugueseNames,
	"pt-BR": portugueseNames,
}

// romanceLayouts puts a full month name between "de"s and drops the American
// month-first order, which no reader of these languages expects.
var romanceLayouts = map[string]string{
	"2 January 2006":         "2 de January de 2006",
	"2 January 2006, 15:04":  "2 de January de 2006, 15:04",
	"2 January, 15:04":       "2 de January, 15:04",
	"Jan 2":                  "2 Jan",
	"Jan 2, 2006 3:04 PM":    "2 Jan 2006, 15:04",
	"Mon, Jan 2":             "Mon, 2 Jan",
	"Monday, January 2":      "Monday, 2 de January",
	"Monday, 2 January 2006": "Monday, 2 de January de 2006",
}

var layoutsFor = map[string]map[string]string{
	"es":    romanceLayouts,
	"pt-PT": romanceLayouts,
	"pt-BR": romanceLayouts,
}
