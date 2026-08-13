package dashboard

// CheckInDay is one calendar day in the mood/energy window.
type CheckInDay struct {
	Label  string
	Mood   int // zero means no check-in that day
	Energy int
}

// CheckInSeries is the trailing check-in window for charts.
type CheckInSeries struct {
	Days []CheckInDay
}

func (s CheckInSeries) HasData() bool {
	for _, d := range s.Days {
		if d.Mood > 0 || d.Energy > 0 {
			return true
		}
	}
	return false
}

// HabitsSummary aggregates adherence across active habits.
type HabitsSummary struct {
	HasHabits  bool
	Rate       int
	Kept       int
	Scheduled  int
	BestStreak int
}

// HydrationDay is one day in the hydration bar chart.
type HydrationDay struct {
	Label    string
	TotalML  int
	TargetML int
}

// HydrationSummary is today's intake plus a trailing bar series.
type HydrationSummary struct {
	TodayML  int
	TargetML int
	Percent  int
	Days     []HydrationDay
}

func (h HydrationSummary) HasData() bool {
	if h.TodayML > 0 {
		return true
	}
	for _, d := range h.Days {
		if d.TotalML > 0 {
			return true
		}
	}
	return false
}

// SleepSummary is last night's sleep when logged.
type SleepSummary struct {
	Logged          bool
	DurationMinutes int
	Quality         *int
}
