package media

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func framesForm(t *testing.T, stills int, fields map[string]string) *multipart.Form {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for range stills {
		part, err := w.CreateFormFile("frames", "frame.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("jpeg")); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form
}

func TestStillsArePairedWithTheirTimes(t *testing.T) {
	form := framesForm(t, 3, map[string]string{"frameTimes": "0.5, 1.5,2.5", "durationSeconds": "4"})

	var in FormUpload
	closeAll, err := readFrameParts(form, &in)
	defer closeAll()
	if err != nil {
		t.Fatal(err)
	}
	if in.DurationSeconds != 4 || len(in.Frames) != 3 {
		t.Fatalf("in = %+v", in)
	}
	for i, want := range []float64{0.5, 1.5, 2.5} {
		if in.Frames[i].AtSeconds != want {
			t.Errorf("still %d at %v, want %v", i+1, in.Frames[i].AtSeconds, want)
		}
		if data, _ := io.ReadAll(in.Frames[i].Body); string(data) != "jpeg" {
			t.Errorf("still %d body = %q", i+1, data)
		}
	}
}

func TestStillsWithoutMatchingTimesAreRefused(t *testing.T) {
	cases := map[string]map[string]string{
		"no length":      {"frameTimes": "1,2"},
		"too few times":  {"frameTimes": "1", "durationSeconds": "4"},
		"too many times": {"frameTimes": "1,2,3", "durationSeconds": "4"},
		"not a number":   {"frameTimes": "1,soon", "durationSeconds": "4"},
		"no times":       {"durationSeconds": "4"},
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			var in FormUpload
			closeAll, err := readFrameParts(framesForm(t, 2, fields), &in)
			defer closeAll()
			var refused apperr.FieldErrors
			if !errors.As(err, &refused) || refused[0].Field != "frames" {
				t.Fatalf("err = %v, want a refusal on frames", err)
			}
		})
	}
}

// A request with no stills is the old video-only upload, not an error here.
func TestNoStillsIsNotARefusal(t *testing.T) {
	var in FormUpload
	closeAll, err := readFrameParts(framesForm(t, 0, nil), &in)
	defer closeAll()
	if err != nil || len(in.Frames) != 0 {
		t.Fatalf("err = %v, frames = %d", err, len(in.Frames))
	}
}
