// Package i18n holds the French and English messages of Cadastre, shared by
// the CLI, the reports and the GUI (which receives the whole catalog).
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Languages supported.
var Languages = []string{"fr", "en"}

// Normalize returns "fr" or "en" for a locale such as "fr-FR" or "en_US.UTF-8".
func Normalize(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(l, "fr") {
		return "fr"
	}
	if strings.HasPrefix(l, "en") {
		return "en"
	}
	return ""
}

// Detect returns the language to use: CADASTRE_LANG, then the user's UI
// language, then French.
func Detect() string {
	if l := Normalize(os.Getenv("CADASTRE_LANG")); l != "" {
		return l
	}
	if l := Normalize(systemLanguage()); l != "" {
		return l
	}
	return "fr"
}

// T returns the message for key in lang, formatted with args (fmt verbs).
// Unknown keys fall back to English, then to the key itself.
func T(lang, key string, args ...any) string {
	msg, ok := catalog[lang][key]
	if !ok {
		if msg, ok = catalog["en"][key]; !ok {
			msg = key
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// Catalog returns every message of lang (English fallback included), for the
// GUI and the HTML report.
func Catalog(lang string) map[string]string {
	out := make(map[string]string, len(catalog["en"]))
	for k, v := range catalog["en"] {
		out[k] = v
	}
	for k, v := range catalog[lang] {
		out[k] = v
	}
	return out
}

// Printer is a convenience wrapper bound to one language.
type Printer struct{ Lang string }

// T translates key.
func (p Printer) T(key string, args ...any) string { return T(p.Lang, key, args...) }

// add merges messages into the catalog (used by the messages_*.go files).
func add(lang string, m map[string]string) {
	if catalog[lang] == nil {
		catalog[lang] = map[string]string{}
	}
	for k, v := range m {
		if _, dup := catalog[lang][k]; dup {
			panic("i18n: duplicate key " + lang + "/" + k)
		}
		catalog[lang][k] = v
	}
}
