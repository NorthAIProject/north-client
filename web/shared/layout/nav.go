package layout

// BuildNav assembles the application's sidebar.
//
// Every page previously defined its own private nav(active) copy of this same
// list, and the copies had already drifted (some were missing items others
// had). One shared list means a new section is added once and every page picks
// it up automatically.
//
// That list now lives in destinations.go, and this function is a fold over it
// rather than a second copy beside it. The sidebar shows a deliberate subset —
// thirteen of thirty-one destinations — because a rail listing everything
// stops being a map. The command palette is where the rest are reachable, and
// deriving both from one registry is what stops the two from disagreeing.
//
// The list is grouped rather than flat because it has outgrown a single
// column: eleven undifferentiated links read as a pile, whereas five short
// groups read as a map of the product. The group headings disappear when the
// rail is collapsed to icons, so they cost nothing in that mode.
func BuildNav(active string) []NavGroup {
	var groups []NavGroup

	for _, groupName := range GroupOrder() {
		group := NavGroup{Label: groupName}

		// Two passes: parents first, so a child can always find the item it
		// nests under regardless of registry order.
		byHref := map[string]int{}
		for _, d := range Destinations() {
			if d.Group != groupName || !d.Nav.Show || d.Nav.Under != "" {
				continue
			}

			item := NavItem{Label: navLabel(d), Href: d.Href, Icon: d.Icon}
			if d.Nav.SelfChildLabel != "" {
				// A section landing page that is also its own first view. The
				// child points at the same href on purpose: it is one page
				// wearing two labels, not two destinations.
				item.Children = append(item.Children, NavItem{
					Label: d.Nav.SelfChildLabel,
					Href:  d.Href,
				})
			}

			byHref[d.Href] = len(group.Items)
			group.Items = append(group.Items, item)
		}

		for _, d := range Destinations() {
			if d.Group != groupName || !d.Nav.Show || d.Nav.Under == "" {
				continue
			}
			parent, ok := byHref[d.Nav.Under]
			if !ok {
				// Unreachable in practice: destinations_test asserts every
				// Under resolves. Skipping beats panicking in a layout.
				continue
			}
			group.Items[parent].Children = append(group.Items[parent].Children, NavItem{
				Label: navLabel(d),
				Href:  d.Href,
			})
		}

		if len(group.Items) > 0 {
			groups = append(groups, group)
		}
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

// navLabel is the rail's name for a destination, which is sometimes shorter
// than the palette's. "Body insights" has to stand alone in a flat search
// result; under an Insights heading it is just "Body".
func navLabel(d Destination) string {
	if d.Nav.Label != "" {
		return d.Nav.Label
	}
	return d.Label
}
