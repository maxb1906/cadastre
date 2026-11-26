//go:build windows

package sysinfo

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/maxb1906/cadastre/internal/rules"
)

// LookupSID resolves a SID string to DOMAIN\user.
func (Env) LookupSID(sid string) (string, bool, error) {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return "", false, nil // not a SID: treat as unknown
	}
	acct, dom, _, err := s.LookupAccount("")
	if err != nil {
		if errors.Is(err, windows.ERROR_NONE_MAPPED) {
			return "", false, nil
		}
		return "", false, err
	}
	if dom != "" {
		return dom + `\` + acct, true, nil
	}
	return acct, true, nil
}

// Profiles reads HKLM\...\ProfileList.
func (Env) Profiles() ([]rules.Profile, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`, registry.READ)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	sids, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var out []rules.Profile
	for _, sid := range sids {
		sk, err := registry.OpenKey(k, sid, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		p, _, err := sk.GetStringValue("ProfileImagePath")
		sk.Close()
		if err != nil {
			continue
		}
		if x, err := registry.ExpandString(p); err == nil {
			p = x
		}
		out = append(out, rules.Profile{SID: strings.TrimSuffix(sid, ".bak"), Path: p})
	}
	return out, nil
}

// InstallerReferences collects the LocalPackage values of every product and
// patch registered with Windows Installer.
func (Env) InstallerReferences() (map[string]bool, error) {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Installer\UserData`, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	users, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	refs := map[string]bool{}
	add := func(k registry.Key, sub string) {
		sk, err := registry.OpenKey(k, sub, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer sk.Close()
		if v, _, err := sk.GetStringValue("LocalPackage"); err == nil && v != "" {
			refs[strings.ToLower(filepath.Base(v))] = true
		}
	}
	for _, u := range users {
		for _, kind := range []string{"Products", "Patches"} {
			kk, err := registry.OpenKey(root, u+`\`+kind, registry.READ)
			if err != nil {
				continue
			}
			codes, _ := kk.ReadSubKeyNames(-1)
			for _, c := range codes {
				if kind == "Products" {
					add(kk, c+`\InstallProperties`)
				} else {
					add(kk, c)
				}
			}
			kk.Close()
		}
	}
	return refs, nil
}

// RunningProcesses returns the lowercase executable names of running
// processes.
func RunningProcesses() (map[string]bool, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	out := map[string]bool{}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))] = true
	}
	return out, nil
}

// HiddenCommand returns an exec.Cmd that does not flash a console window.
func HiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

// ShadowStorageOn returns the VSS diff areas hosted on the volume whose GUID
// path is volGUID (\\?\Volume{...}\), via WMI (Win32_ShadowStorage).
func ShadowStorageOn(volGUID string) (ShadowStorage, error) {
	var st ShadowStorage
	if volGUID == "" {
		return st, ErrUnavailable
	}
	script := `$ErrorActionPreference='Stop';` +
		`$s=@(Get-CimInstance Win32_ShadowStorage | ForEach-Object { [pscustomobject]@{Diff=$_.DiffVolume.DeviceID; Vol=$_.Volume.DeviceID; Used=[uint64]$_.UsedSpace; Alloc=[uint64]$_.AllocatedSpace; Max=[uint64]$_.MaxSpace} });` +
		`$c=@(Get-CimInstance Win32_ShadowCopy | ForEach-Object { $_.VolumeName });` +
		`[pscustomobject]@{Storage=$s; Copies=$c} | ConvertTo-Json -Compress -Depth 3`
	out, err := HiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return st, err
	}
	var res struct {
		Storage json.RawMessage
		Copies  json.RawMessage
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return st, err
	}
	type storage struct {
		Diff, Vol        string
		Used, Alloc, Max uint64
	}
	var list []storage
	if err := unmarshalOneOrMany(res.Storage, &list); err != nil {
		return st, err
	}
	for _, s := range list {
		if strings.EqualFold(s.Diff, volGUID) {
			st.Used += s.Used
			st.Allocated += s.Alloc
			st.Max += s.Max
		}
	}
	var copies []string
	_ = unmarshalOneOrMany(res.Copies, &copies)
	for _, c := range copies {
		if strings.EqualFold(c, volGUID) {
			st.Copies++
		}
	}
	return st, nil
}

// unmarshalOneOrMany accepts a JSON array, a single object or null
// (ConvertTo-Json unwraps one-element arrays).
func unmarshalOneOrMany[T any](raw json.RawMessage, out *[]T) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '[' {
		return json.Unmarshal(raw, out)
	}
	var one T
	if err := json.Unmarshal(raw, &one); err != nil {
		return err
	}
	*out = append(*out, one)
	return nil
}

// IsServerCore reports whether Windows runs without the desktop experience.
func IsServerCore() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("InstallationType")
	return err == nil && strings.EqualFold(v, "Server Core")
}

// ProgramData returns %ProgramData%.
func ProgramData() string {
	if p := os.Getenv("ProgramData"); p != "" {
		return p
	}
	return `C:\ProgramData`
}
