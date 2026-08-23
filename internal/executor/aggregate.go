package executor

import (
	"fmt"
	"strconv"
	"strings"
)

// aggregateFuncs lists the supported aggregate function names.
var aggregateFuncs = []string{"COUNT", "SUM", "AVG", "MIN", "MAX"}

// parseAggregate recognizes an aggregate projection like "SUM(precio)" or
// "COUNT(*)". It returns the canonical (upper-case) function name and the raw
// argument (original case, "*" or a column name), and ok=false when the column
// expression is not an aggregate call.
func parseAggregate(name string) (fn string, arg string, ok bool) {
	trimmed := strings.TrimSpace(name)
	upper := strings.ToUpper(trimmed)
	for _, f := range aggregateFuncs {
		prefix := f + "("
		if strings.HasPrefix(upper, prefix) && strings.HasSuffix(trimmed, ")") {
			inner := strings.TrimSpace(trimmed[len(prefix) : len(trimmed)-1])
			return f, inner, true
		}
	}
	return "", "", false
}

// computeAggregate reduces a set of rows to a single aggregate value for the
// given function. colIdx is the index of the aggregated column, or -1 for
// COUNT(*). SUM/AVG parse cell values numerically; MIN/MAX order values
// numerically when possible and lexicographically otherwise; NULLs are ignored
// (except COUNT(*), which counts every row).
func computeAggregate(fn string, rows [][]interface{}, colIdx int) interface{} {
	switch fn {
	case "COUNT":
		if colIdx < 0 {
			return len(rows)
		}
		n := 0
		for _, r := range rows {
			if colIdx < len(r) && r[colIdx] != nil {
				n++
			}
		}
		return n

	case "SUM":
		sum := 0.0
		for _, r := range rows {
			if colIdx >= 0 && colIdx < len(r) {
				if f, ok := toFloat(r[colIdx]); ok {
					sum += f
				}
			}
		}
		return numberValue(sum)

	case "AVG":
		sum, cnt := 0.0, 0
		for _, r := range rows {
			if colIdx >= 0 && colIdx < len(r) {
				if f, ok := toFloat(r[colIdx]); ok {
					sum += f
					cnt++
				}
			}
		}
		if cnt == 0 {
			return nil
		}
		return sum / float64(cnt)

	case "MIN", "MAX":
		var best interface{}
		for _, r := range rows {
			if colIdx < 0 || colIdx >= len(r) || r[colIdx] == nil {
				continue
			}
			if best == nil {
				best = r[colIdx]
				continue
			}
			c := compareCells(r[colIdx], best)
			if (fn == "MIN" && c < 0) || (fn == "MAX" && c > 0) {
				best = r[colIdx]
			}
		}
		return best
	}
	return nil
}

// toFloat attempts to interpret a stored cell value as a float64.
func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		return f, err == nil
	}
}

// numberValue returns an int64 for whole numbers and a float64 otherwise, so
// integer aggregates render without a trailing ".0".
func numberValue(f float64) interface{} {
	if f == float64(int64(f)) {
		return int64(f)
	}
	return f
}
