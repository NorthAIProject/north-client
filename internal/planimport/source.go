package planimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Limits. They bound what one import can cost — in memory here, and in tokens
// when the file goes to a model — not what a plan may contain.
const (
	// MaxBytes is the largest file accepted.
	MaxBytes = 10 << 20

	// MaxPDFPages caps a PDF. A training block or a week of meals fits on a
	// handful of pages; twenty is generous and still a bounded prompt.
	MaxPDFPages = 20

	// MaxTextChars is MaxPDFPages expressed as text, for DOCX, TXT and MD: about
	// three thousand characters a page.
	MaxTextChars = MaxPDFPages * 3000

	// MaxRows caps a spreadsheet. A year of daily sessions is under four
	// hundred rows; a sheet past this is a log export, not a plan.
	MaxRows = 2000
)

// Kind is what a file turned out to be once its bytes were checked.
type Kind string

const (
	KindCSV   Kind = "csv"
	KindTSV   Kind = "tsv"
	KindXLSX  Kind = "xlsx"
	KindJSON  Kind = "json"
	KindPDF   Kind = "pdf"
	KindDOCX  Kind = "docx"
	KindText  Kind = "text"
	KindImage Kind = "image"

	// KindPDFDocument is a PDF whose text layer yields nothing: a scan, or an
	// export (Canva's, for one) the text reader cannot follow. The model reads
	// the PDF itself, as it reads a photo.
	KindPDFDocument Kind = "pdf_document"
)

// Accepted is the list shown when a file is refused, and the accept attribute
// of the upload controls. Legacy .xls is deliberately absent: see read.
const Accepted = ".pdf,.docx,.txt,.md,.csv,.tsv,.xlsx,.json,.jpg,.jpeg,.png,.webp,.heic"

// Source is a file after it has been opened and checked, ready to be read.
//
// Exactly one of Rows, Text, JSON, Image or Document is set, according to
// Kind. The split decides who reads it: rows and JSON have a stated shape and
// are mapped by code, with no model involved; text, images and PDF documents
// have no shape and go to a model. MIME is set with Image and Document.
type Source struct {
	Kind     Kind
	Filename string

	Rows     [][]string
	Text     string
	JSON     []byte
	Image    []byte
	Document []byte
	MIME     string
}

// Structured reports whether the file has a stated shape that code can map
// without a model.
func (s Source) Structured() bool {
	return s.Kind == KindCSV || s.Kind == KindTSV || s.Kind == KindXLSX || s.Kind == KindJSON
}

// Open checks a file and extracts what it says.
//
// The extension picks the reader and the bytes have to agree with it. A
// renamed file is refused rather than guessed at: a .pdf that is really a
// spreadsheet would otherwise fail somewhere deep inside the PDF reader with an
// error that names neither.
func Open(filename string, data []byte) (Source, error) {
	if len(data) == 0 || len(bytes.TrimSpace(data)) == 0 {
		return Source{}, refuse(ReasonEmpty, "This file is empty.")
	}
	if len(data) > MaxBytes {
		return Source{}, refuse(ReasonTooLarge, fmt.Sprintf("This file is %.1f MB. The limit is 10 MB.", float64(len(data))/(1<<20)))
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	src := Source{Filename: filepath.Base(filename)}

	switch ext {
	case "csv", "tsv":
		text, err := plainText(data)
		if err != nil {
			return Source{}, err
		}
		delimiter := ','
		src.Kind = KindCSV
		if ext == "tsv" {
			delimiter, src.Kind = '\t', KindTSV
		}
		src.Rows, err = readDelimited(text, delimiter)
		return src, err

	case "xlsx", "xlsm":
		if err := checkOfficeContainer(data, "spreadsheet"); err != nil {
			return Source{}, err
		}
		rows, err := readXLSX(data)
		src.Kind, src.Rows = KindXLSX, rows
		return src, err

	case "xls":
		// The binary Excel format predates the zip-based one by a decade and
		// needs its own reader. Every Excel, Numbers and Sheets saves .xlsx, so
		// asking for that is one click for the person and one dependency fewer.
		return Source{}, refuse(ReasonUnsupported, "Old .xls spreadsheets can't be read. Save it as .xlsx or .csv and try again.")

	case "json":
		if !json.Valid(data) {
			return Source{}, refuse(ReasonUnreadable, "This JSON file is not valid JSON.")
		}
		src.Kind, src.JSON = KindJSON, data
		return src, nil

	case "pdf":
		text, err := readPDF(data)
		if errors.Is(err, errNoTextLayer) {
			src.Kind, src.Document, src.MIME = KindPDFDocument, data, "application/pdf"
			return src, nil
		}
		src.Kind, src.Text = KindPDF, text
		return src, err

	case "docx":
		if err := checkOfficeContainer(data, "document"); err != nil {
			return Source{}, err
		}
		text, err := readDOCX(data)
		if err != nil {
			return Source{}, err
		}
		src.Kind, src.Text = KindDOCX, text
		return src, checkTextLength(text)

	case "txt", "md", "markdown", "text":
		text, err := plainText(data)
		if err != nil {
			return Source{}, err
		}
		src.Kind, src.Text = KindText, text
		return src, checkTextLength(text)

	case "jpg", "jpeg", "png", "webp", "heic", "heif":
		mime, ok := imageMIME(data)
		if !ok {
			return Source{}, refuse(ReasonUnreadable, "This doesn't look like a real image file.")
		}
		src.Kind, src.Image, src.MIME = KindImage, data, mime
		return src, nil

	default:
		return Source{}, refuse(ReasonUnsupported, "This file type isn't supported. Use PDF, DOCX, TXT, MD, CSV, TSV, XLSX, JSON, or a JPG, PNG, WEBP or HEIC photo.")
	}
}

// cfbMagic opens an OLE compound file. A modern Office file is a zip; when one
// arrives as a compound file instead, Office has encrypted it — that is what a
// password-protected .docx or .xlsx is on disk.
var cfbMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

func checkOfficeContainer(data []byte, noun string) error {
	switch {
	case bytes.HasPrefix(data, cfbMagic):
		return refuse(ReasonPasswordProtected, fmt.Sprintf("This %s is password-protected. Remove the password and try again.", noun))
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return nil
	default:
		return refuse(ReasonUnreadable, fmt.Sprintf("This doesn't look like a real %s file.", noun))
	}
}

// plainText accepts UTF-8, with or without a byte-order mark, which is what
// every spreadsheet and editor exports today.
func plainText(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if !utf8.Valid(data) {
		return "", refuse(ReasonUnreadable, "This file isn't readable text. Save it as UTF-8 and try again.")
	}
	return string(data), nil
}

func checkTextLength(text string) error {
	if strings.TrimSpace(text) == "" {
		return refuse(ReasonEmpty, "This file has no text in it.")
	}
	if utf8.RuneCountInString(text) > MaxTextChars {
		return refuse(ReasonTooLong, fmt.Sprintf("This file is too long to import. The limit is about %d pages.", MaxPDFPages))
	}
	return nil
}

// imageMIME sniffs an image. http.DetectContentType knows JPEG, PNG and WEBP;
// HEIC it does not, so its ftyp box is read directly.
func imageMIME(data []byte) (string, bool) {
	switch mime := http.DetectContentType(data); mime {
	case "image/jpeg", "image/png", "image/webp":
		return mime, true
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "heic", "heix", "heim", "heis", "mif1", "msf1":
			return "image/heic", true
		}
	}
	return "", false
}
