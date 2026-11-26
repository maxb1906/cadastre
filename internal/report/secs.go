package report

import (
	"strconv"
	"time"
)

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// Itoa formats an int (small helper shared with the GUI).
func Itoa(n int) string { return strconv.Itoa(n) }
