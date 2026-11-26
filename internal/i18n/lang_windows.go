//go:build windows

package i18n

import "golang.org/x/sys/windows"

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func systemLanguage() string {
	r, _, _ := procGetUserDefaultUILanguage.Call()
	switch uint16(r) & 0x3FF { // primary language id
	case 0x0C:
		return "fr"
	case 0x09:
		return "en"
	}
	return "en"
}
