package queryutil

import (
	"fmt"
	"strconv"
)

// compareValues compares two values and returns -1, 0, or 1
func compareValues(a, b interface{}) int {
	// Try numeric comparison by converting both to float64
	if f1, ok1 := toFloat(a); ok1 {
		if f2, ok2 := toFloat(b); ok2 {
			if f1 < f2 {
				return -1
			} else if f1 > f2 {
				return 1
			}
			return 0
		}
	}

	// Bool comparison
	if vb1, ok1 := a.(bool); ok1 {
		if vb2, ok2 := b.(bool); ok2 {
			if vb1 == vb2 {
				return 0
			} else if !vb1 && vb2 {
				return -1
			}
			return 1
		}
	}

	// Fallback to string comparison
	s1 := fmt.Sprintf("%v", a)
	s2 := fmt.Sprintf("%v", b)
	if s1 < s2 {
		return -1
	} else if s1 > s2 {
		return 1
	}
	return 0
}

// toFloat attempts to convert various numeric types and numeric strings to float64
func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		if n, err := strconv.ParseFloat(x, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}
