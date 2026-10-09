// Package xp scores verified progress and ranks friends by it.
//
// XP is never stored. It is derived on read from what the other slices already
// record — a habit kept on a scheduled day, a check-in streak and the marks it
// reaches, a finished session, a weekly review, a crew challenge met, a
// milestone, a goal — so a goal reopened stops paying without a reversal, and
// nothing can be earned that the data does not show. The rules are
// docs/advanced-gamification.md's table; logging, chatting and rating pay
// nothing.
package xp

import "github.com/google/uuid"

// Kinds of verified action, and what each pays.
const (
	KindHabitKept      = "habit_kept"
	KindStreakDay      = "streak_day"
	KindWorkout        = "workout"
	KindMilestone      = "milestone"
	KindGoal           = "goal"
	KindWeekReviewed   = "week_reviewed"
	KindStreakMark     = "streak_mark"
	KindChallengeMet   = "challenge_met"
	PointsHabitKept    = 10
	PointsStreakDay    = 5
	PointsWorkout      = 20
	PointsMilestone    = 40
	PointsGoal         = 150
	PointsWeekReviewed = 25
	PointsStreakMark   = 50
	PointsChallengeMet = 30
	WorkoutMinMinutes  = 10
	WorkoutsPaidPerDay = 2
	StreakDayFrom      = 3
)

// Kinds in the order they are shown.
func Kinds() []string {
	return []string{
		KindWorkout, KindHabitKept, KindStreakDay, KindStreakMark,
		KindWeekReviewed, KindChallengeMet, KindMilestone, KindGoal,
	}
}

// Earned is how many of one kind counted, and what they paid.
type Earned struct {
	Kind   string
	Count  int
	Points int
}

// Level is a title from lifetime XP. It unlocks nothing and is never lost.
type Level struct {
	Number int
	Title  string
	// Floor is the XP the level starts at; Next the XP the next one does, or
	// zero at the top.
	Floor int
	Next  int
}

// levels are spaced so the first two arrive in the first month and the last
// takes about a year of real adherence.
var levels = []Level{
	{Number: 1, Title: "Starter", Floor: 0},
	{Number: 2, Title: "Mover", Floor: 100},
	{Number: 3, Title: "Regular", Floor: 300},
	{Number: 4, Title: "Committed", Floor: 800},
	{Number: 5, Title: "Steady", Floor: 1800},
	{Number: 6, Title: "Strong", Floor: 3500},
	{Number: 7, Title: "Relentless", Floor: 6000},
}

// LevelFor is the level a lifetime total reaches.
func LevelFor(total int) Level {
	i := 0
	for i+1 < len(levels) && total >= levels[i+1].Floor {
		i++
	}
	l := levels[i]
	if i+1 < len(levels) {
		l.Next = levels[i+1].Floor
	}
	return l
}

// Summary is one person's XP: this week by kind, and all time.
type Summary struct {
	Week      []Earned
	WeekTotal int
	Total     int
	Level     Level
}

// Boards a leaderboard can rank by.
const (
	MetricXP       = "xp"
	MetricStreak   = "streak"
	MetricWorkouts = "workouts"
	PeriodWeek     = "week"
	PeriodAll      = "all"
)

// Entry is one row of a leaderboard.
type Entry struct {
	Rank        int
	UserID      uuid.UUID
	DisplayName string
	Handle      string
	Value       int
	// Level is set on XP boards only.
	Level *Level
	Me    bool
}

// Board is a ranked list of the viewer and the friends who share this metric
// with them.
type Board struct {
	Metric  string
	Period  string
	Entries []Entry
	// Sharing is whether the viewer shares this metric, i.e. whether their
	// friends see them on their own boards.
	Sharing bool
}
