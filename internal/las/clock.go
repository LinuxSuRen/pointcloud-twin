package las

import "time"

var now = time.Now

func yday() int { return now().YearDay() }
func year() int { return now().Year() }
