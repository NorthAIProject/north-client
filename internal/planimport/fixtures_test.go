package planimport

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// Fixtures are built in code rather than checked in as binaries, so a reader
// can see exactly what each test feeds the importer.

// minimalPDF writes a valid PDF with one line of Helvetica text per page.
// encrypt adds an /Encrypt dictionary no empty password opens.
func minimalPDF(t *testing.T, pages []string, encrypt bool) []byte {
	t.Helper()

	var (
		buf     bytes.Buffer
		offsets []int
	)
	obj := func(body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}

	buf.WriteString("%PDF-1.4\n")
	n := len(pages)
	// 1 catalog, 2 pages, 3 font, then a page and a content stream per page.
	kids := make([]string, n)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+i*2)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, text := range pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+i*2))
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	trailerExtra := ""
	if encrypt {
		obj("<< /Filter /Standard /V 1 /R 2 /Length 40 /O <" + strings.Repeat("00", 32) + "> /U <" + strings.Repeat("11", 32) + "> /P -4 >>")
		trailerExtra = fmt.Sprintf(" /Encrypt %d 0 R /ID [<0123456789abcdef0123456789abcdef> <0123456789abcdef0123456789abcdef>]", len(offsets))
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, trailerExtra, xref)
	return buf.Bytes()
}

// xlsxFile writes a workbook whose sheets hold the given rows, in order.
func xlsxFile(t *testing.T, sheets ...[][]string) []byte {
	t.Helper()

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	for i, rows := range sheets {
		name := fmt.Sprintf("Sheet%d", i+1)
		if i > 0 {
			if _, err := f.NewSheet(name); err != nil {
				t.Fatalf("new sheet: %v", err)
			}
		}
		for r, row := range rows {
			for c, v := range row {
				cellName, _ := excelize.CoordinatesToCellName(c+1, r+1)
				if err := f.SetCellValue(name, cellName, v); err != nil {
					t.Fatalf("set cell: %v", err)
				}
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

// docxFile writes a minimal Word document: paragraphs, then one table.
func docxFile(t *testing.T, paragraphs []string, table [][]string) []byte {
	t.Helper()

	var body strings.Builder
	for _, p := range paragraphs {
		fmt.Fprintf(&body, `<w:p><w:r><w:t>%s</w:t></w:r></w:p>`, p)
	}
	if len(table) > 0 {
		body.WriteString("<w:tbl>")
		for _, row := range table {
			body.WriteString("<w:tr>")
			for _, c := range row {
				fmt.Fprintf(&body, `<w:tc><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:tc>`, c)
			}
			body.WriteString("</w:tr>")
		}
		body.WriteString("</w:tbl>")
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>%s</w:body></w:document>`, body.String())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngPixel is a valid 1x1 PNG.
var pngPixel = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}
