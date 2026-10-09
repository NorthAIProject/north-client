package planimport

import (
	"errors"
	"strings"
)

// ErrNotText is ExtractText's answer for a photo: it has no text to take out,
// and is read by a model looking at it instead.
var ErrNotText = errors.New("this file is an image, not text")

// ErrNoTextLayer is ExtractText's answer for a PDF whose text cannot be
// extracted. It is not a refusal: an import still reads it, by sending the
// PDF itself to the model.
var ErrNoTextLayer = errors.New("this PDF has no text that can be extracted")

// ExtractText is the text of a file, for a reader that wants the words rather
// than a plan: a document's text, a spreadsheet's rows as tab-separated
// lines, JSON as written.
//
// It opens the file exactly as an import does, so the same types are accepted
// and the same limits apply, and a refused file says why in the same words.
func ExtractText(filename string, data []byte) (string, error) {
	src, err := Open(filename, data)
	if err != nil {
		return "", err
	}
	switch src.Kind {
	case KindImage:
		return "", ErrNotText
	case KindPDFDocument:
		return "", ErrNoTextLayer
	case KindCSV, KindTSV, KindXLSX:
		lines := make([]string, len(src.Rows))
		for i, row := range src.Rows {
			lines[i] = strings.Join(row, "\t")
		}
		return strings.Join(lines, "\n"), nil
	case KindJSON:
		return string(src.JSON), nil
	default:
		return src.Text, nil
	}
}
