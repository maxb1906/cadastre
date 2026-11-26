// Package engine chooses and runs a scan engine (A: raw MFT, C: directory
// walk) and packages the outcome.
package engine

import (
	"time"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/volume"
)

// Engine names.
const (
	EngineMFT  = "mft"  // A: raw MFT
	EngineWalk = "walk" // C: classic directory walk
)

// Warning is a translatable message attached to a result.
type Warning struct {
	Key  string `json:"key"`
	Args []any  `json:"args,omitempty"`
}

// Result is a finished scan.
type Result struct {
	Index        *index.Index
	Volume       volume.Info
	Engine       string
	Started      time.Time
	Duration     time.Duration
	ClusterSize  uint32
	Admin        bool
	FromSnapshot bool
	Warnings     []Warning

	// Engine A details.
	MFTRecords, MFTBad, MFTBytes uint64
	MFTRuns                      int
	// Engine C details.
	WalkErrors uint64
}

// Warn appends a warning.
func (r *Result) Warn(key string, args ...any) {
	r.Warnings = append(r.Warnings, Warning{Key: key, Args: args})
}
