// Package clean implements the integrated cleanup: plans (dry-run first),
// guardrails, revalidation right before deletion, recycle bin or permanent
// deletion, official actions, JSONL journal and PowerShell script export.
//
// Safety rules (cadrage § 5 bis), none of which can be disabled:
//   - nothing is deleted without a prepared plan (dry-run with live checks);
//   - recycle bin by default, permanent only when asked explicitly;
//   - each file is revalidated (size, modification time, file number)
//     right before deletion and skipped if it changed;
//   - a hard-coded blacklist protects system locations;
//   - items of red rules are never deleted;
//   - reparse points are deleted themselves, never followed;
//   - every action is written to the journal.
package clean

import "strings"

// Block reasons (i18n keys "clean.reason.*").
const (
	ReasonVolumeRoot  = "volume_root"
	ReasonMetafile    = "metafile"
	ReasonSystem      = "system_dir"   // System32, SysWOW64, WinSxS...
	ReasonWindows     = "windows_dir"  // C:\Windows outside rule-allowed subfolders
	ReasonProgramDir  = "program_dir"  // Program Files, ProgramData and their direct children
	ReasonProfile     = "profile"      // C:\Users or a whole profile
	ReasonSystemFile  = "system_file"  // pagefile.sys, hiberfil.sys, swapfile.sys
	ReasonProtected   = "protected"    // System Volume Information, Boot, Recovery...
	ReasonRecycleRoot = "recycle_root" // $Recycle.Bin or a user's bin itself
)

var ntfsMetafiles = map[string]bool{
	"$mft": true, "$mftmirr": true, "$logfile": true, "$volume": true, "$attrdef": true, "$bitmap": true,
	"$boot": true, "$badclus": true, "$secure": true, "$upcase": true, "$extend": true,
}

var rootProtected = map[string]bool{
	"system volume information": true, "boot": true, "recovery": true, "efi": true, "bootmgr": true,
	"bootnxt": true, "config.msi": true, "$windows.~ws": false, "documents and settings": true,
	"$winreagent": true, "$sysreset": true,
}

var rootSystemFiles = map[string]bool{"pagefile.sys": true, "hiberfil.sys": true, "swapfile.sys": true, "dumpstack.log.tmp": true}

// Blocked checks a volume-relative path (segments, any case) against the
// hard-coded blacklist. viaRule is true when the item comes from a catalogue
// rule, which may reach inside C:\Windows, Program Files and ProgramData
// (e.g. Windows\Temp) but never into the system directories.
func Blocked(segs []string, viaRule bool) (bool, string) {
	n := len(segs)
	if n == 0 {
		return true, ReasonVolumeRoot
	}
	s := make([]string, n)
	for k, x := range segs {
		s[k] = strings.ToLower(x)
	}
	first := s[0]
	if ntfsMetafiles[first] {
		return true, ReasonMetafile
	}
	if n == 1 && rootSystemFiles[first] {
		return true, ReasonSystemFile
	}
	if rootProtected[first] {
		return true, ReasonProtected
	}
	switch first {
	case "windows":
		if n == 1 {
			return true, ReasonWindows
		}
		switch s[1] {
		case "system32", "syswow64", "winsxs", "servicing", "boot", "fonts", "inf", "systemapps", "systemresources":
			return true, ReasonSystem
		}
		if !viaRule {
			return true, ReasonWindows
		}
	case "program files", "program files (x86)", "programdata":
		if n <= 2 || !viaRule {
			return true, ReasonProgramDir
		}
	case "users":
		if n <= 2 {
			return true, ReasonProfile
		}
		if n == 3 && strings.HasPrefix(s[2], "ntuser.dat") {
			return true, ReasonProfile
		}
		if n == 3 && s[2] == "appdata" {
			return true, ReasonProfile
		}
	case "$recycle.bin":
		if n <= 2 {
			return true, ReasonRecycleRoot
		}
	}
	return false, ""
}
