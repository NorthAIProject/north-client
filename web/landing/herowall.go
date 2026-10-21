package landing

import (
	"context"
	"fmt"
	"math"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// The hero's card wall: a fan of small cards along the bottom of the hero,
// each a fragment of what Khepri keeps — a memory, a streak, a check-in, a
// plan. Decorative, so it is aria-hidden and holds nothing focusable.

// Card kinds. Each one is drawn by its own template in herowall.templ.
const (
	wallMemory   = "memory"
	wallStreak   = "streak"
	wallQuote    = "quote"
	wallSleep    = "sleep"
	wallGoal     = "goal"
	wallHabits   = "habits"
	wallJournal  = "journal"
	wallTelegram = "telegram"
	wallPlan     = "plan"
	wallLift     = "lift"
	wallImage    = "image"
)

// wallCard is the content of one card. Fields a kind does not use stay empty.
type wallCard struct {
	Kind  string
	Label string
	Title string
	Body  string
	Meta  string
	Lines []string
	Image string
	Cover bool // image fills the card rather than sitting on it
}

// wallDeck is every card the wall can show. Edit this list to change what the
// wall says; wallFront and wallBack decide where each card sits.
func wallDeck(ctx context.Context) []wallCard {
	t := func(key string) string { return i18n.T(ctx, key) }
	return []wallCard{
		0:  {Kind: wallStreak, Title: t("land.wall.streak"), Meta: t("land.wall.streak.sub")},
		1:  {Kind: wallJournal, Label: t("land.wall.journal"), Body: t("land.wall.journal.body")},
		2:  {Kind: wallMemory, Label: t("land.wall.memory"), Title: t("land.wall.memory.day"), Body: t("land.wall.memory.body"), Meta: t("land.wall.memory.note")},
		3:  {Kind: wallHabits, Label: t("land.wall.habits"), Meta: t("land.wall.habits.sub")},
		4:  {Kind: wallGoal, Label: t("land.wall.goal"), Title: t("land.wall.goal.title"), Body: "62%", Meta: t("land.wall.goal.sub")},
		5:  {Kind: wallSleep, Label: t("land.wall.sleep"), Title: "84", Body: "7h 32m", Meta: t("land.wall.sleep.sub")},
		6:  {Kind: wallLift, Label: t("land.wall.lift"), Title: t("land.wall.lift.title"), Meta: t("land.wall.lift.sub")},
		7:  {Kind: wallQuote, Body: t("land.wall.quote"), Meta: t("land.wall.quote.meta")},
		8:  {Kind: wallImage, Label: t("land.wall.icon"), Image: "/assets/brand/hero/khepri-app-icon.webp"},
		9:  {Kind: wallPlan, Label: t("land.wall.plan"), Title: t("land.wall.plan.title"), Lines: []string{t("land.wall.plan.1"), t("land.wall.plan.2"), t("land.wall.plan.3")}},
		10: {Kind: wallImage, Label: t("land.wall.mascot"), Image: "/assets/brand/hero/khepri-mascot.webp"},
		11: {Kind: wallImage, Label: t("land.wall.poster"), Image: "/assets/video/north-hero-poster.webp", Cover: true},
		12: {Kind: wallTelegram, Body: t("land.wall.tg.user"), Meta: t("land.wall.tg.reply")},
	}
}

// wallFront and wallBack are deck indexes, left to right. Neighbours differ,
// and the five front cards a phone shows (0, 4, 7, 10, 14) carry the claim on
// their own: a streak, a goal, the check-in, the coach, a Telegram reply.
var (
	wallFront = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 5, 2, 12}
	wallBack  = []int{9, 5, 12, 11, 1, 6, 3, 2, 0, 4, 8, 1, 7, 6}
)

// wallSlot is one positioned card.
type wallSlot struct {
	Card  wallCard
	Style string // custom properties read by .hero-wall-slot in input.css
	Class string // responsive visibility
}

// frontSlots lays out the front row: 15 cards from 0% to 100%, rotation
// +21deg to -21deg, sitting lower towards the edges so the row bleeds off the
// bottom. Five are always shown, four more from md, the last six from lg.
func frontSlots(ctx context.Context) []wallSlot {
	deck := wallDeck(ctx)
	n := len(wallFront)
	mid := float64(n-1) / 2
	slots := make([]wallSlot, n)
	for i, d := range wallFront {
		off := (float64(i) - mid) / mid
		slots[i] = wallSlot{
			Card:  deck[d],
			Style: slotStyle(float64(i)*100/float64(n-1), 21-3*float64(i), 20+16.8*off*off, i, mid),
			Class: frontVisibility(i),
		}
	}
	return slots
}

// backSlots lays out the back row, shown from md: 14 cards between the front
// ones, rotation +15.6deg to -15.6deg, arched up in the middle.
func backSlots(ctx context.Context) []wallSlot {
	deck := wallDeck(ctx)
	n := len(wallBack)
	mid := float64(n-1) / 2
	step := 100 / float64(n)
	slots := make([]wallSlot, n)
	for i, d := range wallBack {
		off := (float64(i) - mid) / mid
		slots[i] = wallSlot{
			Card:  deck[d],
			Style: slotStyle(step/2+float64(i)*step, 15.6-2.4*float64(i), -14.6+18.6*off*off, i, mid),
		}
	}
	return slots
}

// frontVisibility keeps the wall readable as the viewport narrows.
func frontVisibility(i int) string {
	switch i {
	case 0, 4, 7, 10, 14:
		return ""
	case 2, 5, 9, 12:
		return "hidden md:block"
	default:
		return "hidden lg:block"
	}
}

// slotStyle writes one card's position. Cards nearer the middle stack on top,
// and the sway delays cycle through five offsets so neighbours never move
// together.
func slotStyle(x, rot, y float64, i int, mid float64) string {
	z := 20 - int(math.Round(math.Abs(float64(i)-mid)))
	return fmt.Sprintf("--x:%.2f%%;--rot:%.1fdeg;--y:%.1f%%;--delay:%.1fs;z-index:%d",
		x, rot, y, 0.7*float64(i%5), z)
}

// wallHabitGrid is the habit card's five weeks, Monday first. true is a day
// the habit was done.
var wallHabitGrid = [5][7]bool{
	{true, false, true, true, false, true, false},
	{true, true, false, true, true, false, true},
	{false, true, true, true, false, true, true},
	{true, true, true, false, true, true, true},
	{true, true, true, true, true, true, false},
}
