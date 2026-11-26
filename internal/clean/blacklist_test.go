package clean

import (
	"strings"
	"testing"
)

func TestBlacklist(t *testing.T) {
	split := func(p string) []string {
		if p == "" {
			return nil
		}
		return strings.Split(p, `\`)
	}
	for _, c := range []struct {
		path    string
		viaRule bool
		blocked bool
	}{
		{``, true, true},
		{`$MFT`, true, true},
		{`$Extend\$UsnJrnl`, true, true},
		{`Windows`, true, true},
		{`Windows\Temp\x.tmp`, true, false},
		{`Windows\Temp\x.tmp`, false, true},
		{`windows\SYSTEM32\drivers\x.sys`, true, true},
		{`Windows\WinSxS\amd64_x\y.dll`, true, true},
		{`Windows\SysWOW64\a.dll`, true, true},
		{`Program Files`, true, true},
		{`Program Files\App`, true, true},
		{`Program Files\Apache24\logs\access.log`, true, false},
		{`Program Files\Apache24\logs\access.log`, false, true},
		{`ProgramData\Microsoft`, true, true},
		{`Users`, false, true},
		{`Users\bob`, true, true},
		{`Users\bob\AppData`, false, true},
		{`Users\bob\NTUSER.DAT`, false, true},
		{`Users\bob\Videos\a.mp4`, false, false},
		{`pagefile.sys`, true, true},
		{`HIBERFIL.SYS`, false, true},
		{`data\pagefile.sys`, false, false},
		{`System Volume Information\x`, true, true},
		{`$Recycle.Bin`, true, true},
		{`$Recycle.Bin\S-1-5-21-1`, true, true},
		{`$Recycle.Bin\S-1-5-21-1\$R1.zip`, true, false},
		{`Recovery\WindowsRE\winre.wim`, true, true},
		{`inetpub\logs\LogFiles\W3SVC1\u_ex1.log`, false, false},
	} {
		got, reason := Blocked(split(c.path), c.viaRule)
		if got != c.blocked {
			t.Errorf("%q (rule=%v): blocked=%v (%s), want %v", c.path, c.viaRule, got, reason, c.blocked)
		}
	}
}
