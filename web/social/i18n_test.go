package social

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/social/friend"
	"github.com/NorthAIProject/north-client/internal/social/phone"
	"github.com/NorthAIProject/north-client/internal/users"
)

// renderFriendsIn renders the friends page with every optional card showing,
// so a string missed anywhere on it has somewhere to leak.
func renderFriendsIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	user := users.User{DisplayName: "Ana", Locale: locale}
	bruno := friend.Person{ID: uuid.New(), DisplayName: "Bruno Lima", Handle: "bruno"}
	overview := friend.Overview{
		Joined:    3,
		Following: []friend.Connection{{Person: bruno, Status: friend.StatusAccepted}},
	}
	feed := FeedSection{
		Enabled: true,
		Items: []achievement.Item{
			{ID: uuid.New(), DisplayName: "Bruno Lima", Title: "Finished a workout", OccurredAt: time.Now().Add(-2 * time.Hour), Kudos: 2},
		},
	}
	finding := Finding{
		Phone:    PhoneSection{Enabled: true, Dial: "351"},
		Facebook: FacebookSection{Enabled: true, Result: "taken"},
	}

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	page := FriendsPage(user, overview, "https://khepri.test/i/abc", FriendsForm{}, feed, ShareLinks{}, finding)
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestFriendsRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Friends", "Your invite link", "3 people joined through your links.", "2h ago", "Text me a code", "Portugal"},
		users.LocalePTPT: {"Amigos", "O teu link de convite", "3 pessoas entraram através dos teus links.", "há 2 h", "Envia-me um código por SMS", "Espanha"},
		users.LocalePTBR: {"Amigos", "Seu link de convite", "3 pessoas entraram pelos seus links.", "há 2 h", "Me envie um código por SMS", "Espanha"},
		users.LocaleES:   {"Amigos", "Tu enlace de invitación", "3 personas se unieron con tus enlaces.", "hace 2 h", "Envíame un código", "España"},
	}
	for locale, wants := range cases {
		html := renderFriendsIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedFriendsPage(t *testing.T) {
	englishOnly := []string{
		"Your invite link",
		"Anyone can follow you.",
		"Your handle",
		"Follow somebody",
		"What followers see",
		"Phone number",
		"Facebook friends on Khepri",
		"Give kudos",
		"Followers see what you switch on here",
		"Nobody follows you yet.",
		"already connected to another Khepri account",
	}
	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderFriendsIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}

// The public invite page is the first thing a newcomer reads, before they
// have an account or a saved language.
func TestInvitePageIsTranslated(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocaleEN:   "Ana invited you to Khepri",
		users.LocalePTPT: "Ana convidou-te para o Khepri",
		users.LocalePTBR: "Ana convidou você para o Khepri",
		users.LocaleES:   "Ana te ha invitado a Khepri",
	}
	for locale, want := range cases {
		var b strings.Builder
		ctx := i18n.WithLocale(context.Background(), string(locale))
		if err := InvitePage(friend.InvitePreview{Inviter: friend.Person{DisplayName: "Ana"}}).Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		if !strings.Contains(b.String(), want) {
			t.Errorf("%s: expected %q", locale, want)
		}
	}
}

// Every country offered beside a phone number reads in the page's language.
// Names spelled the same everywhere (Portugal, Argentina, Angola) have no
// catalogue entry and fall back to phone.Countries; nothing may render as a
// raw key.
func TestEveryPhoneCountryHasAName(t *testing.T) {
	for _, locale := range users.Locales {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		for _, c := range phone.Countries {
			if got := countryName(ctx, c); got == "" || strings.HasPrefix(got, "social.country.") {
				t.Errorf("%s: +%s renders as %q", locale, c.Dial, got)
			}
		}
	}

	spain := phone.Country{Dial: "34", Name: "Spain"}
	wants := map[users.Locale]string{
		users.LocaleEN:   "Spain",
		users.LocalePTPT: "Espanha",
		users.LocalePTBR: "Espanha",
		users.LocaleES:   "España",
	}
	for locale, want := range wants {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		if got := countryName(ctx, spain); got != want {
			t.Errorf("%s: Spain is %q, want %q", locale, got, want)
		}
	}
}

// Names link to the profile only for people who have a handle, and the
// viewer gets a link to their own once they have one.
func TestNamesLinkToProfiles(t *testing.T) {
	html := renderFriendsIn(t, users.LocaleEN)
	if !strings.Contains(html, `href="/app/u/bruno"`) {
		t.Error("a person with a handle is not linked")
	}
	if strings.Contains(html, "Your profile") {
		t.Error("Your profile shown without a handle")
	}

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "en")
	page := FriendsPage(users.User{DisplayName: "Ana"}, friend.Overview{Handle: "ana"}, "https://khepri.test/i/abc", FriendsForm{}, FeedSection{}, ShareLinks{}, Finding{})
	if err := page.Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `href="/app/u/ana"`) || !strings.Contains(b.String(), "Your profile") {
		t.Error("no link to the viewer's own profile")
	}
}
