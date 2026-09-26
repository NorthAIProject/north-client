package day

import (
	"sort"
	"time"
)

// Health metric names for sleep stages. One reading per contiguous block, its
// start and end being the block's; the value is its length in minutes.
const (
	MetricSleepDeep  = "sleep_deep"
	MetricSleepREM   = "sleep_rem"
	MetricSleepCore  = "sleep_core"
	MetricSleepAwake = "sleep_awake"
)

// StageMetrics maps each stage to the health metric that carries it.
func StageMetrics() map[SleepStage]string {
	return map[SleepStage]string{
		StageDeep:  MetricSleepDeep,
		StageREM:   MetricSleepREM,
		StageCore:  MetricSleepCore,
		StageAwake: MetricSleepAwake,
	}
}

// SleepFromBlocks assembles a night from device-reported stage blocks.
//
// The total counts every stage except awake: that is what "time asleep" means
// on every device that reports stages, and what the manual log records. It
// reports false when there are no blocks.
func SleepFromBlocks(blocks []SleepBlock, source string) (Sleep, bool) {
	if len(blocks) == 0 {
		return Sleep{}, false
	}
	sorted := append([]SleepBlock(nil), blocks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })

	out := Sleep{StageMinutes: map[SleepStage]int{}, Blocks: sorted, Source: source}
	start, end := sorted[0].Start, sorted[0].End
	for _, b := range sorted {
		m := b.Minutes()
		out.StageMinutes[b.Stage] += m
		if b.Stage != StageAwake {
			out.TotalMinutes += m
		}
		if b.End.After(end) {
			end = b.End
		}
	}
	out.Start, out.End = &start, &end
	return out, true
}

// NightWindow is where the night that counts toward date may start: from noon
// the day before until noon on the day. A block starting in it belongs to that
// morning, which is the same rule sleep_logs follow.
func NightWindow(date time.Time) (time.Time, time.Time) {
	y, m, d := date.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, date.Location())
	return midnight.Add(-12 * time.Hour), midnight.Add(12 * time.Hour)
}
