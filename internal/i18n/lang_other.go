//go:build !windows

package i18n

import "os"

func systemLanguage() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if s := os.Getenv(v); s != "" {
			return s
		}
	}
	return ""
}
