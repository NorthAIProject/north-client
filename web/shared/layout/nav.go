package layout

// BuildNav is the one place the application's sidebar is assembled.
//
// Every page previously defined its own private nav(active) copy of this
// same list, and the copies had already drifted (some were missing items
// others had). One shared list means a new section is added once, here, and
// every page picks it up automatically.
func BuildNav(active string) []NavItem {
	items := []NavItem{
		{Label: "Overview", Href: "/app"},
		{Label: "Coach", Href: "/app/chat"},
		{Label: "Fitness", Href: "/app/fitness"},
		{Label: "Mind", Href: "/app/mind"},
		{Label: "Care", Href: "/app/care"},
		{Label: "Goals", Href: "/app/goals"},
		{Label: "Check-ins", Href: "/app/check-ins"},
		{Label: "Memory", Href: "/app/memories"},
		{Label: "Knowledge", Href: "/app/knowledge"},
		{Label: "Settings", Href: "/app/settings"},
	}
	for i := range items {
		items[i].Active = items[i].Href == active
	}
	return items
}
