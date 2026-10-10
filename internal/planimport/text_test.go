package planimport

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractTextReadsEachKindAsText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		data     []byte
		want     string
	}{
		{"csv rows become tab-separated lines", "plan.csv", []byte("Day,Food\nMonday,\"rice, white\"\n"), "Day\tFood\nMonday\trice, white"},
		{"tsv", "plan.tsv", []byte("Day\tFood\nMonday\toats\n"), "Day\tFood\nMonday\toats"},
		{"xlsx", "plan.xlsx", xlsxFile(t, [][]string{{"Day", "Food"}, {"Monday", "eggs"}}), "Day\tFood\nMonday\teggs"},
		{"plain text", "plan.txt", []byte("Lunch: 150 g chicken\n"), "Lunch: 150 g chicken\n"},
		{"markdown", "plan.md", []byte("# Week\n- oats\n"), "# Week\n- oats\n"},
		{"json is passed as written", "plan.json", []byte(`{"days":[]}`), `{"days":[]}`},
		{"pdf", "plan.pdf", minimalPDF(t, []string{"Lunch chicken"}, false), "Lunch chicken"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractText(tc.filename, tc.data)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if tc.filename == "plan.pdf" {
				if !strings.Contains(got, tc.want) {
					t.Fatalf("text = %q, want it to contain %q", got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractTextRefusesAnImageAndKeepsTheFileLimits(t *testing.T) {
	if _, err := ExtractText("photo.png", pngPixel); !errors.Is(err, ErrNotText) {
		t.Fatalf("image: err = %v, want ErrNotText", err)
	}
	long := strings.Repeat("a", MaxTextChars+1)
	if _, err := ExtractText("plan.txt", []byte(long)); ReasonOf(err) != ReasonTooLong {
		t.Fatalf("long text: err = %v, want too long", err)
	}
	if _, err := ExtractText("plan.exe", []byte("MZ")); ReasonOf(err) != ReasonUnsupported {
		t.Fatalf("unsupported: err = %v", err)
	}
}

func TestExtractTextSaysWhenAPDFHasNoTextLayer(t *testing.T) {
	_, err := ExtractText("plano.pdf", minimalPDF(t, []string{" "}, false))
	if !errors.Is(err, ErrNoTextLayer) {
		t.Fatalf("err = %v, want ErrNoTextLayer", err)
	}
	if ReasonOf(err) != "" {
		t.Fatalf("err = %v is a refusal; a PDF without a text layer can still be imported", err)
	}
}
