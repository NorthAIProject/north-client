package i18n

// englishLeaderboard is the XP leaderboard and level card.
var englishLeaderboard = map[string]string{
	"leaderboard.title":   "Leaderboard",
	"leaderboard.mono":    "Progress",
	"leaderboard.intro":   "You and the friends who share with you. XP comes only from things done: workouts, habits kept, streaks, milestones and goals.",
	"leaderboard.friends": "Friends",

	"leaderboard.level":          "Level %[1]d",
	"leaderboard.level.progress": "Progress to the next level",
	"leaderboard.level.tonext":   "%[1]d XP · %[2]d to the next level",
	"leaderboard.level.top":      "%[1]d XP · the top level",
	"leaderboard.week.total":     "%[1]d XP this week",

	// Level titles, keyed by the lowercased English title internal/xp stores.
	"leaderboard.level.starter":    "Starter",
	"leaderboard.level.mover":      "Mover",
	"leaderboard.level.regular":    "Regular",
	"leaderboard.level.committed":  "Committed",
	"leaderboard.level.steady":     "Steady",
	"leaderboard.level.strong":     "Strong",
	"leaderboard.level.relentless": "Relentless",

	"leaderboard.boards":         "Boards",
	"leaderboard.board.xp-week":  "XP this week",
	"leaderboard.board.xp-all":   "XP all time",
	"leaderboard.board.streak":   "Streak",
	"leaderboard.board.workouts": "Workouts this week",

	"leaderboard.alone": "Only you so far. Friends appear here once they share this with followers.",
	// The hidden line wraps the switch name and a link: before, the switch, under, the link.
	"leaderboard.hidden.before": "Your friends don't see you on this board. Turn on",
	"leaderboard.hidden.under":  "under",
	"leaderboard.hidden.link":   "What followers see",
	"leaderboard.you":           "(you)",
}
