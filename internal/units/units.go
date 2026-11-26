// Package units formats sizes, counts and durations for humans, in French or
// English.
package units

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Bytes formats n with binary multiples: "1,5 Go" (fr) or "1.5 GB" (en).
// Windows Explorer also uses binary multiples with these unit names.
func Bytes(n uint64, lang string) string {
	unitsFR := []string{"o", "Ko", "Mo", "Go", "To", "Po"}
	unitsEN := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	u := unitsEN
	if lang == "fr" {
		u = unitsFR
	}
	if n < 1024 {
		return strconv.FormatUint(n, 10) + " " + u[0]
	}
	v := float64(n)
	k := 0
	for v >= 1024 && k < len(u)-1 {
		v /= 1024
		k++
	}
	var s string
	switch {
	case v >= 100:
		s = strconv.FormatFloat(v, 'f', 0, 64)
	case v >= 10:
		s = strconv.FormatFloat(v, 'f', 1, 64)
	default:
		s = strconv.FormatFloat(v, 'f', 2, 64)
	}
	if lang == "fr" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s + " " + u[k]
}

// Count formats an integer with thousands separators.
func Count(n uint64, lang string) string {
	s := strconv.FormatUint(n, 10)
	sep := ","
	if lang == "fr" {
		sep = " " // narrow no-break space
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Percent formats part/total as a percentage with one decimal.
func Percent(part, total uint64, lang string) string {
	if total == 0 {
		return "–"
	}
	s := strconv.FormatFloat(100*float64(part)/float64(total), 'f', 1, 64)
	if lang == "fr" {
		return strings.Replace(s, ".", ",", 1) + " %"
	}
	return s + "%"
}

// Duration formats d compactly ("1.2 s", "3 min 05 s").
func Duration(d time.Duration, lang string) string {
	if d < time.Minute {
		s := strconv.FormatFloat(d.Seconds(), 'f', 1, 64)
		if lang == "fr" {
			s = strings.Replace(s, ".", ",", 1)
		}
		return s + " s"
	}
	m := int(d / time.Minute)
	sec := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%d min %02d s", m, sec)
}

// FileTimeToTime converts a Windows FILETIME to time.Time (zero stays zero).
func FileTimeToTime(ft int64) time.Time {
	if ft <= 0 {
		return time.Time{}
	}
	const epochDiff = 116444736000000000 // 1601-01-01 to 1970-01-01 in 100 ns
	return time.Unix(0, (ft-epochDiff)*100).UTC()
}

// TimeToFileTime converts a time.Time to a Windows FILETIME.
func TimeToFileTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	const epochDiff = 116444736000000000
	return t.UnixNano()/100 + epochDiff
}

// ParseAge parses "30d", "12h", "2w", "90m" or a Go duration.
func ParseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "0" {
		return 0, nil
	}
	mult := map[byte]time.Duration{'d': 24 * time.Hour, 'j': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	if m, ok := mult[s[len(s)-1]]; ok {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid age %q", s)
		}
		return time.Duration(n * float64(m)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid age %q", s)
	}
	return d, nil
}

// ParseSize parses "10M", "1.5G", "500k", "123" (bytes, binary multiples).
func ParseSize(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(s, "IB"), "B"), "O")
	if s == "" {
		return 0, fmt.Errorf("invalid size")
	}
	mult := uint64(1)
	switch s[len(s)-1] {
	case 'K':
		mult = 1 << 10
	case 'M':
		mult = 1 << 20
	case 'G':
		mult = 1 << 30
	case 'T':
		mult = 1 << 40
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return uint64(v * float64(mult)), nil
}
