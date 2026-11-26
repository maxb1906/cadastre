package report

import "time"

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
