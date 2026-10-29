package treemap

import "sort"

// Rect is a laid-out tile.
type Rect struct {
	Index      int // position in the input slice
	X, Y, W, H float64
}

// Squarify lays out values (any order, non-negative) in the rectangle so each
// tile's area is proportional to its value and tiles stay close to square
// (Bruls, Huizing and van Wijk's squarified treemap). Zero values get no tile.
func Squarify(values []float64, x, y, w, h float64) []Rect {
	idx := make([]int, 0, len(values))
	var total float64
	for i, v := range values {
		if v > 0 {
			idx = append(idx, i)
			total += v
		}
	}
	if total == 0 || w <= 0 || h <= 0 {
		return nil
	}
	sort.SliceStable(idx, func(a, b int) bool { return values[idx[a]] > values[idx[b]] })
	scale := w * h / total // value -> area

	var out []Rect
	for len(idx) > 0 {
		short := min(w, h)
		// Grow the row while it makes the worst aspect ratio better.
		row, rowSum := 1, values[idx[0]]*scale
		for row < len(idx) {
			next := rowSum + values[idx[row]]*scale
			if worst(idx[:row+1], values, scale, next, short) > worst(idx[:row], values, scale, rowSum, short) {
				break
			}
			row, rowSum = row+1, next
		}
		// Lay the row along the short side.
		thick := rowSum / short
		pos := 0.0
		for _, i := range idx[:row] {
			length := values[i] * scale / thick
			if w >= h { // row is a column on the left
				out = append(out, Rect{i, x, y + pos, thick, length})
			} else { // row is a strip on top
				out = append(out, Rect{i, x + pos, y, length, thick})
			}
			pos += length
		}
		if w >= h {
			x, w = x+thick, w-thick
		} else {
			y, h = y+thick, h-thick
		}
		idx = idx[row:]
	}
	return out
}

// worst is the largest aspect ratio in a row of the given total area laid along side.
func worst(row []int, values []float64, scale, sum, side float64) float64 {
	var hi, lo float64
	for i, r := range row {
		a := values[r] * scale
		if i == 0 || a > hi {
			hi = a
		}
		if i == 0 || a < lo {
			lo = a
		}
	}
	s2, sum2 := side*side, sum*sum
	return max(s2*hi/sum2, sum2/(s2*lo))
}
