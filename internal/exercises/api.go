package exercises

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// ArtCredit is the attribution the pose artwork's licence requires wherever
// it is shown. The same words the web and Telegram carry.
const ArtCredit = "Illustration: Bryl Lim / Everkinetic, CC BY-SA 4.0"

// API exposes the exercise catalog to native clients.
//
// The pose artwork goes out as SVG path data rather than as files to fetch.
// Each frame is a single white path, so a client can draw it as a native
// shape: tinted to its own theme, animated between frames, and sharp at any
// size, with no image decoding and nothing bundled into the app.
type API struct {
	svc *Service
	art fs.FS
}

// NewAPI serves catalog entries, with artwork read from art (the embedded
// web/assets tree; frames live under exercises/<slug>/frame-N.svg.gz).
// Mount behind auth.RequireBearer.
func NewAPI(svc *Service, art fs.FS) *API {
	return &API{svc: svc, art: art}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/exercises", a.browse)
	r.Get("/exercises/{slug}", a.show)
}

// ExerciseSummary is a catalog entry in a list: enough to choose one.
type ExerciseSummary struct {
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	Equipment  string   `json:"equipment"`
	Difficulty string   `json:"difficulty"`
	Primary    []string `json:"primaryMuscles"`
	HasArt     bool     `json:"hasArt"`
}

type ExerciseList struct {
	Exercises []ExerciseSummary `json:"exercises"`
	// Total is how many match, for paging; a suggestion list leaves it 0.
	Total int `json:"total"`
}

// ProjectList is the list shape, shared with training's suggestion routes.
func ProjectList(found []Exercise) ExerciseList {
	out := ExerciseList{Exercises: make([]ExerciseSummary, 0, len(found))}
	for _, e := range found {
		out.Exercises = append(out.Exercises, ExerciseSummary{
			Slug: e.Slug, Name: e.Name, Category: e.Category, Equipment: e.Equipment,
			Difficulty: e.Difficulty, Primary: nonNil(e.Primary), HasArt: e.HasIllustration(),
		})
	}
	return out
}

// browse searches the catalog: ?q=, ?muscle=, ?category=, ?equipment= (may
// repeat), ?limit= (default 30, at most 100) and ?offset=.
func (a *API) browse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	found, total, err := a.svc.Search(r.Context(), Filter{
		Query: q.Get("q"), Muscle: q.Get("muscle"), Category: q.Get("category"),
		Equipment: q["equipment"], Limit: limit, Offset: max(offset, 0),
	})
	if err != nil {
		httpx.Error(w, err, "The exercise library could not be searched.")
		return
	}
	out := ProjectList(found)
	out.Total = total
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ExerciseDetail is one catalog entry with its artwork.
type ExerciseDetail struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Equipment    string   `json:"equipment"`
	Difficulty   string   `json:"difficulty"`
	Instructions string   `json:"instructions"`
	VideoURL     string   `json:"videoUrl,omitempty"`
	Primary      []string `json:"primaryMuscles"`
	Secondary    []string `json:"secondaryMuscles"`
	Art          *Art     `json:"art,omitempty"`
}

// Art is the three pose frames of a movement, drawn in a square of Size
// points. Fill each path in the even-odd rule; the source art is white on
// transparent, so the fill colour is the client's choice.
type Art struct {
	Size   int      `json:"size"`
	Frames []string `json:"frames"`
	Credit string   `json:"credit"`
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	e, err := a.svc.GetBySlug(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		httpx.Error(w, err, "That exercise was not found.")
		return
	}

	detail := ExerciseDetail{
		Slug:         e.Slug,
		Name:         e.Name,
		Category:     e.Category,
		Equipment:    e.Equipment,
		Difficulty:   e.Difficulty,
		Instructions: e.Instructions,
		VideoURL:     e.VideoURL,
		Primary:      nonNil(e.Primary),
		Secondary:    nonNil(e.Secondary),
	}
	if e.HasIllustration() {
		art, err := a.frames(e.IllustrationSlug)
		if err != nil {
			httpx.Error(w, apperr.Wrap(err, "read exercise art"), "The exercise artwork could not be read.")
			return
		}
		detail.Art = art
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

// artFrameCount is how many poses every movement has; scripts/workout-guide-art
// writes exactly three.
const artFrameCount = 3

// The artwork's shape, which the frames are checked against rather than
// assumed: a client that draws path data needs to know it has the whole
// picture in one path, in a known box.
var (
	artViewBox = regexp.MustCompile(`viewBox="0 0 (\d+) (\d+)"`)
	artPath    = regexp.MustCompile(`<path[^>]*\sd="([^"]+)"`)
)

func (a *API) frames(illustration string) (*Art, error) {
	art := &Art{Credit: ArtCredit, Frames: make([]string, 0, artFrameCount)}
	for i := 1; i <= artFrameCount; i++ {
		svg, err := readGzip(a.art, fmt.Sprintf("exercises/%s/frame-%d.svg.gz", illustration, i))
		if err != nil {
			return nil, err
		}
		box := artViewBox.FindStringSubmatch(svg)
		paths := artPath.FindAllStringSubmatch(svg, -1)
		if box == nil || box[1] != box[2] || len(paths) != 1 {
			return nil, fmt.Errorf("%s frame %d: want one path in a square viewBox", illustration, i)
		}
		var size int
		_, _ = fmt.Sscan(box[1], &size)
		if art.Size != 0 && art.Size != size {
			return nil, fmt.Errorf("%s: frames disagree on size", illustration)
		}
		art.Size = size
		art.Frames = append(art.Frames, paths[0][1])
	}
	return art, nil
}

func readGzip(fsys fs.FS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()
	b, err := io.ReadAll(zr)
	return string(b), err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
