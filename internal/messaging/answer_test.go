package messaging

import "testing"

// The safety property is the ordering: anything that could be read either way
// must not come back as approval, because approval runs a write.
func TestParseAnswer(t *testing.T) {
	cases := []struct {
		text       string
		approve    bool
		understood bool
	}{
		{AnswerApprove, true, true},
		{AnswerDecline, false, true},

		{"yes", true, true},
		{"Yes!", true, true},
		{"yes please do", true, true},
		{"ok", true, true},
		{"go ahead", true, true},
		{"sure, confirm it", true, true},

		{"no", false, true},
		{"No.", false, true},
		{"no thanks", false, true},
		{"don't", false, true},
		{"stop", false, true},
		{"cancel that", false, true},

		// Both kinds of word present. Refusal wins, because the alternative is
		// writing something nobody agreed to.
		{"please don't", false, true},
		{"no, go ahead and skip it", false, true},

		{"what would that do exactly?", false, false},
		{"", false, false},
		{"tell me more first", false, false},
	}

	for _, c := range cases {
		approve, understood := parseAnswer(c.text)
		if approve != c.approve || understood != c.understood {
			t.Errorf("parseAnswer(%q) = (%v, %v), want (%v, %v)",
				c.text, approve, understood, c.approve, c.understood)
		}
	}
}

// Telegram numbers people positive and groups negative. A group id must never
// be bindable, because a group's id is shared by everybody in it.
func TestLinkableExternalID(t *testing.T) {
	cases := map[string]bool{
		"884422":      true,  // a person
		"1":           true,  // still a person
		"-1001234":    false, // supergroup
		"-99":         false, // group
		"0":           false, // not an id Telegram issues
		"":            false,
		"some-handle": true, // a future platform that does not use numbers
	}
	for id, want := range cases {
		if got := linkableExternalID(id); got != want {
			t.Errorf("linkableExternalID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestNormaliseCode(t *testing.T) {
	cases := map[string]string{
		"abc23xyz":        "ABC23XYZ",
		"  ABC23XYZ  ":    "ABC23XYZ",
		"abc2-3xyz":       "ABC23XYZ",
		"/start ABC23XYZ": "ABC23XYZ",
		"/link abc23xyz":  "ABC23XYZ",
	}
	for in, want := range cases {
		if got := normaliseCode(in); got != want {
			t.Errorf("normaliseCode(%q) = %q, want %q", in, got, want)
		}
	}
}

// The alphabet exists so a retyped code does not fail on a character nobody
// can tell apart. A regression here shows up as "that code is not valid".
func TestCodeAlphabetHasNoAmbiguousCharacters(t *testing.T) {
	for _, banned := range "01OIL" {
		for _, c := range codeAlphabet {
			if c == banned {
				t.Errorf("%q is in the code alphabet and is misread when retyped", c)
			}
		}
	}
}

func TestNewCodeIsTheRightShape(t *testing.T) {
	code, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	if len(code) != CodeLength {
		t.Fatalf("code is %d characters, want %d", len(code), CodeLength)
	}
	for _, c := range code {
		found := false
		for _, allowed := range codeAlphabet {
			if c == allowed {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("code %q contains %q, which is not in the alphabet", code, c)
		}
	}
}
