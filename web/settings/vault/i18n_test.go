package vault

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestVaultRendersInTheChosenLanguage(t *testing.T) {
	synced := time.Date(2026, 10, 3, 15, 4, 0, 0, time.UTC)
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Vault folder", "Sync now", "Change folder", "Last sync: Oct 3, 2026 3:04 PM"},
		users.LocalePTPT: {"Pasta do cofre", "Sincronizar agora", "Mudar de pasta", "Última sincronização: 3 out 2026, 15:04"},
		users.LocalePTBR: {"Pasta do cofre", "Sincronizar agora", "Trocar de pasta", "Última sincronização: 3 out 2026, 15:04"},
		users.LocaleES:   {"Carpeta del baúl", "Sincronizar ahora", "Cambiar de carpeta", "Última sincronización: 3 oct 2026, 15:04"},
	}
	for locale, wants := range cases {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		var b strings.Builder
		page := Page(users.User{DisplayName: "Ana", Locale: locale}, View{RootPath: "/Users/ana/Vault", LastSyncAt: &synced}, Form{})
		if err := page.Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		html := b.String()
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: vault page does not contain %q", locale, want)
			}
		}
		if locale != users.LocaleEN {
			for _, leak := range []string{"Vault folder", "Sync now", "Folder path", "Save and sync", "worker must run"} {
				if strings.Contains(html, leak) {
					t.Errorf("%s: English %q is still on the vault page", locale, leak)
				}
			}
		}
	}
}

func TestFormatUpdatedFollowsTheLocale(t *testing.T) {
	at := time.Date(2026, 10, 3, 15, 4, 0, 0, time.UTC)
	if got := FormatUpdated(context.Background(), at); got != "updated 3:04PM" {
		t.Errorf("English: got %q", got)
	}
	pt := i18n.WithLocale(context.Background(), string(users.LocalePTPT))
	if got := FormatUpdated(pt, at); got != "atualizado às 15:04" {
		t.Errorf("pt-PT: got %q", got)
	}
}
