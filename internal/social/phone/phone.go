// Package phone turns what somebody types into an E.164 number and proves
// they own it with a texted code. It knows nothing about accounts: the social
// service decides what a verified number is for.
package phone

import (
	"errors"
	"strings"
)

// ErrInvalid is a number that cannot be E.164 whatever country it is read in.
var ErrInvalid = errors.New("not a phone number")

// ErrNeedsCountry is a national number typed without a country to read it in.
var ErrNeedsCountry = errors.New("phone number needs a country code")

// Country is one choice in the calling-code select.
type Country struct {
	// Dial is the calling code without the plus: "351".
	Dial string
	Name string
}

// Countries is the short select offered beside a national number. It is a
// convenience, not a limit: any country works by typing the number with its
// "+" or "00" prefix.
var Countries = []Country{
	{"351", "Portugal"},
	{"55", "Brazil"},
	{"34", "Spain"},
	{"44", "United Kingdom"},
	{"1", "United States / Canada"},
	{"33", "France"},
	{"49", "Germany"},
	{"39", "Italy"},
	{"31", "Netherlands"},
	{"32", "Belgium"},
	{"353", "Ireland"},
	{"41", "Switzerland"},
	{"52", "Mexico"},
	{"54", "Argentina"},
	{"244", "Angola"},
	{"258", "Mozambique"},
	{"61", "Australia"},
	{"91", "India"},
}

// KnownDial reports whether dial is one of Countries.
func KnownDial(dial string) bool {
	for _, c := range Countries {
		if c.Dial == dial {
			return true
		}
	}
	return false
}

// Normalize returns raw as E.164 ("+351912345678"). raw may be
// "+<digits>", "00<digits>", or a national number read in the country whose
// calling code is dial (digits only, no plus). Spaces, dashes, dots and
// parentheses are ignored.
//
// A national number drops one leading trunk 0 ("07911 123456" in the UK is
// +447911123456), except in Italy, where the 0 is part of the number.
//
// This is a shape check, not a guarantee the number exists: the texted code
// is what proves that.
func Normalize(raw, dial string) (string, error) {
	s := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', '(', ')', ' ':
			return -1
		}
		return r
	}, strings.TrimSpace(raw))

	var digits string
	switch {
	case strings.HasPrefix(s, "+"):
		digits = s[1:]
	case strings.HasPrefix(s, "00"):
		digits = s[2:]
	default:
		dial = strings.TrimPrefix(strings.TrimSpace(dial), "+")
		if s == "" || !allDigits(s) {
			return "", ErrInvalid
		}
		if dial == "" {
			return "", ErrNeedsCountry
		}
		if !allDigits(dial) {
			return "", ErrInvalid
		}
		if dial != "39" {
			s = strings.TrimPrefix(s, "0")
		}
		digits = dial + s
	}
	if !allDigits(digits) || len(digits) < 8 || len(digits) > 15 || digits[0] == '0' {
		return "", ErrInvalid
	}
	return "+" + digits, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// DefaultDial guesses the calling code to preselect from an account's
// timezone, then its language. Empty when neither says: the select then
// starts unchosen rather than guessing wrong.
func DefaultDial(timezone, locale string) string {
	if dial, ok := timezoneDial[timezone]; ok {
		return dial
	}
	switch locale {
	case "pt-PT":
		return "351"
	case "pt-BR":
		return "55"
	}
	return ""
}

// timezoneDial maps the zones of Countries that name one country.
var timezoneDial = map[string]string{
	"Europe/Lisbon": "351", "Atlantic/Madeira": "351", "Atlantic/Azores": "351",
	"Europe/Madrid": "34", "Atlantic/Canary": "34",
	"Europe/London": "44",
	"Europe/Paris":  "33", "Europe/Berlin": "49", "Europe/Rome": "39",
	"Europe/Amsterdam": "31", "Europe/Brussels": "32", "Europe/Dublin": "353",
	"Europe/Zurich":    "41",
	"America/New_York": "1", "America/Chicago": "1", "America/Denver": "1",
	"America/Los_Angeles": "1", "America/Phoenix": "1", "America/Anchorage": "1",
	"Pacific/Honolulu": "1", "America/Toronto": "1", "America/Vancouver": "1",
	"America/Mexico_City":            "52",
	"America/Argentina/Buenos_Aires": "54",
	"Africa/Luanda":                  "244", "Africa/Maputo": "258",
	"Australia/Sydney": "61", "Australia/Melbourne": "61", "Australia/Perth": "61",
	"Asia/Kolkata": "91", "Asia/Calcutta": "91",
	"America/Sao_Paulo": "55", "America/Bahia": "55", "America/Fortaleza": "55",
	"America/Recife": "55", "America/Belem": "55", "America/Manaus": "55",
	"America/Cuiaba": "55", "America/Campo_Grande": "55", "America/Porto_Velho": "55",
	"America/Boa_Vista": "55", "America/Rio_Branco": "55", "America/Maceio": "55",
	"America/Araguaina": "55", "America/Santarem": "55", "America/Noronha": "55",
}
