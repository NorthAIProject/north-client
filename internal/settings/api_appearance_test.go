package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type appearanceSpy struct{ surfaces []string }

func (s *appearanceSpy) AppearanceChanged(_ context.Context, _ uuid.UUID, _, surface string) {
	s.surfaces = append(s.surfaces, surface)
}

// profileAPI is the profile half of the native API over a real database. The
// handler only touches users.Service, so that is all it is given.
func profileAPI(t *testing.T) (*API, *users.Service, *appearanceSpy, users.User) {
	t.Helper()
	pool := testdb.New(t)
	spy := &appearanceSpy{}
	svc := users.NewService(users.NewRepository(pool)).WithAppearanceRecorder(spy)
	user, err := svc.Register(context.Background(), users.Registration{
		Email: "api-appearance@north.test", DisplayName: "Ana", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return NewAPI(&Handler{users: svc}), svc, spy, user
}

func call(t *testing.T, a *API, svc *users.Service, userID uuid.UUID, method, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	// Re-read so the context carries the stored row, as RequireBearer's would.
	user, err := svc.ByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("read user: %v", err)
	}
	req := httptest.NewRequest(method, "/settings/profile", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	if method == http.MethodGet {
		a.getProfile(rec, req)
	} else {
		a.putProfile(rec, req)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const profileWithout = `{"email":"api-appearance@north.test","displayName":"Ana","timezone":"Europe/Lisbon","locale":"en","coachingStyle":"","coachingTone":"direct"}`

func profileBody(appearance string) string {
	return strings.TrimSuffix(profileWithout, "}") + `,"appearance":"` + appearance + `"}`
}

// iOS applies its own default to "never chosen", so the server must not
// report the web's default for somebody who never picked.
func TestGetProfileOmitsAnAppearanceNeverChosen(t *testing.T) {
	a, svc, _, user := profileAPI(t)

	rec, out := call(t, a, svc, user.ID, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if _, present := out["appearance"]; present {
		t.Fatalf("appearance = %v, want it absent", out["appearance"])
	}
}

// The shipped iOS builds PUT the whole profile without the field. Absent means
// unchanged — not cleared, and not counted as a choice.
func TestPutProfileWithoutAppearanceKeepsTheStoredOne(t *testing.T) {
	a, svc, spy, user := profileAPI(t)

	if rec, out := call(t, a, svc, user.ID, http.MethodPut, profileBody("light")); rec.Code != http.StatusOK || out["appearance"] != "light" {
		t.Fatalf("set: status %d body %s", rec.Code, rec.Body)
	}
	rec, out := call(t, a, svc, user.ID, http.MethodPut, profileWithout)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if out["appearance"] != "light" {
		t.Fatalf("appearance = %v after a PUT without it, want light kept", out["appearance"])
	}
	if _, out = call(t, a, svc, user.ID, http.MethodGet, ""); out["appearance"] != "light" {
		t.Fatalf("GET appearance = %v, want light", out["appearance"])
	}
	if len(spy.surfaces) != 1 || spy.surfaces[0] != "ios" {
		t.Fatalf("recorded %v, want one change from ios", spy.surfaces)
	}
}

func TestPutProfileRefusesAnUnknownAppearance(t *testing.T) {
	a, svc, _, user := profileAPI(t)

	rec, out := call(t, a, svc, user.ID, http.MethodPut, profileBody("sepia"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422 (the spec's ValidationError): %s", rec.Code, rec.Body)
	}
	detail, _ := out["error"].(map[string]any)
	fields, _ := detail["fields"].(map[string]any)
	if fields["appearance"] == nil {
		t.Fatalf("error = %v, want a message on appearance", out)
	}
}
