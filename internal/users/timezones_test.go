package users_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

// Every offered zone must be one this build can actually load. A name that
// LoadLocation rejects would be silently rewritten to UTC by ResolveZone, so
// the select box would quietly refuse to save one of its own options.
func TestEveryOfferedZoneResolves(t *testing.T) {
	for _, group := range users.ZoneGroupsIncluding("") {
		if group.Region == "" {
			t.Error("zone group with no region")
		}
		if len(group.Zones) == 0 {
			t.Errorf("region %q offers no zones", group.Region)
		}
		for _, zone := range group.Zones {
			if _, err := time.LoadLocation(zone); err != nil {
				t.Errorf("zone %q (%s): %v", zone, group.Region, err)
			}
		}
	}
}

func TestZoneGroupsIncludingKeepsAnUnlistedZone(t *testing.T) {
	const unlisted = "America/Argentina/Ushuaia"

	groups := users.ZoneGroupsIncluding(unlisted)
	if len(groups) == 0 {
		t.Fatal("no groups")
	}
	first := groups[0]
	if first.Region != "Current" || len(first.Zones) != 1 || first.Zones[0] != unlisted {
		t.Fatalf("first group = %+v, want the unlisted zone on its own", first)
	}
}

func TestZoneGroupsIncludingDoesNotDuplicateAListedZone(t *testing.T) {
	groups := users.ZoneGroupsIncluding("Europe/Lisbon")

	seen := 0
	for _, g := range groups {
		if g.Region == "Current" {
			t.Fatal("added a Current group for a zone that is already offered")
		}
		for _, z := range g.Zones {
			if z == "Europe/Lisbon" {
				seen++
			}
		}
	}
	if seen != 1 {
		t.Fatalf("Europe/Lisbon appears %d times, want 1", seen)
	}
}

func TestResolveZoneFallsBackToUTC(t *testing.T) {
	cases := map[string]string{
		"":                  "UTC",
		"   ":               "UTC",
		"Mars/Olympus_Mons": "UTC",
		"Europe/Lisbon":     "Europe/Lisbon",
		" Europe/Lisbon ":   "Europe/Lisbon",
	}

	for in, want := range cases {
		if got := users.ResolveZone(in); got != want {
			t.Errorf("ResolveZone(%q) = %q, want %q", in, got, want)
		}
	}
}
