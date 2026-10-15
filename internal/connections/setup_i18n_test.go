package connections_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/connections"
)

// The prose around a setup reads in the person's language; everything they
// copy does not change, because a translated command is a broken command.
func TestSetupProseReadsInTheReadersLanguage(t *testing.T) {
	for _, kind := range connections.ClientKinds {
		en := instructions(t, kind)
		for _, tag := range []string{"es", "pt-PT", "pt-BR"} {
			got := en.In(tag)

			if got.ConfigLabel == en.ConfigLabel {
				t.Errorf("%s %s: config label is still %q", kind, tag, got.ConfigLabel)
			}
			if en.SafeNote != "" && got.SafeNote == en.SafeNote {
				t.Errorf("%s %s: safe note is still English", kind, tag)
			}
			if got.Config != en.Config || got.Safe != en.Safe || got.Export != en.Export ||
				got.Prompt != en.Prompt || got.SafeLabel != en.SafeLabel || got.URL != en.URL || got.Token != en.Token {
				t.Errorf("%s %s: In changed something a person copies", kind, tag)
			}
		}
		if again := en.In("en"); again != en {
			t.Errorf("%s: English In is not the identity", kind)
		}
	}
}

func TestSetupProseSpotChecks(t *testing.T) {
	codex := instructions(t, connections.ClientCodex)
	if got, want := codex.In("pt-PT").SafeNote, "Esse comando escreve isto, que também podes escrever tu. O Codex lê a chave do ambiente, por isso o ficheiro nunca a contém."; got != want {
		t.Errorf("pt-PT codex note = %q", got)
	}
	if got, want := instructions(t, connections.ClientOther).In("es").ConfigLabel, "datos de conexión"; got != want {
		t.Errorf("es other label = %q, want %q", got, want)
	}
}
