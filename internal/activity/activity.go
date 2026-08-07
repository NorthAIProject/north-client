// Package activity tracks a person's exercise sessions and estimates the
// calories burned using MET values scaled by their own body weight, rather
// than a flat per-activity number that would be the same for every user.
package activity

import "github.com/NorthAIProject/north-client/internal/activity/activity"

// The session/MET shapes live in a leaf package so the service and any
// future template that renders one do not import each other.
type (
	Session = activity.Session
	MET     = activity.MET
)

const (
	StatusActive    = activity.StatusActive
	StatusPaused    = activity.StatusPaused
	StatusCompleted = activity.StatusCompleted
	StatusCancelled = activity.StatusCancelled

	SourceManual = activity.SourceManual
	SourceStrava = activity.SourceStrava
)

var (
	METTable  = activity.METTable
	LookupMET = activity.LookupMET
	METCodes  = activity.METCodes
)
