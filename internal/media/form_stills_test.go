package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"math"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/media/analysis"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// realJPEG is an image the upload can measure, unlike jpegBytes, which is only
// a signature.
func realJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (m *memStorage) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objs)
}

func mp4Bytes() []byte {
	return append([]byte("\x00\x00\x00\x18ftypmp42"), make([]byte, 64)...)
}

type formFixture struct {
	svc   *media.Service
	queue *jobs.Queue
	user  users.User
	store *memStorage
}

func newFormFixture(t *testing.T, vision ai.Client) formFixture {
	t.Helper()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "lifter@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Lifter",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	store := newMemStorage()
	queue := jobs.NewQueue(pool)
	svc := media.NewService(media.Options{
		Repository: media.NewRepository(pool),
		Storage:    store,
		Queue:      queue,
		Vision:     vision,
	})
	return formFixture{svc: svc, queue: queue, user: user, store: store}
}

func formUpload(t *testing.T, times ...float64) media.FormUpload {
	t.Helper()
	clip := mp4Bytes()
	in := media.FormUpload{
		Filename:        "squat.mp4",
		Size:            int64(len(clip)),
		Body:            bytes.NewReader(clip),
		DurationSeconds: 8,
		ExerciseSlug:    "back-squat",
	}
	for _, at := range times {
		in.Frames = append(in.Frames, media.FrameUpload{AtSeconds: at, Body: bytes.NewReader(realJPEG(t, 64, 48))})
	}
	return in
}

// The whole point of stills: the issue the model names by frame number comes
// back at the exact second the client cut that frame, and nothing the request
// carries is something Haiku 5.5 refuses.
func TestAFormCheckIsReadFromItsStills(t *testing.T) {
	vision := fake.Text(`{"exercise":"back squat","confidence":"high","summary":"Solid set.","issues":[
		{"frame":3,"severity":"moderate","observation":"Hips are above the knees while the chest is down.","correction":"Lead with the chest."},
		{"frame":9,"severity":"minor","observation":"Names a still that was never sent.","correction":"-"}]}`)
	fx := newFormFixture(t, vision)
	ctx := context.Background()

	pending, err := fx.svc.UploadVideo(ctx, fx.user.ID, formUpload(t, 0.5, 2.5, 4.5, 6.5))
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != analysis.StatusPending || pending.DurationSeconds != 8 || pending.ExerciseSlug != "back-squat" {
		t.Fatalf("pending = %+v", pending)
	}

	job, ok, err := fx.queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err = fx.svc.AnalyzeVideo(ctx, job.Payload); err != nil {
		t.Fatal(err)
	}

	done, err := fx.svc.GetAnalysis(ctx, pending.ID, fx.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !done.IsDone() || done.Result == nil {
		t.Fatalf("analysis = %+v", done)
	}
	if done.Result.Method != analysis.MethodStills || done.Provider != "fake" {
		t.Errorf("method=%q provider=%q", done.Result.Method, done.Provider)
	}
	if len(done.Result.Issues) != 1 {
		t.Fatalf("issues = %+v, want only the one that names a real still", done.Result.Issues)
	}
	if got := done.Result.Issues[0]; got.Frame != 3 || got.TimestampSeconds != 4.5 {
		t.Errorf("issue anchored at frame %d, %.1f s; want frame 3, 4.5 s", got.Frame, got.TimestampSeconds)
	}

	req := vision.LastCall()
	if req.Temperature != nil {
		t.Error("the stills request sets a temperature; Haiku 5.5 answers that with a 400")
	}
	images := 0
	for _, part := range req.Messages[0].Parts {
		if len(part.InlineData) > 0 {
			images++
			if part.MIMEType != "image/jpeg" {
				t.Errorf("still sent as %q", part.MIMEType)
			}
		}
	}
	if images != 4 {
		t.Errorf("model saw %d stills, want 4", images)
	}
	if len(vision.Uploads()) != 0 {
		t.Error("the clip was uploaded to the provider; stills must be the only thing sent")
	}

	links, err := fx.svc.FrameLinks(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 4 || links[2].Index != 3 || links[2].AtSeconds != 4.5 || links[2].URL == "" {
		t.Errorf("links = %+v", links)
	}
}

// One bad reply is asked again; the second answer is used.
func TestAFormCheckAsksAgainWhenTheReplyIsNotJSON(t *testing.T) {
	vision := fake.New(
		fake.Response{Text: "Looks like a squat to me!"},
		fake.Response{Text: `{"exercise":"squat","confidence":"low","summary":"The angle hides the hips.","issues":[]}`},
	)
	fx := newFormFixture(t, vision)
	ctx := context.Background()

	pending, err := fx.svc.UploadVideo(ctx, fx.user.ID, formUpload(t, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := fx.queue.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.svc.AnalyzeVideo(ctx, job.Payload); err != nil {
		t.Fatal(err)
	}
	done, _ := fx.svc.GetAnalysis(ctx, pending.ID, fx.user.ID)
	if !done.IsDone() || done.Result.Confidence != analysis.ConfidenceLow {
		t.Fatalf("analysis = %+v", done)
	}
	if n := len(vision.Calls()); n != 2 {
		t.Errorf("%d calls, want 2", n)
	}
}

// A build that sends video only, where nothing reads video, is told to update
// before 90 MB is stored for a check that could never finish.
func TestAVideoWithNoStillsIsRefusedWhereNothingReadsVideo(t *testing.T) {
	fx := newFormFixture(t, fake.Text("{}"))

	_, err := fx.svc.UploadVideo(context.Background(), fx.user.ID, formUpload(t))
	var fields apperr.FieldErrors
	if !errors.As(err, &fields) || len(fields) != 1 || fields[0].Field != "video" || !strings.Contains(fields[0].Message, "Update") {
		t.Fatalf("err = %v", err)
	}
	if fx.store.count() != 0 {
		t.Error("the clip was stored although the upload was refused")
	}
}

func TestStillsAreCheckedBeforeAnythingIsStored(t *testing.T) {
	good := func(t *testing.T) []byte { return realJPEG(t, 64, 48) }
	frame := func(at float64, data []byte) media.FrameUpload {
		return media.FrameUpload{AtSeconds: at, Body: bytes.NewReader(data)}
	}
	var many []media.FrameUpload

	cases := []struct {
		name   string
		mutate func(t *testing.T, in *media.FormUpload)
		want   string
	}{
		{"too many", func(t *testing.T, in *media.FormUpload) {
			for i := range analysis.MaxFrames + 1 {
				many = append(many, frame(float64(i)*0.1, good(t)))
			}
			in.Frames = many
		}, "at most"},
		{"out of order", func(t *testing.T, in *media.FormUpload) {
			in.Frames = []media.FrameUpload{frame(3, good(t)), frame(2, good(t))}
		}, "clip order"},
		{"same moment twice", func(t *testing.T, in *media.FormUpload) {
			in.Frames = []media.FrameUpload{frame(2, good(t)), frame(2, good(t))}
		}, "clip order"},
		{"past the end", func(t *testing.T, in *media.FormUpload) {
			in.Frames = []media.FrameUpload{frame(9, good(t))}
		}, "clip order"},
		{"not a number", func(t *testing.T, in *media.FormUpload) {
			in.Frames = []media.FrameUpload{frame(math.NaN(), good(t))}
		}, "clip order"},
		{"no length", func(t *testing.T, in *media.FormUpload) {
			in.DurationSeconds = 0
		}, "length"},
		{"endless clip", func(t *testing.T, in *media.FormUpload) {
			in.DurationSeconds = math.Inf(1)
		}, "length"},
		{"a PNG", func(t *testing.T, in *media.FormUpload) {
			in.Frames = []media.FrameUpload{frame(1, pngBytes())}
		}, "JPEG"},
		{"too large an edge", func(t *testing.T, in *media.FormUpload) {
			in.Frames = []media.FrameUpload{frame(1, realJPEG(t, media.MaxFrameEdge+1, 16))}
		}, "pixels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFormFixture(t, fake.Text("{}"))
			in := formUpload(t, 1, 2)
			tc.mutate(t, &in)

			_, err := fx.svc.UploadVideo(context.Background(), fx.user.ID, in)
			var fields apperr.FieldErrors
			if !errors.As(err, &fields) || len(fields) != 1 || fields[0].Field != "frames" {
				t.Fatalf("err = %v, want a refusal on frames", err)
			}
			if !strings.Contains(fields[0].Message, tc.want) {
				t.Errorf("message %q does not mention %q", fields[0].Message, tc.want)
			}
			if fx.store.count() != 0 {
				t.Error("something was stored although the stills were refused")
			}
		})
	}
}
