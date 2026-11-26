//go:build !windows

package sysinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/maxb1906/cadastre/internal/rules"
)

// LookupSID is unavailable outside Windows.
func (Env) LookupSID(string) (string, bool, error) { return "", false, ErrUnavailable }

// Profiles is unavailable outside Windows.
func (Env) Profiles() ([]rules.Profile, error) { return nil, ErrUnavailable }

// InstallerReferences is unavailable outside Windows.
func (Env) InstallerReferences() (map[string]bool, error) { return nil, ErrUnavailable }

// RunningProcesses lists process names from /proc.
func RunningProcesses() (map[string]bool, error) {
	out := map[string]bool{}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, ErrUnavailable
	}
	for _, e := range ents {
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm")); err == nil {
			out[strings.ToLower(strings.TrimSpace(string(b)))] = true
		}
	}
	return out, nil
}

// HiddenCommand is exec.Command outside Windows.
func HiddenCommand(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

// ShadowStorageOn is unavailable outside Windows.
func ShadowStorageOn(string) (ShadowStorage, error) { return ShadowStorage{}, ErrUnavailable }

// IsServerCore is false outside Windows.
func IsServerCore() bool { return false }

// ProgramData returns a data directory for Cadastre outside Windows.
func ProgramData() string {
	if d, err := os.UserConfigDir(); err == nil {
		return d
	}
	return os.TempDir()
}
