package knowledge

import (
	"net/url"
	"strconv"
)

func showMoreURL(view SearchView) string {
	next := view.Offset + len(view.Hits)
	q := url.Values{}
	q.Set("q", view.Query)
	q.Set("offset", strconv.Itoa(next))
	q.Set("limit", strconv.Itoa(view.Limit))
	q.Set("append", "1")
	return "/app/knowledge/search?" + q.Encode()
}
