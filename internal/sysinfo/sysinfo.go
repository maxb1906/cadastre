// Package sysinfo queries the system (accounts, profiles, Windows Installer
// registrations, processes, shadow copies). It implements rules.Env.
package sysinfo

import (
	"errors"

	"github.com/maxb1906/cadastre/internal/rules"
)

// ErrUnavailable is returned on platforms without the information.
var ErrUnavailable = errors.New("sysinfo: unavailable on this platform")

// Env is the system implementation of rules.Env.
type Env struct{}

var _ rules.Env = Env{}

// ShadowStorage is the VSS diff area hosted on a volume.
type ShadowStorage struct {
	Used      uint64 // UsedSpace
	Allocated uint64 // AllocatedSpace
	Max       uint64 // MaxSpace
	Copies    int    // number of shadow copies of the volume
}
