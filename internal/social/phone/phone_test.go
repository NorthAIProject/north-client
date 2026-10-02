package phone_test

import (
	"errors"
	"testing"

	"github.com/NorthAIProject/north-client/internal/social/phone"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw, dial, want string
		err             error
	}{
		{raw: "+351 912 345 678", want: "+351912345678"},
		{raw: "00351912345678", want: "+351912345678"},
		{raw: "00 351 (912) 345-678", want: "+351912345678"},
		{raw: "912 345 678", dial: "351", want: "+351912345678"},
		{raw: "912.345.678", dial: "+351", want: "+351912345678"},
		// The country is ignored once the number carries its own.
		{raw: "+44 7911 123456", dial: "351", want: "+447911123456"},
		// A national trunk 0 is dropped...
		{raw: "07911 123456", dial: "44", want: "+447911123456"},
		{raw: "(11) 91234-5678", dial: "55", want: "+5511912345678"},
		// ...except in Italy, where it belongs to the number.
		{raw: "06 1234 5678", dial: "39", want: "+390612345678"},
		{raw: "(415) 555-0100", dial: "1", want: "+14155550100"},

		{raw: "912 345 678", err: phone.ErrNeedsCountry},
		{raw: "", dial: "351", err: phone.ErrInvalid},
		{raw: "+", err: phone.ErrInvalid},
		{raw: "+351 912 abc", err: phone.ErrInvalid},
		{raw: "ana@north.test", dial: "351", err: phone.ErrInvalid},
		{raw: "+1234567", err: phone.ErrInvalid},          // 7 digits: too short
		{raw: "+1234567890123456", err: phone.ErrInvalid}, // 16 digits: too long
		{raw: "+0351912345678", err: phone.ErrInvalid},    // no calling code starts with 0
		{raw: "12345", dial: "1", err: phone.ErrInvalid},
		{raw: "912345678", dial: "3a", err: phone.ErrInvalid},
	} {
		got, err := phone.Normalize(tc.raw, tc.dial)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("Normalize(%q, %q) = %q, %v; want error %v", tc.raw, tc.dial, got, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q, %q) = %q, %v; want %q", tc.raw, tc.dial, got, err, tc.want)
		}
	}
}

func TestDefaultDial(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ tz, locale, want string }{
		{"Europe/Lisbon", "en", "351"},
		{"America/Sao_Paulo", "en", "55"},
		{"Europe/London", "pt-PT", "44"},
		// The timezone says nothing; the language does.
		{"UTC", "pt-BR", "55"},
		{"UTC", "pt-PT", "351"},
		// Neither says: no guess.
		{"UTC", "en", ""},
		{"Asia/Tokyo", "es", ""},
	} {
		if got := phone.DefaultDial(tc.tz, tc.locale); got != tc.want {
			t.Errorf("DefaultDial(%q, %q) = %q, want %q", tc.tz, tc.locale, got, tc.want)
		}
	}
	for _, c := range phone.Countries {
		if !phone.KnownDial(c.Dial) {
			t.Errorf("%s is listed but not known", c.Name)
		}
	}
}
