package main

import "github.com/maxb1906/cadastre/internal/report"

// cleanState holds the cleanup plans of the session (milestone 8).
type cleanState struct{}

func (a *App) cleanupSummary() *report.CleanupSummary { return nil }
