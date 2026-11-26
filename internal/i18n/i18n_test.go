package i18n

import (
	"regexp"
	"testing"
)

func TestCatalogsAreComplete(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z%]`)
	for key, en := range catalog["en"] {
		fr, ok := catalog["fr"][key]
		if !ok {
			t.Errorf("missing French message for %q", key)
			continue
		}
		a, b := verbs.FindAllString(en, -1), verbs.FindAllString(fr, -1)
		if len(a) != len(b) {
			t.Errorf("%q: verbs differ: en %v fr %v", key, a, b)
		}
	}
	for key := range catalog["fr"] {
		if _, ok := catalog["en"][key]; !ok {
			t.Errorf("missing English message for %q", key)
		}
	}
}

func TestT(t *testing.T) {
	if got := T("fr", "test.hello", "Max"); got != "Bonjour Max" {
		t.Error(got)
	}
	if got := T("xx", "test.hello", "Max"); got != "Hello Max" {
		t.Error(got)
	}
	if got := T("fr", "no.such.key"); got != "no.such.key" {
		t.Error(got)
	}
	if Normalize("fr_FR.UTF-8") != "fr" || Normalize("en-GB") != "en" || Normalize("de") != "" {
		t.Error("Normalize")
	}
}
