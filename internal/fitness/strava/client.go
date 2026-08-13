package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const (
	authURL      = "https://www.strava.com/oauth/authorize"
	tokenURL     = "https://www.strava.com/oauth/token"
	activitesURL = "https://www.strava.com/api/v3/athlete/activities"

	httpTimeout = 20 * time.Second

	// perPage is deliberately modest. Strava allows 100 read requests per 15
	// minutes and 1000 a day; one page per sync keeps a manual "Sync now"
	// far away from either limit, and the next sync picks up where this one
	// stopped.
	perPage = 50

	// firstSyncLookback bounds the initial backfill. Importing an athlete's
	// entire history on connect could be thousands of activities and several
	// minutes of rate-limited paging, for data nobody is about to ask about.
	firstSyncLookback = 90 * 24 * time.Hour
)

// oauthConfig is nil when credentials are absent, which is what makes the
// whole integration report itself unavailable rather than failing at boot.
// Same shape as internal/auth's newGoogleOAuth.
type oauthConfig struct {
	cfg *oauth2.Config
}

func newOAuth(clientID, clientSecret, baseURL string) *oauthConfig {
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	if clientID == "" || clientSecret == "" {
		return &oauthConfig{}
	}

	return &oauthConfig{cfg: &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  strings.TrimRight(baseURL, "/") + "/app/fitness/strava/callback",
		// activity:read_all also covers activities the athlete marked
		// private; without it their own data would be invisible to a tool
		// they explicitly connected.
		Scopes:   []string{"activity:read_all"},
		Endpoint: oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
	}}
}

func (o *oauthConfig) enabled() bool { return o != nil && o.cfg != nil }

// authCodeURL builds the consent URL. approval_prompt=auto so a returning
// athlete is not asked again, and the scope is stated up front.
func (o *oauthConfig) authCodeURL(state string) string {
	return o.cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("approval_prompt", "auto"),
		oauth2.SetAuthURLParam("response_type", "code"),
	)
}

// tokenResponse is the subset of Strava's token payload North uses. Strava
// returns the athlete inline on the initial exchange but not on refresh.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Athlete      struct {
		ID int64 `json:"id"`
	} `json:"athlete"`
}

func (o *oauthConfig) exchange(ctx context.Context, code string) (tokenResponse, error) {
	return o.postToken(ctx, url.Values{
		"code":       {code},
		"grant_type": {"authorization_code"},
	})
}

func (o *oauthConfig) refresh(ctx context.Context, refreshToken string) (tokenResponse, error) {
	return o.postToken(ctx, url.Values{
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	})
}

// postToken performs a token request. Errors deliberately carry the status
// code and nothing else: the request body contains the client secret and the
// response contains tokens, so neither may reach a log.
func (o *oauthConfig) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("client_id", o.cfg.ClientID)
	form.Set("client_secret", o.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, apperr.Wrap(err, "strava: build token request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return tokenResponse{}, apperr.Wrap(err, "strava: token request")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		// The body is deliberately discarded rather than reported: this
		// response carries tokens on success and echoes request parameters
		// on failure, and neither belongs in an error string.
		return tokenResponse{}, apperr.Wrap(apperr.ErrUnavailable, "strava: token request failed with status %d", resp.StatusCode)
	}

	var out tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return tokenResponse{}, apperr.Wrap(err, "strava: decode token response")
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return tokenResponse{}, apperr.New("strava: token response was missing tokens")
	}
	return out, nil
}

// apiActivity is the subset of Strava's activity payload North imports.
type apiActivity struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	SportType   string  `json:"sport_type"`
	StartDate   string  `json:"start_date"`
	ElapsedTime int     `json:"elapsed_time"`
	MovingTime  int     `json:"moving_time"`
	Calories    float64 `json:"calories"`
	Distance    float64 `json:"distance"`

	TotalElevationGain float64 `json:"total_elevation_gain"`
	AverageSpeed       float64 `json:"average_speed"`

	// Map carries the route. The list endpoint returns only the summary
	// polyline, which is exactly the resolution a route drawn a few hundred
	// pixels wide can show — fetching full streams would cost one request per
	// activity against a 100-per-15-minutes budget for detail nobody sees.
	Map struct {
		SummaryPolyline string `json:"summary_polyline"`
	} `json:"map"`
}

// sport prefers the newer sport_type and falls back to the legacy type, so
// stored rows carry whichever one Strava actually sent.
func (a apiActivity) sport() string {
	if a.SportType != "" {
		return a.SportType
	}
	return a.Type
}

// fetchActivities returns the athlete's activities after `after`, most
// recent first, capped at one page.
func fetchActivities(ctx context.Context, accessToken string, after time.Time) ([]apiActivity, error) {
	endpoint, err := url.Parse(activitesURL)
	if err != nil {
		return nil, apperr.Wrap(err, "strava: build activities url")
	}

	q := endpoint.Query()
	q.Set("per_page", strconv.Itoa(perPage))
	if !after.IsZero() {
		q.Set("after", strconv.FormatInt(after.Unix(), 10))
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, apperr.Wrap(err, "strava: build activities request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, apperr.Wrap(err, "strava: fetch activities")
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		// Worth distinguishing: the caller should try again later rather
		// than treat the connection as broken.
		return nil, apperr.Wrap(apperr.ErrUnavailable, "strava: rate limited, try again later")
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, apperr.Wrap(apperr.ErrUnavailable, "strava: activities request failed with status %d", resp.StatusCode)
	}

	var out []apiActivity
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, apperr.Wrap(err, "strava: decode activities")
	}
	return out, nil
}

// startedAt parses Strava's ISO-8601 start_date, which is always UTC.
func (a apiActivity) startedAt() (time.Time, error) {
	t, err := time.Parse(time.RFC3339, a.StartDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("strava: parse start_date %q: %w", a.StartDate, err)
	}
	return t, nil
}

// duration is the time North treats the session as having lasted.
//
// Moving time, not elapsed: a ride that includes a half-hour café stop did
// not burn calories for that half hour, and North's MET estimate multiplies
// straight through duration. Falls back to elapsed when moving is absent.
func (a apiActivity) duration() time.Duration {
	if a.MovingTime > 0 {
		return time.Duration(a.MovingTime) * time.Second
	}
	return time.Duration(a.ElapsedTime) * time.Second
}
