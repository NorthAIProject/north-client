package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
)

func TestLogReadyWarnsWhenHermesIsNamedButMissing(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := ai.NewRegistry()
	r.Register(fake.Text("unused"))
	if err := r.SetDefault("fake"); err != nil {
		t.Fatal(err)
	}

	AIConfig{Chain: []string{"hermes", "fake"}}.LogReady(log, r)

	out := buf.String()
	if !strings.Contains(out, "ai providers ready") {
		t.Fatalf("missing ready line: %s", out)
	}
	if !strings.Contains(out, "HERMES_API_KEY") {
		t.Fatalf("missing hermes skip warning: %s", out)
	}
}

func TestLogReadyIsQuietWhenNamedProvidersAreRegistered(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := ai.NewRegistry()
	r.Register(fake.Text("unused"))
	if err := r.SetDefault("fake"); err != nil {
		t.Fatal(err)
	}

	AIConfig{Chain: []string{"fake"}}.LogReady(log, r)

	if strings.Contains(buf.String(), "skipped") || strings.Contains(buf.String(), "HERMES_API_KEY") {
		t.Fatalf("unexpected warning: %s", buf.String())
	}
}
