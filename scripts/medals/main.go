//go:build ignore

// Excluded from the module build: it renders web/shared/medal, which left
// with the browser app. It moves to the web app's repository with web/assets.

// Command medals writes scripts/medals/preview/index.html: every coin medal
// with its id, English name and description, so names, copy and art can be
// judged together. Run from the repository root: go run ./scripts/medals
package main

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log"
	"os"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/web/shared/medal"
)

const out = "scripts/medals/preview/index.html"

func coin(b award.Badge, size int, locked, monthly bool) string {
	name := i18n.Translate("en", "awards.badge."+b.ID+".name")
	var buf bytes.Buffer
	err := medal.Coin(medal.Props{BadgeID: b.ID, Area: b.Area, Size: size, Locked: locked, Monthly: monthly, Label: name}).Render(context.Background(), &buf)
	if err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

func monthly(p medal.MonthlyProps) string {
	var buf bytes.Buffer
	if err := medal.MonthlyCoin(p).Render(context.Background(), &buf); err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

func card(b award.Badge, size int, locked, monthly bool) string {
	name := html.EscapeString(i18n.Translate("en", "awards.badge."+b.ID+".name"))
	desc := html.EscapeString(i18n.Translate("en", "awards.badge."+b.ID+".desc"))
	area := html.EscapeString(i18n.Translate("en", "awards.area."+string(b.Area)))
	priv := ""
	if b.Area.Private() {
		priv = " &middot; private"
	}
	return fmt.Sprintf(`<figure class="card">%s<figcaption><b>%s</b><code>%s</code><span class="area">%s%s</span><p>%s</p></figcaption></figure>`,
		coin(b, size, locked, monthly), name, b.ID, area, priv, desc)
}

func main() {
	cat := award.Catalog()
	var b bytes.Buffer
	b.WriteString(head)
	b.WriteString("<h2>All " + fmt.Sprint(len(cat)) + " badges, earned, 96 px</h2><div class=grid>")
	for _, x := range cat {
		b.WriteString(card(x, 96, false, false))
	}
	b.WriteString("</div><h2>Monthly (gold scalloped rim) and locked, 96 px</h2><div class=grid>")
	b.WriteString(card(cat[3], 96, false, true))
	b.WriteString(card(cat[3], 96, true, false))
	b.WriteString(card(cat[9], 96, true, false))
	b.WriteString("</div><h2>Small, 40 px: earned, locked, monthly</h2><div class=row>")
	for _, x := range cat {
		b.WriteString(coin(x, 40, false, false))
	}
	b.WriteString("</div><div class=row>")
	for _, x := range cat {
		b.WriteString(coin(x, 40, true, false))
	}
	b.WriteString("</div><div class=row>")
	for _, x := range cat[:11] {
		b.WriteString(coin(x, 40, false, true))
	}
	b.WriteString("</div><h2>Monthly challenge coin: OCT 18, NOV 5, DEC 27 earned, one locked; 96 px then 36 px</h2>")
	months := []medal.MonthlyProps{
		{Month: "OCT", Target: 18}, {Month: "NOV", Target: 5}, {Month: "DEC", Target: 27}, {Month: "JAN", Target: 100, Locked: true},
	}
	for _, size := range []int{96, 36} {
		b.WriteString("<div class=row>")
		for _, m := range months {
			m.Size = size
			m.Label = m.Month + " " + fmt.Sprint(m.Target)
			b.WriteString(monthly(m))
		}
		b.WriteString("</div>")
	}
	b.WriteString("</body></html>")
	if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", out)
}

const head = `<!doctype html><html><head><meta charset=utf-8><title>Medals</title>
<style>
:root{--bg:#fff;--fg:#18181b;--mut:#52525b}
body.dark{--bg:#0b0b0f;--fg:#f4f4f5;--mut:#a1a1aa}
body{margin:0;padding:24px;background:var(--bg);color:var(--fg);font:14px system-ui,sans-serif;width:1100px}
h2{font-size:15px;margin:28px 0 12px}
.grid{display:grid;grid-template-columns:repeat(4,1fr);gap:18px}
.card{margin:0;display:flex;gap:12px;align-items:flex-start}
.card svg{flex:none}
figcaption{display:flex;flex-direction:column;gap:2px;min-width:0}
figcaption code,.area{color:var(--mut);font-size:11px}
figcaption p{margin:2px 0 0;font-size:12px;color:var(--mut)}
.row{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:10px}
</style>
<script>if(new URLSearchParams(location.search).get('theme')==='dark')document.documentElement.classList.add('dark'),addEventListener('DOMContentLoaded',()=>document.body.classList.add('dark'))</script>
</head><body>`
