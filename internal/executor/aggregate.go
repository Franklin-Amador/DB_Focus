package executor

import (
	"fmt"
	"strconv"
	"strings"
)

// aggregateFuncs lists the supported aggregate function names. MAX_BY/MIN_BY
// are two-argument aggregates (value, order).
var aggregateFuncs = []string{"COUNT", "SUM", "AVG", "MIN", "MAX", "MAX_BY", "MIN_BY"}

// aggregateArgs splits an aggregate's inner argument text into its comma-
// separated arguments (e.g. "status, updated_at" -> ["status", "updated_at"]).
func aggregateArgs(arg string) []string {
	parts := strings.Split(arg, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

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

// computeAggregate reduces rows to a single value. colIdx is the aggregated
// column (-1 for COUNT(*)); colIdx2 is the ordering column for MAX_BY/MIN_BY
// (ignored by the single-argument aggregates).
func computeAggregate(fn string, rows [][]interface{}, colIdx, colIdx2 int) interface{} {
	switch fn {
	case "MAX_BY", "MIN_BY":
		var best interface{}      // value at the extremum of the order column
		var bestOrder interface{} // current extremum of the order column
		for _, r := range rows {
			if colIdx < 0 || colIdx >= len(r) || colIdx2 < 0 || colIdx2 >= len(r) {
				continue
			}
			o := r[colIdx2]
			if o == nil { // ignore rows whose ordering value is NULL
				continue
			}
			if bestOrder == nil {
				bestOrder, best = o, r[colIdx]
				continue
			}
			c := compareCells(o, bestOrder)
			if (fn == "MAX_BY" && c > 0) || (fn == "MIN_BY" && c < 0) {
				bestOrder, best = o, r[colIdx]
			}
		}
		return best

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
