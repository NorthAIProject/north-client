// Package facebook is the slice of Facebook Login and the Graph API that
// "Connect Facebook" needs: the consent URL, and one call that turns a code
// into the person's app-scoped id and their friends' ids.
//
// The access token never leaves Import. It is used for two reads and dropped;
// nothing here can store it because nothing here returns it.
package facebook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const (
	graphVersion = "v21.0"
	dialogURL    = "https://www.facebook.com/" + graphVersion + "/dialog/oauth"
	graphURL     = "https://graph.facebook.com/" + graphVersion
	httpTimeout  = 20 * time.Second

	// Scopes asks for the person's id and the friends who also use the app.
	// user_friends returns nobody else, by Facebook's design.
	Scopes = "public_profile,user_friends"

	// friendsPageSize and maxFriendPages bound one import at Facebook's
	// 5,000-friend cap.
	friendsPageSize = 500
	maxFriendPages  = 10
)

// Account is what one import learned: who connected, and the app-scoped ids
// of their friends who connected Khepri too.
type Account struct {
	// ID is the app-scoped user id, stable for this app.
	ID        string
	FriendIDs []string
}

// Client talks to Facebook for one app. The zero value of the URL fields and
// HTTP talks to the real Facebook; tests point them at a fake.
type Client struct {
	AppID     string
	AppSecret string

	DialogURL string
	GraphURL  string
	HTTP      *http.Client
}

// New returns nil when either credential is missing, which callers read as
// "Facebook is not configured".
func New(appID, appSecret string) *Client {
	appID, appSecret = strings.TrimSpace(appID), strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return nil
	}
	return &Client{AppID: appID, AppSecret: appSecret}
}

// AuthCodeURL is the consent dialog. redirectURI must be registered in the
// Meta app exactly as given, and Import must be passed the same one.
func (c *Client) AuthCodeURL(state, redirectURI string) string {
	base := c.DialogURL
	if base == "" {
		base = dialogURL
	}
	return base + "?" + url.Values{
		"client_id":     {c.AppID},
		"redirect_uri":  {redirectURI},
		"state":         {state},
		"scope":         {Scopes},
		"response_type": {"code"},
	}.Encode()
}

// Import exchanges code for a token, reads /me and every page of
// /me/friends, and lets the token go.
func (c *Client) Import(ctx context.Context, code, redirectURI string) (Account, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	if strings.TrimSpace(code) == "" {
		return Account{}, apperr.New("facebook: missing authorization code")
	}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.get(ctx, c.graph("/oauth/access_token", url.Values{
		"client_id":     {c.AppID},
		"client_secret": {c.AppSecret},
		"redirect_uri":  {redirectURI},
		"code":          {code},
	}), &tok); err != nil {
		return Account{}, apperr.Wrap(err, "facebook: exchange code")
	}
	if tok.AccessToken == "" {
		return Account{}, apperr.New("facebook: token response had no token")
	}
	auth := url.Values{"access_token": {tok.AccessToken}, "appsecret_proof": {c.proof(tok.AccessToken)}}

	var me struct {
		ID string `json:"id"`
	}
	meQuery := url.Values{"fields": {"id"}}
	for k, v := range auth {
		meQuery[k] = v
	}
	if err := c.get(ctx, c.graph("/me", meQuery), &me); err != nil {
		return Account{}, apperr.Wrap(err, "facebook: read profile")
	}
	if me.ID == "" {
		return Account{}, apperr.New("facebook: profile had no id")
	}

	friends, err := c.friends(ctx, auth)
	if err != nil {
		return Account{}, err
	}
	return Account{ID: me.ID, FriendIDs: friends}, nil
}

// friends follows paging.next until it runs out or the page bound is hit.
func (c *Client) friends(ctx context.Context, auth url.Values) ([]string, error) {
	q := url.Values{"fields": {"id"}, "limit": {strconv.Itoa(friendsPageSize)}}
	for k, v := range auth {
		q[k] = v
	}
	next := c.graph("/me/friends", q)
	var ids []string
	for page := 0; next != "" && page < maxFriendPages; page++ {
		var body struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Paging struct {
				Next string `json:"next"`
			} `json:"paging"`
		}
		if err := c.get(ctx, next, &body); err != nil {
			return nil, apperr.Wrap(err, "facebook: read friends")
		}
		for _, f := range body.Data {
			if f.ID != "" {
				ids = append(ids, f.ID)
			}
		}
		next = c.nextPage(body.Paging.Next, auth)
	}
	return ids, nil
}

// nextPage is Facebook's paging.next with the token and proof set, and only
// if it points back at the Graph API: the token goes nowhere else.
func (c *Client) nextPage(next string, auth url.Values) string {
	if next == "" {
		return ""
	}
	u, err := url.Parse(next)
	base, baseErr := url.Parse(c.graph("", nil))
	if err != nil || baseErr != nil || u.Scheme != base.Scheme || u.Host != base.Host {
		return ""
	}
	q := u.Query()
	for k, v := range auth {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) graph(path string, q url.Values) string {
	base := c.GraphURL
	if base == "" {
		base = graphURL
	}
	return strings.TrimRight(base, "/") + path + "?" + q.Encode()
}

// proof is appsecret_proof, which keeps a leaked token useless to anybody
// without the app secret when "Require App Secret" is on in the Meta app.
func (c *Client) proof(token string) string {
	mac := hmac.New(sha256.New, []byte(c.AppSecret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// get decodes a Graph response. Errors never carry the URL: every one of
// these URLs holds a code, a secret or a token.
func (c *Client) get(ctx context.Context, rawURL string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return errors.New("build request")
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return apperr.Wrap(err, "request")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return apperr.Wrap(err, "read response")
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Type string `json:"type"`
				Code int    `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return apperr.Wrap(apperr.ErrUnavailable, "status %d, %s code %d", resp.StatusCode, e.Error.Type, e.Error.Code)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return apperr.Wrap(err, "decode response")
	}
	return nil
}
