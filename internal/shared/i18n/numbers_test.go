package i18n

import "testing"

func TestFormatDecimalUsesTheLocalesMark(t *testing.T) {
	cases := []struct {
		tag  string
		v    float64
		prec int
		want string
	}{
		{"en", 72.5, 1, "72.5"},
		{"es", 72.5, 1, "72,5"},
		{"pt-PT", 72.5, 1, "72,5"},
		{"pt-BR", 3.14159, 2, "3,14"},
		{"pt-PT", 6200, 0, "6200"},
		{"", 72.5, 1, "72.5"},
		{"fr", 72.5, 1, "72.5"},
	}
	for _, c := range cases {
		if got := FormatDecimal(c.tag, c.v, c.prec); got != c.want {
			t.Errorf("FormatDecimal(%q, %v, %d) = %q, want %q", c.tag, c.v, c.prec, got, c.want)
		}
	}
}

// The wrapper must honour the verb, width and precision the catalogue asked
// for, and leave every non-float argument and every literal point alone.
func TestTranslateFLocalizesOnlyFloatArguments(t *testing.T) {
	cases := []struct {
		tag    string
		format string
		args   []any
		want   string
	}{
		{"en", "%.1[1]f kg", []any{72.5}, "72.5 kg"},
		{"es", "%.1[1]f kg", []any{72.5}, "72,5 kg"},
		{"pt-PT", "%[2]s: %.1[1]f", []any{72.5, "v1.2"}, "v1.2: 72,5"},
		{"pt-BR", "%6.2[1]f|", []any{float32(1.5)}, "  1,50|"},
		{"es", "%[1]d of %.0[2]f", []any{3, 4.0}, "3 of 4"},
		{"es", "%[1]v", []any{0.25}, "0,25"},
	}
	for _, c := range cases {
		key := "test.numbers." + c.format
		english[key] = c.format
		got := TranslateF(c.tag, key, c.args...)
		delete(english, key)
		if got != c.want {
			t.Errorf("TranslateF(%q, %q, %v) = %q, want %q", c.tag, c.format, c.args, got, c.want)
		}
	}
}

func TestFormatThousandsGroupsWithTheLocalesMark(t *testing.T) {
	cases := []struct {
		tag  string
		n    int
		want string
	}{
		{"en", 12345, "12,345"},
		{"es", 12345, "12.345"},
		{"pt-PT", 1234567, "1.234.567"},
		{"pt-BR", 1000, "1.000"},
		{"en", 1000, "1,000"},
		{"es", 999, "999"},
		{"en", 0, "0"},
		{"en", -12345, "-12,345"},
	}
	for _, c := range cases {
		if got := FormatThousands(c.tag, c.n); got != c.want {
			t.Errorf("FormatThousands(%q, %d) = %q, want %q", c.tag, c.n, got, c.want)
		}
	}
}
