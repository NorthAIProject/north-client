package layout

// BuildNav is the one place the application's sidebar is assembled.
//
// Every page previously defined its own private nav(active) copy of this
// same list, and the copies had already drifted (some were missing items
// others had). One shared list means a new section is added once, here, and
// every page picks it up automatically.
//
// The list is grouped rather than flat because it has outgrown a single
// column: eleven undifferentiated links read as a pile, whereas five short
// groups read as a map of the product. The group headings disappear when the
// rail is collapsed to icons, so they cost nothing in that mode.
func BuildNav(active string) []NavGroup {
	groups := []NavGroup{
		{
			Label: "Today",
			Items: []NavItem{
				{Label: "Overview", Href: "/app", Icon: "layout-dashboard"},
				{Label: "Check-ins", Href: "/app/check-ins", Icon: "smile"},
				{Label: "Coach", Href: "/app/chat", Icon: "message-circle"},
			},
		},
		{
			Label: "Body",
			Items: []NavItem{
				{Label: "Fitness", Href: "/app/fitness", Icon: "dumbbell"},
				{Label: "Care", Href: "/app/care", Icon: "heart-pulse"},
			},
		},
		{
			Label: "Mind",
			Items: []NavItem{
				{Label: "Mind", Href: "/app/mind", Icon: "brain"},
				{Label: "Memory", Href: "/app/memories", Icon: "sparkles"},
			},
		},
		{
			Label: "Progress",
			Items: []NavItem{
				{Label: "Goals", Href: "/app/goals", Icon: "target"},
				{
					Label: "Insights",
					Href:  "/app/insights/timeline",
					Icon:  "chart-line",
					// The one nested entry. Insights is five views over the
					// same data, and promoting all five to the top level would
					// undo the grouping this list exists to provide.
					Children: []NavItem{
						{Label: "Activity", Href: "/app/insights/timeline"},
						{Label: "Body", Href: "/app/insights/body"},
						{Label: "Mind", Href: "/app/insights/mind"},
						{Label: "Progress", Href: "/app/insights/progress"},
						{Label: "Training", Href: "/app/insights/training"},
					},
				},
			},
		},
		{
			Label: "System",
			Items: []NavItem{
				{Label: "Knowledge", Href: "/app/knowledge", Icon: "book-open"},
				{Label: "Settings", Href: "/app/settings", Icon: "settings"},
			},
		},
	}

	for gi := range groups {
		for ii := range groups[gi].Items {
			item := &groups[gi].Items[ii]
			item.Active = item.Href == active

			for ci := range item.Children {
				child := &item.Children[ci]
				child.Active = child.Href == active
				// A parent whose child is the current page counts as active
				// too, so the section stays lit while you are inside it.
				if child.Active {
					item.Active = true
				}
			}
		}
	}
	return groups
}
