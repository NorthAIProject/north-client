package social_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/social/facebook"
	"github.com/NorthAIProject/north-client/internal/social/phone"
)

func phoneHash(e164 string) string {
	sum := sha256.Sum256([]byte(e164))
	return hex.EncodeToString(sum[:])
}

// verifierSpy texts nothing, accepts one code, and remembers who it texted.
type verifierSpy struct {
	mu      sync.Mutex
	texted  []string
	code    string
	failing error
}

func (v *verifierSpy) Start(_ context.Context, number string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.failing != nil {
		return v.failing
	}
	v.texted = append(v.texted, number)
	return nil
}

func (v *verifierSpy) Check(_ context.Context, _, code string) (bool, error) {
	return code == v.code, nil
}

// verify gives an account a number, the whole way through.
func verify(t *testing.T, svc *social.Service, userID uuid.UUID, raw string) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.StartPhoneVerification(ctx, userID, raw, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckPhoneCode(ctx, userID, "123456"); err != nil {
		t.Fatal(err)
	}
}

func TestPhoneVerification(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")

	// Off without a verifier, except for reading and removing.
	if _, err := svc.StartPhoneVerification(ctx, ana.ID, "+351912345678", ""); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("start without a verifier = %v, want not found", err)
	}
	if _, err := svc.Phone(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}

	spy := &verifierSpy{code: "123456"}
	svc.WithPhoneVerifier(spy)

	if _, err := svc.StartPhoneVerification(ctx, ana.ID, "912 345 678", ""); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a national number with no country = %v", err)
	}
	p, err := svc.StartPhoneVerification(ctx, ana.ID, "912 345 678", "351")
	if err != nil {
		t.Fatal(err)
	}
	if p.Pending != "+351912345678" || p.Number != "" || len(spy.texted) != 1 {
		t.Fatalf("after start = %+v, texted %v", p, spy.texted)
	}
	if _, err = svc.CheckPhoneCode(ctx, ana.ID, "654321"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a wrong code = %v", err)
	}
	if p, err = svc.CheckPhoneCode(ctx, ana.ID, "123456"); err != nil {
		t.Fatal(err)
	}
	if p.Number != "+351912345678" || p.VerifiedAt.IsZero() || p.Pending != "" {
		t.Fatalf("after check = %+v", p)
	}
	if _, err := svc.CheckPhoneCode(ctx, ana.ID, "123456"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a used code checked again = %v", err)
	}
	if _, err := svc.StartPhoneVerification(ctx, ana.ID, "+351 912 345 678", ""); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("re-verifying the same number = %v", err)
	}

	// Somebody else proving the same number takes it.
	verify(t, svc, joao.ID, "00351912345678")
	if p, _ := svc.Phone(ctx, ana.ID); p.Number != "" {
		t.Fatalf("Ana kept a number João verified: %+v", p)
	}
	if p, _ := svc.Phone(ctx, joao.ID); p.Number != "+351912345678" {
		t.Fatalf("João = %+v", p)
	}

	// A different number can replace a waiting one, and cancelling forgets it.
	if _, err := svc.StartPhoneVerification(ctx, ana.ID, "+447911123456", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.CancelPhoneVerification(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckPhoneCode(ctx, ana.ID, "123456"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a cancelled code = %v", err)
	}

	if err := svc.RemovePhone(ctx, joao.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.Phone(ctx, joao.ID); p.Number != "" {
		t.Fatalf("after remove = %+v", p)
	}

	// A number the provider will not text is a field error, and is not left
	// waiting for a code.
	spy.failing = phone.ErrUndeliverable
	if _, err := svc.StartPhoneVerification(ctx, joao.ID, "+351212345678", ""); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("undeliverable = %v", err)
	}
	if p, _ := svc.Phone(ctx, joao.ID); p.Pending != "" {
		t.Fatalf("an undelivered text is pending: %+v", p)
	}
}

func TestPhoneRateLimits(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	svc.WithPhoneVerifier(&verifierSpy{code: "123456"})
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")
	rita := person(t, pool, "rita@north.test", "Rita")

	// Per account per hour, whatever the numbers.
	for i := range social.MaxStartsPerUserHour {
		if _, err := svc.StartPhoneVerification(ctx, ana.ID, "+35191234567"+string(rune('0'+i)), ""); err != nil {
			t.Fatalf("start %d: %v", i+1, err)
		}
	}
	if _, err := svc.StartPhoneVerification(ctx, ana.ID, "+351919999999", ""); !errors.Is(err, social.ErrPhoneRateLimited) {
		t.Fatalf("start past the hourly limit = %v", err)
	}

	// Per number per day, whoever asks: an account cannot flood a stranger.
	// Ana has already texted +351912345670 once.
	for i := 1; i < social.MaxStartsPerNumberDay; i++ {
		who := joao.ID
		if i > 3 {
			who = rita.ID
		}
		if _, err := svc.StartPhoneVerification(ctx, who, "+351912345670", ""); err != nil {
			t.Fatalf("start %d to one number: %v", i+1, err)
		}
	}
	if _, err := svc.StartPhoneVerification(ctx, rita.ID, "+351912345670", ""); !errors.Is(err, social.ErrPhoneRateLimited) {
		t.Fatalf("start past the number's daily limit = %v", err)
	}

	// Guesses against one text.
	for range social.MaxCodeChecks {
		if _, err := svc.CheckPhoneCode(ctx, rita.ID, "000001"); !apperr.Is(err, apperr.ErrValidation) {
			t.Fatalf("a wrong guess = %v", err)
		}
	}
	if _, err := svc.CheckPhoneCode(ctx, rita.ID, "123456"); !errors.Is(err, social.ErrPhoneRateLimited) {
		t.Fatalf("the right code after the limit = %v, want rate limited", err)
	}
}

// Phone hashes find verified numbers of people with a handle, never an
// unverified number, a handle-less account, or anybody blocked either way.
func TestMatchContactsByPhone(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	svc.WithPhoneVerifier(&verifierSpy{code: "123456"})
	me := person(t, pool, "me@north.test", "Me")
	ana := person(t, pool, "ana@north.test", "Ana")
	pending := person(t, pool, "pending@north.test", "Pending")
	hidden := person(t, pool, "hidden@north.test", "Hidden")
	blocked := person(t, pool, "blocked@north.test", "Blocked")
	for u, h := range map[uuid.UUID]string{ana.ID: "ana", pending.ID: "pending", blocked.ID: "blocked"} {
		if _, err := svc.SetHandle(ctx, u, h); err != nil {
			t.Fatal(err)
		}
	}
	verify(t, svc, ana.ID, "+351910000001")
	verify(t, svc, hidden.ID, "+351910000003")
	verify(t, svc, blocked.ID, "+351910000004")
	if _, err := svc.StartPhoneVerification(ctx, pending.ID, "+351910000002", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Block(ctx, me.ID, blocked.ID); err != nil {
		t.Fatal(err)
	}

	phones := []string{phoneHash("+351910000001"), phoneHash("+351910000002"), phoneHash("+351910000003"), phoneHash("+351910000004")}
	found, err := svc.MatchContacts(ctx, me.ID, nil, phones)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != ana.ID || found[0].Status != "" {
		t.Fatalf("found = %+v, want only Ana", found)
	}

	// An email and a phone hash for the same person find them once.
	found, err = svc.MatchContacts(ctx, me.ID, []string{emailHash("ana@north.test")}, phones[:1])
	if err != nil || len(found) != 1 {
		t.Fatalf("found = %+v, %v", found, err)
	}

	// The cap is on both lists together.
	tooMany := make([]string, social.MaxContactHashes)
	for i := range tooMany {
		tooMany[i] = phones[0]
	}
	if _, err := svc.MatchContacts(ctx, me.ID, []string{emailHash("x@north.test")}, tooMany); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("2001 hashes = %v", err)
	}
	if _, err := svc.MatchContacts(ctx, me.ID, nil, []string{"+351910000001"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a raw number was accepted: %v", err)
	}
}

// facebookFake hands out one Facebook account per code.
type facebookFake struct {
	accounts map[string]facebook.Account
}

func (f *facebookFake) AuthCodeURL(state, redirectURI string) string {
	return "https://facebook.test/dialog?" + url.Values{"state": {state}, "redirect_uri": {redirectURI}}.Encode()
}

func (f *facebookFake) Import(_ context.Context, code, _ string) (facebook.Account, error) {
	a, ok := f.accounts[code]
	if !ok {
		return facebook.Account{}, apperr.New("facebook: bad code")
	}
	return a, nil
}

func TestConnectFacebookImportsFriendsHere(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	me := person(t, pool, "me@gmail.test", "Me")
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")
	rita := person(t, pool, "rita@north.test", "Rita")
	blocker := person(t, pool, "blocker@north.test", "Blocker")
	for u, h := range map[uuid.UUID]string{ana.ID: "ana", rita.ID: "rita", blocker.ID: "blocker"} {
		if _, err := svc.SetHandle(ctx, u, h); err != nil {
			t.Fatal(err)
		}
	}
	fb := &facebookFake{accounts: map[string]facebook.Account{
		"ana": {ID: "fb-ana"}, "joao": {ID: "fb-joao"}, "rita": {ID: "fb-rita"}, "blocker": {ID: "fb-blocker"},
		"me":    {ID: "fb-me", FriendIDs: []string{"fb-ana", "fb-joao", "fb-blocker", "fb-stranger"}},
		"me-2":  {ID: "fb-me", FriendIDs: []string{"fb-ana", "fb-rita"}},
		"theft": {ID: "fb-ana"},
	}}

	if _, err := svc.ConnectFacebook(ctx, me.ID, "me", "https://kheprios.com/cb"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("connect without Facebook configured = %v", err)
	}
	svc.WithFacebook(fb)

	for code, u := range map[string]uuid.UUID{"ana": ana.ID, "joao": joao.ID, "rita": rita.ID, "blocker": blocker.ID} {
		if _, err := svc.ConnectFacebook(ctx, u, code, "https://kheprios.com/cb"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Block(ctx, blocker.ID, me.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Follow(ctx, me.ID, "ana"); err != nil {
		t.Fatal(err)
	}

	// Ana has a handle; João has none; Blocker blocked me; the stranger is
	// not here. Any email works: mine matches nobody's Facebook.
	got, err := svc.ConnectFacebook(ctx, me.ID, "me", "https://kheprios.com/cb")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Connected || got.ImportedAt.IsZero() || len(got.People) != 1 || got.People[0].ID != ana.ID || got.People[0].Status != social.StatusPending {
		t.Fatalf("import = %+v, want Ana, already asked", got)
	}

	// Connecting again is finding again.
	got, err = svc.ConnectFacebook(ctx, me.ID, "me-2", "https://kheprios.com/cb")
	if err != nil || len(got.People) != 2 {
		t.Fatalf("second import = %+v, %v; want Ana and Rita", got, err)
	}
	// What is shown is re-read: Rita hiding herself drops her at once.
	if _, err := svc.SetHandle(ctx, rita.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ = svc.FacebookFriends(ctx, me.ID); len(got.People) != 1 {
		t.Fatalf("after Rita cleared her handle = %+v", got.People)
	}

	// A Facebook account already connected elsewhere is refused, not moved.
	if _, err := svc.ConnectFacebook(ctx, joao.ID, "theft", "https://kheprios.com/cb"); !errors.Is(err, social.ErrFacebookTaken) {
		t.Fatalf("connecting Ana's Facebook to João = %v", err)
	}
	if anas, _ := svc.FacebookFriends(ctx, ana.ID); !anas.Connected {
		t.Fatal("Ana lost her Facebook link to a refused attempt")
	}

	if err := svc.DisconnectFacebook(ctx, me.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = svc.FacebookFriends(ctx, me.ID); got.Connected || len(got.People) != 0 {
		t.Fatalf("after disconnect = %+v", got)
	}
	// Disconnecting frees the Facebook account for another Khepri account.
	if _, err := svc.ConnectFacebook(ctx, joao.ID, "me", "https://kheprios.com/cb"); err != nil {
		t.Fatalf("connecting a freed Facebook account = %v", err)
	}
}

// The app's flow: the state is minted for one account, finds it at the
// callback, and works once.
func TestNativeFacebookStateIsSingleUse(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	me := person(t, pool, "me@north.test", "Me")
	svc.WithFacebook(&facebookFake{accounts: map[string]facebook.Account{"code": {ID: "fb-me"}}})

	consent, err := svc.BeginNativeFacebook(ctx, me.ID, "https://kheprios.com"+social.FacebookNativeCallbackPath)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(consent)
	state := u.Query().Get("state")
	if len(state) < 40 || !strings.HasSuffix(u.Query().Get("redirect_uri"), social.FacebookNativeCallbackPath) {
		t.Fatalf("consent = %s", consent)
	}

	for _, bad := range []string{"", "not-a-state", strings.ToUpper(state)} {
		if _, err = svc.FinishNativeFacebook(ctx, bad, "code", ""); !apperr.Is(err, apperr.ErrNotFound) {
			t.Fatalf("state %q = %v, want not found", bad, err)
		}
	}
	got, err := svc.FinishNativeFacebook(ctx, state, "code", "")
	if err != nil || !got.Connected {
		t.Fatalf("finish = %+v, %v", got, err)
	}
	if _, err := svc.FinishNativeFacebook(ctx, state, "code", ""); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a reused state = %v, want not found", err)
	}
}
