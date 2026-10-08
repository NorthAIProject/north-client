package planimport

import (
	"bytes"
	"strings"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestOpenRefusesWithAReasonAndAFieldError(t *testing.T) {
	t.Parallel()

	cfb := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 512)...)

	cases := []struct {
		name     string
		filename string
		data     []byte
		want     Reason
	}{
		{"empty", "plan.csv", nil, ReasonEmpty},
		{"whitespace only", "plan.txt", []byte("  \n\t "), ReasonEmpty},
		{"too large", "plan.csv", bytes.Repeat([]byte("a"), MaxBytes+1), ReasonTooLarge},
		{"unknown extension", "plan.pages", []byte("hello"), ReasonUnsupported},
		{"zip archive", "plans.zip", []byte("PK\x03\x04"), ReasonUnsupported},
		{"legacy xls", "plan.xls", cfb, ReasonUnsupported},
		{"encrypted xlsx", "plan.xlsx", cfb, ReasonPasswordProtected},
		{"encrypted docx", "plan.docx", cfb, ReasonPasswordProtected},
		{"xlsx that is not a zip", "plan.xlsx", []byte("day,exercise"), ReasonUnreadable},
		{"pdf that is not a pdf", "plan.pdf", []byte("day,exercise"), ReasonUnreadable},
		{"encrypted pdf", "plan.pdf", minimalPDF(t, []string{"Squat 3x5"}, true), ReasonPasswordProtected},
		{"pdf over the page limit", "plan.pdf", minimalPDF(t, make21Pages(), false), ReasonTooLong},
		{"scanned pdf", "plan.pdf", minimalPDF(t, []string{" "}, false), ReasonUnreadable},
		{"invalid json", "plan.json", []byte("{days:"), ReasonUnreadable},
		{"not utf-8", "plan.txt", []byte{0xff, 0xfe, 0x00, 0x41}, ReasonUnreadable},
		{"text over the length limit", "plan.md", []byte(strings.Repeat("squat ", MaxTextChars/5)), ReasonTooLong},
		{"image that is not an image", "plan.jpg", []byte("not a jpeg at all"), ReasonUnreadable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Open(tc.filename, tc.data)
			if got := ReasonOf(err); got != tc.want {
				t.Fatalf("reason = %q (err %v), want %q", got, err, tc.want)
			}

			// Every refusal has to reach the person next to the upload
			// control, which is what a FieldErrors on "file" does.
			var fields apperr.FieldErrors
			if !apperr.As(err, &fields) || fields.Messages()["file"] == "" {
				t.Fatalf("err %v does not carry a field error on file", err)
			}
			if !apperr.Is(err, apperr.ErrValidation) {
				t.Fatalf("err %v is not a validation error", err)
			}
		})
	}
}

func make21Pages() []string {
	pages := make([]string, MaxPDFPages+1)
	for i := range pages {
		pages[i] = "Squat 3x5"
	}
	return pages
}

func TestOpenReadsEachKind(t *testing.T) {
	t.Parallel()

	heic := append([]byte{0, 0, 0, 24}, []byte("ftypheic\x00\x00\x00\x00mif1heic")...)

	cases := []struct {
		name     string
		filename string
		data     []byte
		kind     Kind
		check    func(t *testing.T, s Source)
	}{
		{"csv with bom", "plan.CSV", []byte("\xEF\xBB\xBFday,exercise\nMon,Squat\n"), KindCSV, func(t *testing.T, s Source) {
			if s.Rows[0][0] != "day" || s.Rows[1][1] != "Squat" {
				t.Fatalf("rows = %q", s.Rows)
			}
		}},
		{"tsv", "plan.tsv", []byte("day\texercise\nMon\tBench, close grip\n"), KindTSV, func(t *testing.T, s Source) {
			if s.Rows[1][1] != "Bench, close grip" {
				t.Fatalf("rows = %q", s.Rows)
			}
		}},
		{"xlsx reads the first sheet only", "plan.xlsx", xlsxFile(t,
			[][]string{{"Day", "Exercise"}, {"Mon", "Squat"}},
			[][]string{{"Day", "Exercise"}, {"Tue", "Deadlift"}},
		), KindXLSX, func(t *testing.T, s Source) {
			if len(s.Rows) != 2 || s.Rows[1][1] != "Squat" {
				t.Fatalf("rows = %q, want only the first sheet", s.Rows)
			}
		}},
		{"json", "plan.json", []byte(`[{"day":"Mon","exercises":[]}]`), KindJSON, nil},
		{"pdf text layer", "plan.pdf", minimalPDF(t, []string{"Monday Squat 3x5", "Thursday Bench 3x8"}, false), KindPDF, func(t *testing.T, s Source) {
			if !strings.Contains(s.Text, "Squat") || !strings.Contains(s.Text, "Bench") {
				t.Fatalf("text = %q", s.Text)
			}
		}},
		{"docx keeps table cells apart", "plan.docx", docxFile(t, []string{"Block 3"}, [][]string{{"Day", "Exercise", "Sets"}, {"Mon", "Squat", "3"}}), KindDOCX, func(t *testing.T, s Source) {
			if !strings.Contains(s.Text, "Block 3\n") || !strings.Contains(s.Text, "Mon\tSquat\t3") {
				t.Fatalf("text = %q", s.Text)
			}
		}},
		{"markdown", "plan.md", []byte("# Push day\n- Bench 3x8"), KindText, nil},
		{"png", "photo.png", pngPixel, KindImage, func(t *testing.T, s Source) {
			if s.MIME != "image/png" {
				t.Fatalf("mime = %q", s.MIME)
			}
		}},
		{"heic", "IMG_0001.HEIC", heic, KindImage, func(t *testing.T, s Source) {
			if s.MIME != "image/heic" {
				t.Fatalf("mime = %q", s.MIME)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src, err := Open(tc.filename, tc.data)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if src.Kind != tc.kind {
				t.Fatalf("kind = %q, want %q", src.Kind, tc.kind)
			}
			if tc.check != nil {
				tc.check(t, src)
			}
		})
	}
}
