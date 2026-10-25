package las

import "math"

func float64FromBits(u uint64) float64 { return math.Float64frombits(u) }
func round(v float64) float64          { return math.Round(v) }
