// Package memories owns durable profile facts: manual notes, extraction
// candidates, and the subset the coach is allowed to know.
package memories

import "github.com/NorthAIProject/north-client/internal/memories/memory"

type Memory = memory.Memory

const (
	StatusPending  = memory.StatusPending
	StatusApproved = memory.StatusApproved
	StatusRejected = memory.StatusRejected

	SourceUser       = memory.SourceUser
	SourceExtraction = memory.SourceExtraction

	CategoryPreference = memory.CategoryPreference
	CategoryConstraint = memory.CategoryConstraint
	CategoryHabit      = memory.CategoryHabit
	CategoryInjury     = memory.CategoryInjury
	CategoryEquipment  = memory.CategoryEquipment
	CategorySchedule   = memory.CategorySchedule
	CategoryCoaching   = memory.CategoryCoaching
	CategoryGeneral    = memory.CategoryGeneral
)

var (
	Categories = memory.Categories
	Statuses   = memory.Statuses
)
