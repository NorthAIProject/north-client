package users

import (
	"slices"
	"strings"
	"time"
)

// ZoneGroup is a region and the zones offered under it.
type ZoneGroup struct {
	Region string
	Zones  []string
}

// zoneGroups is a curated shortlist, not the whole tzdata.
//
// The full database is well over 400 names, most of them aliases or zones
// nobody picks from a settings page, and Go exposes no way to enumerate it
// anyway. A shortlist a person can scan — and search, since the select box
// filters — beats completeness here, and ZoneGroupsIncluding covers whoever
// arrives on a zone this list forgot.
var zoneGroups = []ZoneGroup{
	{Region: "Africa", Zones: []string{
		"Africa/Abidjan", "Africa/Accra", "Africa/Algiers", "Africa/Cairo",
		"Africa/Casablanca", "Africa/Johannesburg", "Africa/Lagos", "Africa/Nairobi",
	}},
	{Region: "Americas", Zones: []string{
		"America/Anchorage", "America/Argentina/Buenos_Aires", "America/Bogota",
		"America/Chicago", "America/Denver", "America/Halifax", "America/Lima",
		"America/Los_Angeles", "America/Mexico_City", "America/New_York",
		"America/Phoenix", "America/Santiago", "America/Sao_Paulo",
		"America/St_Johns", "America/Toronto", "America/Vancouver",
	}},
	{Region: "Asia", Zones: []string{
		"Asia/Bangkok", "Asia/Dhaka", "Asia/Dubai", "Asia/Hong_Kong",
		"Asia/Jakarta", "Asia/Jerusalem", "Asia/Karachi", "Asia/Kolkata",
		"Asia/Manila", "Asia/Riyadh", "Asia/Seoul", "Asia/Shanghai",
		"Asia/Singapore", "Asia/Taipei", "Asia/Tehran", "Asia/Tokyo",
	}},
	{Region: "Europe", Zones: []string{
		"Atlantic/Azores", "Europe/Amsterdam", "Europe/Athens", "Europe/Berlin",
		"Europe/Brussels", "Europe/Bucharest", "Europe/Budapest", "Europe/Copenhagen",
		"Europe/Dublin", "Europe/Helsinki", "Europe/Istanbul", "Europe/Kyiv",
		"Europe/Lisbon", "Europe/London", "Europe/Madrid", "Europe/Moscow",
		"Europe/Oslo", "Europe/Paris", "Europe/Prague", "Europe/Rome",
		"Europe/Stockholm", "Europe/Vienna", "Europe/Warsaw", "Europe/Zurich",
	}},
	{Region: "Oceania", Zones: []string{
		"Australia/Adelaide", "Australia/Brisbane", "Australia/Melbourne",
		"Australia/Perth", "Australia/Sydney", "Pacific/Auckland",
		"Pacific/Fiji", "Pacific/Honolulu",
	}},
	{Region: "Other", Zones: []string{"UTC"}},
}

// ZoneGroupsIncluding returns the offered zones, with current added as a group
// of its own when the shortlist does not already carry it.
//
// Without this, somebody whose account holds a zone this list forgot would open
// settings, see a select box that cannot show what they chose, and relocate
// themselves the next time they saved their name.
func ZoneGroupsIncluding(current string) []ZoneGroup {
	current = strings.TrimSpace(current)
	if current == "" || knownZone(current) {
		return zoneGroups
	}

	out := make([]ZoneGroup, 0, len(zoneGroups)+1)
	out = append(out, ZoneGroup{Region: "Current", Zones: []string{current}})
	return append(out, zoneGroups...)
}

func knownZone(name string) bool {
	for _, g := range zoneGroups {
		if slices.Contains(g.Zones, name) {
			return true
		}
	}
	return false
}

// ResolveZone returns the zone to store for a submitted value: the value
// itself when this build can load it, UTC otherwise.
//
// Service.ValidateRegistration and Service.UpdateProfile share this so the two
// paths cannot drift on what "a timezone we do not recognise" means.
func ResolveZone(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "UTC"
	}
	return name
}
