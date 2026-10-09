package planimport

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// readDelimited reads CSV or TSV. Ragged rows are allowed: a spreadsheet export
// routinely drops trailing empty cells, and refusing the file for that would
// refuse most real ones.
func readDelimited(text string, delimiter rune) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delimiter
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	var rows [][]string
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, refuse(ReasonUnreadable, fmt.Sprintf("This file couldn't be read as a table: %v.", err))
		}
		rows = append(rows, row)
		if len(rows) > MaxRows {
			return nil, refuse(ReasonTooLong, fmt.Sprintf("This file has more than %d rows. Split it and import one plan at a time.", MaxRows))
		}
	}
	return rows, nil
}

// readXLSX reads the first sheet only. A workbook's other sheets are as often
// last month's plan or a progress log as anything to import, and merging them
// silently would be worse than asking for the one that matters to come first.
func readXLSX(data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if errors.Is(err, excelize.ErrWorkbookPassword) {
		return nil, refuse(ReasonPasswordProtected, "This spreadsheet is password-protected. Remove the password and try again.")
	}
	if err != nil {
		return nil, refuse(ReasonUnreadable, "This spreadsheet couldn't be opened.")
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, refuse(ReasonEmpty, "This spreadsheet has no sheets.")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, refuse(ReasonUnreadable, "The first sheet of this spreadsheet couldn't be read.")
	}
	if len(rows) > MaxRows {
		return nil, refuse(ReasonTooLong, fmt.Sprintf("This sheet has more than %d rows. Split it and import one plan at a time.", MaxRows))
	}
	return rows, nil
}

// errNoTextLayer is readPDF's answer for a PDF that opened, within the page
// limit, but gave no words: Open keeps it as a document for the model.
var errNoTextLayer = errors.New("the PDF has no text layer")

// readPDF extracts a PDF's text layer, page by page.
func readPDF(data []byte) (string, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(data[:min(len(data), 1024)], " \t\r\n\x00"), []byte("%PDF")) {
		return "", refuse(ReasonUnreadable, "This doesn't look like a real PDF file.")
	}

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if errors.Is(err, pdf.ErrInvalidPassword) {
		return "", refuse(ReasonPasswordProtected, "This PDF is password-protected. Remove the password and try again.")
	}
	if err != nil {
		return "", refuse(ReasonUnreadable, "This PDF couldn't be opened.")
	}
	if reader.NumPage() > MaxPDFPages {
		return "", refuse(ReasonTooLong, fmt.Sprintf("This PDF has %d pages. The limit is %d.", reader.NumPage(), MaxPDFPages))
	}

	var (
		b        strings.Builder
		readable bool
	)
	for n := 1; n <= reader.NumPage(); n++ {
		page := reader.Page(n)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			// One unreadable page must not cost the rest. The model is told
			// about the gap rather than handed a plan that silently skips a day.
			text = fmt.Sprintf("[page %d could not be read]", n)
		} else if hasLetters(text) {
			readable = true
		}
		fmt.Fprintf(&b, "--- Page %d ---\n%s\n", n, text)
	}

	if !readable {
		// A scan, or an export whose text this reader cannot follow. Every
		// provider reads a PDF itself, so it goes to the model whole.
		return "", errNoTextLayer
	}
	return b.String(), nil
}

func hasLetters(s string) bool {
	return strings.IndexFunc(s, unicode.IsLetter) >= 0
}

// readDOCX pulls the text out of word/document.xml.
//
// The standard library is enough: a .docx is a zip, and the body is one XML
// file in which w:t holds text, w:p ends a paragraph, and w:tc/w:tr end a
// table cell and row. Tables are kept as tab-separated lines because a plan in
// Word is very often a table, and flattening its cells into one run of words
// would lose which number belonged to which exercise.
func readDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", refuse(ReasonUnreadable, "This document couldn't be opened.")
	}

	var body *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			body = f
			break
		}
	}
	if body == nil {
		return "", refuse(ReasonUnreadable, "This doesn't look like a real Word document.")
	}

	rc, err := body.Open()
	if err != nil {
		return "", refuse(ReasonUnreadable, "This document couldn't be opened.")
	}
	defer func() { _ = rc.Close() }()

	// The text cap bounds the output; this bounds the XML read to get there,
	// so a zip bomb disguised as a document stops early.
	dec := xml.NewDecoder(io.LimitReader(rc, 50<<20))

	var (
		out    []byte
		inCell int
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", refuse(ReasonUnreadable, "This document couldn't be read.")
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tc":
				inCell++
			case "tab":
				out = append(out, '\t')
			case "br", "cr":
				out = append(out, '\n')
			case "t":
				var text string
				if err := dec.DecodeElement(&text, &t); err != nil {
					return "", refuse(ReasonUnreadable, "This document couldn't be read.")
				}
				out = append(out, text...)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				// Paragraphs inside a cell are one value; elsewhere a line.
				if inCell > 0 {
					out = append(out, ' ')
				} else {
					out = append(out, '\n')
				}
			case "tc":
				inCell--
				out = append(bytes.TrimRight(out, " "), '\t')
			case "tr":
				out = append(bytes.TrimRight(out, "\t"), '\n')
			}
		}

		if len(out) > MaxTextChars*4 {
			break
		}
	}
	return string(out), nil
}
