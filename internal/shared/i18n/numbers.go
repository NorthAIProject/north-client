package i18n

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// usesDecimalComma lists the locales that write "72,5 kg" rather than
// "72.5 kg". English is the only one served that uses a point.
//
// Decimal and FormatDecimal change only the decimal separator. Most of the
// app writes "6200 kcal" ungrouped in every language; a place that does group
// thousands uses Thousands, so the grouping mark never collides with the
// decimal one ("12.345" in Spanish, "12,345" in English).
var usesDecimalComma = map[string]bool{
	"es":    true,
	"pt-PT": true,
	"pt-BR": true,
}

// Decimal formats v to prec decimal places for the request's locale. See
// FormatDecimal.
func Decimal(ctx context.Context, v float64, prec int) string {
	return FormatDecimal(LocaleFrom(ctx), v, prec)
}

// FormatDecimal formats v to prec decimal places with the separator tag's
// readers expect.
//
// It is for text a person reads. A number a machine reads back — an input's
// value, a CSS width, an SVG coordinate, a data attribute — must keep the
// point, or the browser parses "72,5" as nothing; those stay on strconv.
func FormatDecimal(tag string, v float64, prec int) string {
	return localizeSeparator(tag, strconv.FormatFloat(v, 'f', prec, 64))
}

// Thousands groups n in threes for the request's locale. See FormatThousands.
func Thousands(ctx context.Context, n int) string {
	return FormatThousands(LocaleFrom(ctx), n)
}

// FormatThousands groups n in threes with the mark tag's readers expect: a
// comma in English, a point where the decimal mark is a comma.
func FormatThousands(tag string, n int) string {
	sep := byte(',')
	if usesDecimalComma[tag] {
		sep = '.'
	}
	digits := strconv.Itoa(n)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	b.WriteString(sign)
	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(sep)
		}
		b.WriteByte(digits[i])
	}
	return b.String()
}

func localizeSeparator(tag, formatted string) string {
	if !usesDecimalComma[tag] {
		return formatted
	}
	return strings.Replace(formatted, ".", ",", 1)
}

// localFloat formats a float argument of Tf or TranslateF with the locale's
// decimal separator, honouring whatever verb, width and precision the
// catalogue string asked for. Wrapping the argument rather than rewriting the
// finished sentence is what keeps the point in "v1.2" or "e.g." intact.
type localFloat struct {
	v   float64
	tag string
}

func (f localFloat) Format(s fmt.State, verb rune) {
	_, _ = io.WriteString(s, localizeSeparator(f.tag, fmt.Sprintf(fmt.FormatString(s, verb), f.v)))
}

// localizeArgs wraps every float in args for tag. Other arguments pass through
// untouched, and so does everything when tag reads a decimal point.
func localizeArgs(tag string, args []any) []any {
	if !usesDecimalComma[tag] {
		return args
	}
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case float64:
			out[i] = localFloat{v: v, tag: tag}
		case float32:
			out[i] = localFloat{v: float64(v), tag: tag}
		default:
			out[i] = a
		}
	}
	return out
}
